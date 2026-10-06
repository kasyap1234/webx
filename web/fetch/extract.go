package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ExtractRequest configures structured extraction: fetch a page, hand its
// markdown + a JSON schema to an LLM, get typed data back. Targets any
// OpenAI-compatible endpoint — Ollama by default (free, local) or a hosted
// provider via env.
//
//	WEBX_LLM_BASE        OpenAI-compatible base (default http://localhost:11434/v1)
//	WEBX_LLM_MODEL       model name (default llama3.1)
//	WEBX_LLM_KEY         bearer token for hosted providers
//	WEBX_LLM_MAX_TOKENS  generation cap (default 4096 — reasoning models
//	                     spend thinking budget before content; bounds runaway)
//	WEBX_LLM_TIMEOUT     request timeout in seconds (default 120 — raise for slow local hardware)
type ExtractRequest struct {
	URL      string
	Markdown string         // pre-fetched page text — skips the fetch when set
	Title    string         // page title for provenance when Markdown is set
	Schema   map[string]any // JSON schema for the wanted structure
	Prompt   string         // extra instructions ("extract the pricing tiers")
	MaxChars int            // page context cap, 0 -> 16000
	Browser  bool
	Session  string
}

// ExtractResult is the LLM's structured answer plus provenance.
type ExtractResult struct {
	URL   string         `json:"url"`
	Title string         `json:"title,omitempty"`
	Data  map[string]any `json:"data"`
	Model string         `json:"model"`
	Base  string         `json:"base"`
	OK    bool           `json:"ok"`
	Err   string         `json:"err,omitempty"`
}

func llmBase() string {
	if b := os.Getenv("WEBX_LLM_BASE"); b != "" {
		return strings.TrimRight(b, "/")
	}
	return "http://localhost:11434/v1"
}

func llmModel() string {
	if m := os.Getenv("WEBX_LLM_MODEL"); m != "" {
		return m
	}
	return "llama3.1"
}

func llmMaxTokens() int {
	if v := os.Getenv("WEBX_LLM_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 4096
}

func llmTimeout() time.Duration {
	if v := os.Getenv("WEBX_LLM_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 120 * time.Second
}

var extractClient = &http.Client{Timeout: llmTimeout()}

// LLMChat is the shared OpenAI-compatible chat call — extract, summarize,
// and research all ride on it. Returns the assistant message text.
//
// Two retry classes, both bounded: a 400 naming response_format drops that
// field (LM Studio and older llama.cpp reject "json_object"; prompts demand
// bare JSON anyway), and 429/5xx — free-tier shared pools saturate for
// seconds at a time — retry with backoff. max_tokens is always bounded: a
// misconfigured chat template makes small models generate endlessly.
func LLMChat(ctx context.Context, sys, user string, jsonMode bool) (string, error) {
	noFormat := false
	var lastErr error
	for attempt := range 4 {
		out, code, raw, err := llmRequest(ctx, sys, user, jsonMode, noFormat)
		if err == nil {
			return out, nil
		}
		switch {
		case code == http.StatusBadRequest && jsonMode && !noFormat &&
			structuredUnsupported(raw):
			noFormat = true
		case code == http.StatusTooManyRequests || code >= 500:
			lastErr = err
			if attempt == 3 {
				break
			}
			select {
			case <-time.After(time.Duration(1<<attempt) * 2 * time.Second):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		default:
			return "", err
		}
	}
	return "", lastErr
}

// structuredUnsupported reports whether a 400 body blames the
// response_format field. Providers name it differently — OpenAI/LM Studio
// say "response_format", OpenRouter passes through upstream text like
// "does not support feature: structured-outputs".
func structuredUnsupported(raw []byte) bool {
	s := string(raw)
	return strings.Contains(s, "response_format") || strings.Contains(s, "structured")
}

// llmRequest performs one chat completion. Returns the assistant content,
// or the HTTP status and raw body for the caller to classify.
func llmRequest(ctx context.Context, sys, user string, jsonMode, noFormat bool) (string, int, []byte, error) {
	payload := map[string]any{
		"model":      llmModel(),
		"max_tokens": llmMaxTokens(),
		"messages": []map[string]string{
			{"role": "system", "content": sys},
			{"role": "user", "content": user},
		},
		"temperature": 0,
	}
	if jsonMode && !noFormat {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		llmBase()+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", 0, nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if k := os.Getenv("WEBX_LLM_KEY"); k != "" {
		httpReq.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := extractClient.Do(httpReq)
	if err != nil {
		return "", 0, nil, fmt.Errorf("llm unreachable at %s (%w) — start an OpenAI-compatible endpoint (default Ollama) or set WEBX_LLM_BASE", llmBase(), err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", 0, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// OpenAI-compatible errors carry {"error":{"message":…}} — surface
		// the message, not the raw JSON blob.
		var env struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &env) == nil && env.Error.Message != "" {
			return "", resp.StatusCode, raw, fmt.Errorf("llm %s (%s) — model %q at %s; pull the model or set WEBX_LLM_MODEL",
				http.StatusText(resp.StatusCode), env.Error.Message, llmModel(), llmBase())
		}
		return "", resp.StatusCode, raw, fmt.Errorf("llm status %d: %s", resp.StatusCode, truncateStr(string(raw), 200))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", resp.StatusCode, raw, fmt.Errorf("llm response parse: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", resp.StatusCode, raw, fmt.Errorf("llm returned no choices")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), resp.StatusCode, raw, nil
}

// LLMHealth probes the configured chat endpoint — doctor's row for the
// WEBX_LLM_* backend. A GET /models, not a generation: verifies reachability
// and that WEBX_LLM_MODEL exists without spending tokens.
func LLMHealth(ctx context.Context) (ok bool, latency time.Duration, note string) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, llmBase()+"/models", nil)
	if err != nil {
		return false, 0, err.Error()
	}
	if k := os.Getenv("WEBX_LLM_KEY"); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := extractClient.Do(req)
	latency = time.Since(start).Round(time.Millisecond)
	if err != nil {
		return false, latency, "unreachable — extract/verify/research degrade to excerpts"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, latency, fmt.Sprintf("status %d", resp.StatusCode)
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if json.Unmarshal(raw, &list) != nil || len(list.Data) == 0 {
		return true, latency, "reachable (model list unavailable)"
	}
	for _, m := range list.Data {
		if m.ID == llmModel() {
			return true, latency, "model ready"
		}
	}
	return false, latency, fmt.Sprintf("model %q not listed — set WEBX_LLM_MODEL to one of %d available", llmModel(), len(list.Data))
}

// Summarize produces a tight 3-5 sentence digest of a page — Firecrawl's
// "summary" format equivalent. Best-effort: callers ignore the error.
func Summarize(ctx context.Context, markdown string) (string, error) {
	if len(markdown) > 12000 {
		markdown = markdown[:12000]
	}
	return LLMChat(ctx,
		"You summarize web pages for AI coding agents. 3-5 sentences max: what the page is, the key facts/APIs, and anything non-obvious. No preamble.",
		markdown, false)
}

// Extract fetches the page then asks the configured LLM for structured data
// matching Schema — the Firecrawl /extract equivalent, minus the vendor.
// When req.Markdown is set the page text is already known and the fetch
// is skipped (callers that scraped the page upstream don't pay twice).
func Extract(ctx context.Context, req ExtractRequest) (*ExtractResult, error) {
	res := &ExtractResult{URL: req.URL, Model: llmModel(), Base: llmBase()}
	md := req.Markdown
	if md == "" {
		doc, err := Fetch(ctx, FetchRequest{URL: req.URL, Browser: req.Browser, Session: req.Session})
		if err != nil {
			return nil, err
		}
		res.Title = doc.Title
		md = doc.Markdown
	} else {
		res.Title = req.Title
	}
	cap := req.MaxChars
	if cap <= 0 {
		cap = 16000
	}
	if len(md) > cap {
		md = md[:cap]
	}
	schemaJSON, _ := json.Marshal(req.Schema)
	sys := `You are a data extraction engine. Given a web page in markdown, produce ONLY a JSON object matching the user's schema. No prose, no markdown fences, no commentary. If a field is absent on the page use null.`
	user := fmt.Sprintf("SCHEMA:\n%s\n\nINSTRUCTIONS: %s\n\nPAGE (markdown):\n%s",
		string(schemaJSON), req.Prompt, md)

	content, err := LLMChat(ctx, sys, user, true)
	if err != nil {
		return nil, err
	}
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	var data map[string]any
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		res.Err = "llm returned non-JSON: " + truncateStr(content, 120)
		res.Data = map[string]any{"raw": content}
		return res, nil
	}
	res.Data = data
	res.OK = true
	return res, nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

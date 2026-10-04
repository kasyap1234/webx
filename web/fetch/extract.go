package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ExtractRequest configures structured extraction: fetch a page, hand its
// markdown + a JSON schema to an LLM, get typed data back. Targets any
// OpenAI-compatible endpoint — Ollama by default (free, local) or a hosted
// provider via env.
//
//	WEBX_LLM_BASE   OpenAI-compatible base (default http://localhost:11434/v1)
//	WEBX_LLM_MODEL  model name (default llama3.1)
//	WEBX_LLM_KEY    bearer token for hosted providers
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

var extractClient = &http.Client{Timeout: 120 * time.Second}

// LLMChat is the shared OpenAI-compatible chat call — extract, summarize,
// and research all ride on it. Returns the assistant message text.
func LLMChat(ctx context.Context, sys, user string, jsonMode bool) (string, error) {
	payload := map[string]any{
		"model": llmModel(),
		"messages": []map[string]string{
			{"role": "system", "content": sys},
			{"role": "user", "content": user},
		},
		"temperature": 0,
	}
	if jsonMode {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		llmBase()+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if k := os.Getenv("WEBX_LLM_KEY"); k != "" {
		httpReq.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := extractClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("llm %s: %w", llmBase(), err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm status %d: %s", resp.StatusCode, truncateStr(string(raw), 200))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("llm response parse: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm returned no choices")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
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

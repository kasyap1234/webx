package eval

// Competitor engines — the same eval corpus run through Exa / Tavily /
// Firecrawl APIs so "how does webx compare" is a measurement, not a guess.
// Every engine is key-gated: no key → honestly skipped, never fabricated.

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

// Engine is a third-party search/extract API we can score with the same corpus.
type Engine interface {
	Name() string
	// Search returns ranked result URLs for the query.
	Search(ctx context.Context, query string, num int) ([]string, error)
	// Extract returns markdown-ish text for one URL; nil-able per engine.
	Extract(ctx context.Context, url string) (string, error)
}

// EnginesFor resolves names → engines. Missing keys are reported, not faked —
// eval output must never imply a comparison that did not run.
func EnginesFor(names []string) (engines []Engine, skipped []string) {
	reg := map[string]func() (Engine, string){
		"exa":       func() (Engine, string) { return exaEngine(), "EXA_API_KEY" },
		"tavily":    func() (Engine, string) { return tavilyEngine(), "TAVILY_API_KEY" },
		"firecrawl": func() (Engine, string) { return firecrawlEngine(), "FIRECRAWL_API_KEY" },
	}
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		f, ok := reg[n]
		if !ok {
			skipped = append(skipped, n+" (unknown engine)")
			continue
		}
		e, env := f()
		if e == nil {
			skipped = append(skipped, fmt.Sprintf("%s (no %s)", n, env))
			continue
		}
		engines = append(engines, e)
	}
	return engines, skipped
}

var engineHTTP = &http.Client{Timeout: 30 * time.Second}

func postJSON(ctx context.Context, url string, hdrs map[string]string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	resp, err := engineHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %.200s", resp.StatusCode, out)
	}
	return out, nil
}

// urlsFrom digs result URLs out of whatever array shape the API returned —
// response shapes drift between API versions, so we walk defensively.
func urlsFrom(payload []byte, keys ...string) []string {
	var v any
	if json.Unmarshal(payload, &v) != nil {
		return nil
	}
	var urls []string
	var walk func(x any)
	walk = func(x any) {
		switch t := x.(type) {
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			if u, ok := t["url"].(string); ok && u != "" {
				urls = append(urls, u)
				return
			}
			for _, k := range keys {
				if sub, ok := t[k]; ok {
					walk(sub)
				}
			}
		}
	}
	walk(v)
	return urls
}

// markdownFrom pulls a text body out of a scrape/extract response.
func markdownFrom(payload []byte, fields ...string) string {
	var v any
	if json.Unmarshal(payload, &v) != nil {
		return ""
	}
	var best string
	var walk func(x any)
	walk = func(x any) {
		switch t := x.(type) {
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			for _, f := range fields {
				if s, ok := t[f].(string); ok && len(s) > len(best) {
					best = s
				}
			}
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return best
}

// ─── Exa ────────────────────────────────────────────────────────────────────

type exa struct{ key string }

func exaEngine() Engine {
	k := os.Getenv("EXA_API_KEY")
	if k == "" {
		return nil
	}
	return &exa{key: k}
}

func (e *exa) Name() string { return "exa" }

func (e *exa) Search(ctx context.Context, q string, num int) ([]string, error) {
	out, err := postJSON(ctx, "https://api.exa.ai/search",
		map[string]string{"x-api-key": e.key},
		map[string]any{"query": q, "numResults": num, "type": "auto"})
	return urlsFrom(out, "results"), err
}

func (e *exa) Extract(ctx context.Context, u string) (string, error) {
	out, err := postJSON(ctx, "https://api.exa.ai/contents",
		map[string]string{"x-api-key": e.key},
		map[string]any{"urls": []string{u}, "text": true})
	if err != nil {
		return "", err
	}
	return markdownFrom(out, "text"), nil
}

// ─── Tavily ─────────────────────────────────────────────────────────────────

type tavily struct{ key string }

func tavilyEngine() Engine {
	k := os.Getenv("TAVILY_API_KEY")
	if k == "" {
		return nil
	}
	return &tavily{key: k}
}

func (e *tavily) Name() string { return "tavily" }

func (e *tavily) Search(ctx context.Context, q string, num int) ([]string, error) {
	out, err := postJSON(ctx, "https://api.tavily.com/search",
		map[string]string{"Authorization": "Bearer " + e.key},
		map[string]any{"query": q, "max_results": num, "search_depth": "advanced"})
	return urlsFrom(out, "results"), err
}

func (e *tavily) Extract(ctx context.Context, u string) (string, error) {
	out, err := postJSON(ctx, "https://api.tavily.com/extract",
		map[string]string{"Authorization": "Bearer " + e.key},
		map[string]any{"urls": []string{u}})
	if err != nil {
		return "", err
	}
	return markdownFrom(out, "raw_content", "content"), nil
}

// ─── Firecrawl ──────────────────────────────────────────────────────────────
// FIRECRAWL_BASE_URL lets you point at a self-hosted Firecrawl — or at a
// webx server, which is also the honest way to eval our /v1 compat.

type fcrawl struct{ key, base string }

func firecrawlEngine() Engine {
	base := strings.TrimSuffix(os.Getenv("FIRECRAWL_BASE_URL"), "/")
	if base == "" {
		base = "https://api.firecrawl.dev"
	}
	return &fcrawl{key: os.Getenv("FIRECRAWL_API_KEY"), base: base}
}

func (e *fcrawl) Name() string { return "firecrawl" }

func (e *fcrawl) Search(ctx context.Context, q string, num int) ([]string, error) {
	out, err := postJSON(ctx, e.base+"/v2/search",
		map[string]string{"Authorization": "Bearer " + e.key},
		map[string]any{"query": q, "limit": num})
	return urlsFrom(out, "data", "web", "results"), err
}

func (e *fcrawl) Extract(ctx context.Context, u string) (string, error) {
	out, err := postJSON(ctx, e.base+"/v1/scrape",
		map[string]string{"Authorization": "Bearer " + e.key},
		map[string]any{"url": u, "formats": []string{"markdown"}})
	if err != nil {
		return "", err
	}
	return markdownFrom(out, "markdown", "rawMarkdown"), nil
}

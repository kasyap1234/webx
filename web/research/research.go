// Package research implements multi-query deep research — the Tavily
// /research idea, composed from pieces webx already has: query expansion
// via the configured LLM, fused metasearch per sub-query, --fit-scraped
// source content, and a cited synthesis pass. Every stage degrades
// gracefully: no LLM → single-query + excerpts instead of a report.
package research

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/search"
)

// Report is the research run's output — the synthesized markdown plus the
// sources it cites and the sub-queries it fanned out to.
type Report struct {
	Query    string          `json:"query"`
	Queries  []string        `json:"queries"`        // expanded sub-queries actually searched
	Report   string          `json:"report"`         // markdown, [n]-cited
	JSON     json.RawMessage `json:"json,omitempty"` // set when output_schema was requested
	Sources  []Source        `json:"sources"`
	Fallback bool            `json:"fallback,omitempty"` // no LLM → excerpts only
}

// Source is one fetched page the report may cite — index i maps to [i+1].
type Source struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt,omitempty"` // fit-filtered content used as evidence
}

// Options tunes a research run.
type Options struct {
	MaxSources int // sources to fetch, 0 -> 6
	SubQueries int // expansion count, 0 -> 3
	// Schema, when set, switches the synthesis pass to structured output —
	// Tavily's output_schema: the report comes back as JSON matching the
	// schema, with a "sources" field listing cited indices.
	Schema map[string]any
}

// Run executes the pipeline: expand → search → fit-scrape → synthesize.
func Run(ctx context.Context, query string, opts Options, logf func(string, ...any)) (*Report, error) {
	if opts.MaxSources <= 0 {
		opts.MaxSources = 6
	}
	if opts.SubQueries <= 0 {
		opts.SubQueries = 3
	}
	rep := &Report{Query: query}

	// 1 — expand into sub-queries. LLM down → just the original query.
	queries := expand(ctx, query, opts.SubQueries)
	rep.Queries = queries
	if logf != nil && len(queries) > 1 {
		logf("expanded into %d sub-queries", len(queries))
	}

	// 2 — fused search per sub-query, dedup by URL.
	seen := map[string]bool{}
	var hits []search.Result
	for _, q := range queries {
		resp := search.Search(ctx, search.Request{Query: q, Num: 8})
		for _, r := range resp.Results {
			if !seen[r.URL] {
				seen[r.URL] = true
				hits = append(hits, r)
			}
		}
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("research: no sources found for %q", query)
	}
	if len(hits) > opts.MaxSources*2 {
		hits = hits[:opts.MaxSources*2]
	}

	// 3 — fetch sources with --fit so each contributes only relevant chunks.
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 4)
	for i := range hits {
		if len(rep.Sources) >= opts.MaxSources {
			break
		}
		r := hits[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			doc, err := fetch.Fetch(ctx, fetch.FetchRequest{
				URL: r.URL, Fit: query, AutoRender: false,
			})
			if err != nil {
				if logf != nil {
					logf("source %s: %v", r.URL, err)
				}
				return
			}
			content := doc.FitMarkdown
			if content == "" {
				content = doc.Markdown
			}
			if len(content) > 3000 {
				content = content[:3000] + "…"
			}
			title := doc.Title
			if title == "" {
				title = r.Title
			}
			mu.Lock()
			rep.Sources = append(rep.Sources, Source{URL: doc.FinalURL, Title: title, Excerpt: content})
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(rep.Sources) == 0 {
		return nil, fmt.Errorf("research: all %d candidate sources failed to fetch", len(hits))
	}

	// 4 — synthesize a cited report; LLM down → structured excerpts.
	if len(opts.Schema) > 0 {
		rep.JSON, rep.Fallback = synthesizeSchema(ctx, query, rep.Sources, opts.Schema)
		if len(rep.JSON) > 0 {
			rep.Report = string(rep.JSON)
		}
		return rep, nil
	}
	rep.Report, rep.Fallback = synthesize(ctx, query, rep.Sources)
	return rep, nil
}

// synthesizeSchema is synthesize with a JSON-schema contract — the LLM
// fills the caller's shape from the same numbered sources.
func synthesizeSchema(ctx context.Context, query string, sources []Source, schema map[string]any) (json.RawMessage, bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "QUESTION: %s\n\nSOURCES (cite as [n]):\n", query)
	for i, s := range sources {
		fmt.Fprintf(&b, "---\n[%d] %s\n%s\n%s\n", i+1, s.Title, s.URL, s.Excerpt)
	}
	schemaJSON, _ := json.Marshal(schema)
	out, err := fetch.LLMChat(ctx,
		`You are a research engine for coding agents. Answer the question from the numbered sources and return ONLY a JSON object matching this JSON Schema: `+string(schemaJSON)+`. Include a "citations" array of source indices you relied on (e.g. [1,3]). No prose outside the JSON.`,
		b.String(), true)
	if err != nil {
		return nil, true
	}
	out = strings.TrimSpace(out)
	out = strings.TrimPrefix(out, "```json")
	out = strings.TrimPrefix(out, "```")
	out = strings.TrimSuffix(out, "```")
	out = strings.TrimSpace(out)
	if !json.Valid([]byte(out)) {
		return nil, true
	}
	return json.RawMessage(out), false
}

// expand asks the LLM for search sub-queries; graceful single-query fallback.
func expand(ctx context.Context, query string, n int) []string {
	out, err := fetch.LLMChat(ctx,
		`You expand a research question into web search sub-queries. Reply ONLY as JSON: {"queries":["q1","q2","q3"]}. Make them diverse: definition/mechanism, practical how-to, current state/pitfalls.`,
		fmt.Sprintf("Question: %s\nReturn %d sub-queries.", query, n), true)
	if err != nil {
		return []string{query}
	}
	out = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(out), "```json"), "```")
	out = strings.TrimSuffix(out, "```")
	var parsed struct {
		Queries []string `json:"queries"`
	}
	if json.Unmarshal([]byte(out), &parsed) != nil || len(parsed.Queries) == 0 {
		return []string{query}
	}
	queries := append([]string{query}, parsed.Queries...)
	if len(queries) > n+1 {
		queries = queries[:n+1]
	}
	return queries
}

// synthesize writes the cited report. Fallback = numbered excerpts when no
// LLM is configured — still useful, honestly labeled.
func synthesize(ctx context.Context, query string, sources []Source) (string, bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "QUESTION: %s\n\nSOURCES (cite as [n]):\n", query)
	for i, s := range sources {
		fmt.Fprintf(&b, "---\n[%d] %s\n%s\n%s\n", i+1, s.Title, s.URL, s.Excerpt)
	}
	report, err := fetch.LLMChat(ctx,
		`You are a research engine for coding agents. Write a concise markdown report answering the question from the numbered sources. Rules: cite every claim as [n] inline; prefer code/API specifics over generalities; end with a "Sources" section listing [n] title — URL. Under 600 words.`,
		b.String(), false)
	if err != nil {
		var fb strings.Builder
		fmt.Fprintf(&fb, "# %s\n\n*(LLM unavailable — source excerpts below)*\n\n", query)
		for i, s := range sources {
			fmt.Fprintf(&fb, "## [%d] %s\n%s\n\n%s\n\n", i+1, s.Title, s.URL, s.Excerpt)
		}
		return fb.String(), true
	}
	return report, false
}

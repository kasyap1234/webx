// Package mcp exposes webx's capabilities as an MCP server over stdio
// (newline-delimited JSON-RPC 2.0), so coding agents can call search,
// scrape, ask, and map as native tools.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"

	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/index"
	"github.com/kasyap1234/webx/web/research"
	"github.com/kasyap1234/webx/web/search"
)

const protocolVersion = "2025-03-26"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

var tools = []map[string]any{
	{
		"name": "webx_search",
		"description": "Use when you need web search results — fused across free providers " +
			"(DuckDuckGo, HN, Stack Overflow, Wikipedia, GitHub, code search, package registries) " +
			"and a local docs index. Returns ranked URLs with snippets and provenance. " +
			"Set scrape=true to read top pages inline with query-relevant highlights; " +
			"rerank=true to re-sort by page-content relevance.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":     map[string]any{"type": "string", "description": "search query"},
				"num":       map[string]any{"type": "integer", "description": "max results (default 10)"},
				"site":      map[string]any{"type": "string", "description": "restrict to domain, e.g. go.dev"},
				"providers": map[string]any{"type": "string", "description": "comma list: index,ddg,hn,so,wiki,gh,reddit,sg,npm,crates,searxng,brave"},
				"scrape":    map[string]any{"type": "boolean", "description": "fetch page content inline"},
				"rerank":    map[string]any{"type": "boolean", "description": "re-sort by content relevance (needs scrape)"},
			},
			"required": []string{"query"},
		},
	},
	{
		"name": "webx_scrape",
		"description": "Use when you already have a URL and need its content as clean markdown — " +
			"docs pages, blog posts, GitHub files (returned as raw code), PDFs of specs. " +
			"Strips navigation/ads. For JS-rendered/SPA pages set render=true (or auto_render=true " +
			"to escalate only when needed); browser=true defeats TLS-fingerprint bot checks " +
			"(Cloudflare). actions scripts browser steps: \"click:.accept | wait:.quote | screenshot\".",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":         map[string]any{"type": "string", "description": "URL to fetch"},
				"max_chars":   map[string]any{"type": "integer", "description": "truncate output (default no limit)"},
				"raw":         map[string]any{"type": "boolean", "description": "skip extraction, full page"},
				"browser":     map[string]any{"type": "boolean", "description": "Chrome TLS fingerprint for bot-checked sites"},
				"render":      map[string]any{"type": "boolean", "description": "run JavaScript in Chrome — for SPA/client-rendered pages"},
				"auto_render": map[string]any{"type": "boolean", "description": "render only when the page looks JS-required"},
				"wait_for":    map[string]any{"type": "string", "description": "CSS selector to wait for before extracting"},
				"actions":     map[string]any{"type": "string", "description": "browser steps: \"click:.accept | wait:.q | type:#s=x | press:enter | scroll | screenshot\""},
				"fit":         map[string]any{"type": "string", "description": "return only blocks relevant to this query — the token-saving path"},
			},
			"required": []string{"url"},
		},
	},
	{
		"name": "webx_ask",
		"description": "Use for research questions needing several sources — searches the web, " +
			"reads the top pages, and returns citation-numbered excerpts under a token budget. " +
			"Cheaper than scrape-per-URL when you only need the answer, not the pages.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question":   map[string]any{"type": "string"},
				"num":        map[string]any{"type": "integer", "description": "sources to cite (default 5)"},
				"max_tokens": map[string]any{"type": "integer", "description": "output budget (default 3000)"},
			},
			"required": []string{"question"},
		},
	},
	{
		"name": "webx_query",
		"description": "Use to search ONLY the local docs index — your private/previously-indexed " +
			"corpus (site docs added via `webx index` or `webx seed`). Faster than web search, " +
			"works offline, and can see docs the public web can't.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"num":   map[string]any{"type": "integer", "description": "max results (default 10)"},
			},
			"required": []string{"query"},
		},
	},
	{
		"name": "webx_map",
		"description": "Use to discover what pages exist on a site — reads its sitemap. " +
			"Run before bulk-scraping or indexing a docs domain.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"domain": map[string]any{"type": "string", "description": "site domain or URL"},
				"limit":  map[string]any{"type": "integer", "description": "max URLs (default 200)"},
			},
			"required": []string{"domain"},
		},
	},
	{
		"name": "webx_doctor",
		"description": "Use when search results look empty or wrong — probes every provider " +
			"and reports which are live, their latency, and configuration errors.",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
	{
		"name": "webx_crawl",
		"description": "Use to follow links across a site toward a goal — adaptive crawl that " +
			"scores each page's relevance to your query, goes deeper where it's promising, and " +
			"stops when it has enough. Better than scrape+guess for 'find the API docs on X'.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":   map[string]any{"type": "string", "description": "start URL"},
				"goal":  map[string]any{"type": "string", "description": "what you're looking for — steers the crawl"},
				"limit": map[string]any{"type": "integer", "description": "max pages (default 10)"},
				"depth": map[string]any{"type": "integer", "description": "max link depth (default 2)"},
			},
			"required": []string{"url"},
		},
	},
	{
		"name": "webx_extract",
		"description": "Use to pull structured data out of a page — fetches it and runs a " +
			"schema-driven LLM extraction (Ollama/OpenAI-compatible backend). Returns JSON " +
			"matching your schema.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":    map[string]any{"type": "string", "description": "URL to extract from"},
				"schema": map[string]any{"type": "object", "description": "JSON schema for the output"},
				"prompt": map[string]any{"type": "string", "description": "extraction instructions"},
			},
			"required": []string{"url"},
		},
	},
	{
		"name": "webx_diff",
		"description": "Use to check whether a previously-indexed page changed — diffs the " +
			"live page against the indexed version (webx index must have seen it first).",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "indexed URL to compare"},
			},
			"required": []string{"url"},
		},
	},
	{
		"name": "webx_research",
		"description": "Use for deep research on a question — expands into sub-queries, reads " +
			"the top sources, and returns a cited markdown report (or JSON when output_schema is set).",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":         map[string]any{"type": "string", "description": "research question"},
				"max_sources":   map[string]any{"type": "integer", "description": "pages to read (default 6)"},
				"output_schema": map[string]any{"type": "object", "description": "JSON Schema — report returned as matching JSON"},
			},
			"required": []string{"query"},
		},
	},
	{
		"name": "webx_verify",
		"description": "Use to fact-check a factual claim against live web sources — returns " +
			"a verdict (supported|refuted|unclear) with confidence and cited sources.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"claim":       map[string]any{"type": "string", "description": "the claim to check"},
				"max_sources": map[string]any{"type": "integer", "description": "pages to read (default 4)"},
			},
			"required": []string{"claim"},
		},
	},
	{
		"name": "webx_watch",
		"description": "Use to check a watched page once for changes — fetches with a " +
			"conditional GET against the indexed snapshot and reports changed|unchanged.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "URL to check (indexed or not)"},
			},
			"required": []string{"url"},
		},
	},
	{
		"name": "webx_similar",
		"description": "Use to find pages similar to a URL — distills the page's signature " +
			"terms and matches against the local index (or live web with web=true).",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "URL to find similar pages for"},
				"num": map[string]any{"type": "integer", "description": "max results (default 10)"},
				"web": map[string]any{"type": "boolean", "description": "search the live web instead of the index"},
			},
			"required": []string{"url"},
		},
	},
	{
		"name": "webx_wayback",
		"description": "Use for deleted/paywalled/changed pages — pulls the nearest Wayback " +
			"Machine snapshot as clean markdown.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{"type": "string", "description": "URL to look up in the archive"},
				"ts":  map[string]any{"type": "string", "description": "target timestamp YYYYMMDD[hhmmss] — nearest capture wins"},
			},
			"required": []string{"url"},
		},
	},
	{
		"name": "webx_llms",
		"description": "Use to generate a whole-site llms.txt from the local index — " +
			"the agent-discovery file Mintlify sells, built from pages webx already knows.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"host":  map[string]any{"type": "string", "description": "indexed host, e.g. go.dev"},
				"limit": map[string]any{"type": "integer", "description": "max pages (default 200)"},
			},
			"required": []string{"host"},
		},
	},
	{
		"name": "webx_answer",
		"description": "Use for a direct answer with citations — searches, reads top sources, " +
			"and synthesizes via the configured LLM (extractive evidence when none is set). " +
			"Exa /answer parity.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "question to answer"},
				"num":   map[string]any{"type": "integer", "description": "sources to read (default 5)"},
				"llm":   map[string]any{"type": "boolean", "description": "synthesize via WEBX_LLM_* (default true)"},
			},
			"required": []string{"query"},
		},
	},
	{
		"name": "webx_batch",
		"description": "Use to scrape many URLs in one call — concurrent fetches merged into " +
			"one markdown result per URL. Faster than N webx_scrape calls.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"urls":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"max_chars": map[string]any{"type": "integer", "description": "truncate each page (default 8000)"},
				"fit":       map[string]any{"type": "string", "description": "keep only blocks relevant to this query"},
			},
			"required": []string{"urls"},
		},
	},
}

// Serve reads JSON-RPC messages from r and writes responses to w until EOF.
func Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 32<<20)
	enc := json.NewEncoder(w)

	write := func(resp rpcResponse) error {
		return enc.Encode(resp)
	}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = write(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"),
				Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}
		if req.ID == nil {
			continue // notification — no response
		}
		if resp := dispatch(ctx, &req); resp != nil {
			_ = write(*resp)
		}
	}
	return sc.Err()
}

// dispatch handles one JSON-RPC request — shared by the stdio loop and the
// streamable HTTP transport so both stay identical behaviorally.
// Returns nil for notifications (no id → no response).
func dispatch(ctx context.Context, req *rpcRequest) *rpcResponse {
	if req.ID == nil {
		return nil
	}
	base := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		base.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "webx", "version": "0.1.0"},
		}
	case "ping":
		base.Result = map[string]any{}
	case "tools/list":
		base.Result = map[string]any{"tools": tools}
	case "tools/call":
		var p toolCallParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			base.Error = &rpcError{Code: -32602, Message: "invalid tools/call params"}
			break
		}
		base.Result = callTool(ctx, p.Name, p.Arguments)
	default:
		base.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return &base
}

// callTool dispatches a tools/call to the engine and shapes the response as
// MCP content blocks.
func callTool(ctx context.Context, name string, args json.RawMessage) any {
	text, err := runTool(ctx, name, args)
	content := []map[string]any{{"type": "text", "text": text}}
	if err != nil {
		return map[string]any{"content": []map[string]any{
			{"type": "text", "text": "webx error: " + err.Error()},
		}, "isError": true}
	}
	return map[string]any{"content": content}
}

func runTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	switch name {
	case "webx_search":
		var a struct {
			Query     string `json:"query"`
			Num       int    `json:"num"`
			Site      string `json:"site"`
			Providers string `json:"providers"`
			Scrape    bool   `json:"scrape"`
			Rerank    bool   `json:"rerank"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Query == "" {
			return "", fmt.Errorf("required argument: query")
		}
		var providers []string
		if a.Providers != "" {
			providers = strings.Split(a.Providers, ",")
		}
		resp := search.Search(ctx, search.Request{
			Query: a.Query, Num: a.Num, Site: a.Site, Providers: providers,
			Scrape: a.Scrape || a.Rerank, Rerank: a.Rerank,
		})
		return marshalSearch(resp), nil

	case "webx_query":
		var a struct {
			Query string `json:"query"`
			Num   int    `json:"num"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Query == "" {
			return "", fmt.Errorf("required argument: query")
		}
		resp := search.Search(ctx, search.Request{
			Query: a.Query, Num: a.Num, Providers: []string{"index"},
		})
		return marshalSearch(resp), nil

	case "webx_doctor":
		hs := search.Diagnose(ctx, "")
		var b strings.Builder
		for _, h := range hs {
			mark := "ok  "
			if !h.OK {
				mark = "FAIL"
			}
			fmt.Fprintf(&b, "%s %-8s", mark, h.Name)
			if h.Results > 0 {
				fmt.Fprintf(&b, " %3d results", h.Results)
			}
			if h.Latency > 0 {
				fmt.Fprintf(&b, " %7s", h.Latency)
			}
			if h.Note != "" {
				b.WriteString("  " + h.Note)
			}
			b.WriteString("\n")
		}
		return b.String(), nil

	case "webx_scrape":
		var a struct {
			URL        string `json:"url"`
			MaxChars   int    `json:"max_chars"`
			Raw        bool   `json:"raw"`
			Browser    bool   `json:"browser"`
			Render     bool   `json:"render"`
			AutoRender bool   `json:"auto_render"`
			WaitFor    string `json:"wait_for"`
			Actions    string `json:"actions"`
			Fit        string `json:"fit"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.URL == "" {
			return "", fmt.Errorf("required argument: url")
		}
		doc, err := fetch.Fetch(ctx, fetch.FetchRequest{
			URL: a.URL, Raw: a.Raw, Browser: a.Browser,
			Render: a.Render || a.Actions != "", AutoRender: a.AutoRender,
			WaitFor: a.WaitFor, Actions: a.Actions, Fit: a.Fit,
		})
		if err != nil {
			return "", err
		}
		md := doc.Markdown
		if a.Fit != "" && doc.FitMarkdown != "" {
			md = doc.FitMarkdown
		}
		truncated := ""
		if a.MaxChars > 0 && len(md) > a.MaxChars {
			md = md[:a.MaxChars]
			truncated = " (truncated)"
		}
		head := fmt.Sprintf("# %s\n%s%s\n\n", doc.Title, doc.FinalURL, truncated)
		return head + md, nil

	case "webx_ask":
		var a struct {
			Question  string `json:"question"`
			Num       int    `json:"num"`
			MaxTokens int    `json:"max_tokens"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Question == "" {
			return "", fmt.Errorf("required argument: question")
		}
		if a.Num <= 0 {
			a.Num = 5
		}
		if a.MaxTokens <= 0 {
			a.MaxTokens = 3000
		}
		resp := search.Search(ctx, search.Request{
			Query: a.Question, Num: a.Num, Scrape: true, ScrapeChars: 8000,
		})
		return marshalAsk(a.Question, resp, a.MaxTokens*4), nil

	case "webx_map":
		var a struct {
			Domain string `json:"domain"`
			Limit  int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Domain == "" {
			return "", fmt.Errorf("required argument: domain")
		}
		if a.Limit <= 0 {
			a.Limit = 200
		}
		raw := a.Domain
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		base, err := url.Parse(raw)
		if err != nil {
			return "", err
		}
		urls, err := index.DiscoverURLs(ctx, base, a.Limit)
		if err != nil {
			return "", err
		}
		return strings.Join(urls, "\n"), nil

	case "webx_crawl":
		var a struct {
			URL   string `json:"url"`
			Goal  string `json:"goal"`
			Limit int    `json:"limit"`
			Depth int    `json:"depth"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.URL == "" {
			return "", fmt.Errorf("required argument: url")
		}
		if a.Limit <= 0 {
			a.Limit = 10
		}
		if a.Limit > 25 {
			a.Limit = 25 // MCP calls are synchronous — cap the crawl
		}
		res, err := fetch.Crawl(ctx, fetch.CrawlRequest{
			Start: a.URL, Query: a.Goal, Limit: a.Limit, Depth: a.Depth, SameHost: true,
		}, nil)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "crawl %s — %d fetched, %d relevant%s\n\n",
			a.URL, res.Fetched, res.Relevant,
			map[bool]string{true: " (stopped early — goal met)"}[res.StoppedEarly])
		for _, p := range res.Pages {
			mark := " "
			if p.Relevant {
				mark = "★"
			}
			fmt.Fprintf(&b, "%s %.2f  %s\n   %s\n   %s\n\n", mark, p.Relevance, p.FinalURL, p.Title, p.Excerpt)
		}
		return b.String(), nil

	case "webx_extract":
		var a struct {
			URL    string         `json:"url"`
			Schema map[string]any `json:"schema"`
			Prompt string         `json:"prompt"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.URL == "" {
			return "", fmt.Errorf("required argument: url")
		}
		if a.Schema == nil {
			a.Schema = map[string]any{"type": "object"}
		}
		res, err := fetch.Extract(ctx, fetch.ExtractRequest{
			URL: a.URL, Schema: a.Schema, Prompt: a.Prompt,
		})
		if err != nil {
			return "", err
		}
		if !res.OK {
			return "", fmt.Errorf("extract failed (%s): %s", res.Model, res.Err)
		}
		out, _ := json.MarshalIndent(res.Data, "", "  ")
		return string(out), nil

	case "webx_diff":
		var a struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.URL == "" {
			return "", fmt.Errorf("required argument: url")
		}
		idx, err := index.Open(index.DefaultPathEnv())
		if err != nil {
			return "", err
		}
		defer idx.Close()
		old, err := idx.Get(ctx, a.URL)
		if err != nil {
			return "", fmt.Errorf("no indexed version of %s — index it first (webx index)", a.URL)
		}
		doc, err := fetch.Fetch(ctx, fetch.FetchRequest{URL: a.URL})
		if err != nil {
			return "", err
		}
		added, removed := multisetDiff(old.Body, doc.Markdown)
		var b strings.Builder
		fmt.Fprintf(&b, "%s\nindexed %s → now: +%d −%d lines\n\n",
			a.URL, old.FetchedAt.Format("2006-01-02 15:04"), len(added), len(removed))
		for _, l := range removed {
			fmt.Fprintf(&b, "- %s\n", l)
		}
		for _, l := range added {
			fmt.Fprintf(&b, "+ %s\n", l)
		}
		return b.String(), nil

	case "webx_research":
		var a struct {
			Query        string         `json:"query"`
			MaxSources   int            `json:"max_sources"`
			OutputSchema map[string]any `json:"output_schema"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Query == "" {
			return "", fmt.Errorf("required argument: query")
		}
		rep, err := research.Run(ctx, a.Query,
			research.Options{MaxSources: a.MaxSources, Schema: a.OutputSchema}, nil)
		if err != nil {
			return "", err
		}
		return rep.Report, nil

	case "webx_verify":
		var a struct {
			Claim      string `json:"claim"`
			MaxSources int    `json:"max_sources"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Claim == "" {
			return "", fmt.Errorf("required argument: claim")
		}
		v, err := research.Verify(ctx, a.Claim, a.MaxSources, nil)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "verdict: %s", v.Verdict)
		if v.Confidence > 0 {
			fmt.Fprintf(&b, " (%.0f%%)", v.Confidence*100)
		}
		if v.Reasoning != "" {
			fmt.Fprintf(&b, "\nreasoning: %s", v.Reasoning)
		}
		fmt.Fprintln(&b, "\nsources:")
		for i, s := range v.Sources {
			fmt.Fprintf(&b, "[%d] %s — %s\n", i+1, s.Title, s.URL)
		}
		return b.String(), nil

	case "webx_watch":
		var a struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.URL == "" {
			return "", fmt.Errorf("required argument: url")
		}
		idx, err := index.Open(index.DefaultPathEnv())
		if err != nil {
			return "", err
		}
		defer idx.Close()
		old, oerr := idx.Get(ctx, a.URL)
		doc, err := fetch.Fetch(ctx, fetch.FetchRequest{URL: a.URL})
		if err != nil {
			return "", err
		}
		if oerr != nil {
			// First sight — index the baseline, report as "new".
			_ = idx.Put(ctx, index.Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown})
			return "baseline indexed — future calls report changes", nil
		}
		added, removed := multisetDiff(old.Body, doc.Markdown)
		if len(added) == 0 && len(removed) == 0 {
			return "unchanged", nil
		}
		_ = idx.Put(ctx, index.Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown})
		return fmt.Sprintf("CHANGED +%d −%d lines", len(added), len(removed)), nil

	case "webx_similar":
		var a struct {
			URL string `json:"url"`
			Num int    `json:"num"`
			Web bool   `json:"web"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.URL == "" {
			return "", fmt.Errorf("required argument: url")
		}
		if a.Num <= 0 {
			a.Num = 10
		}
		idx, err := index.Open(index.DefaultPathEnv())
		if err != nil {
			return "", err
		}
		defer idx.Close()
		if !a.Web {
			hits, err := index.Similar(ctx, idx, a.URL, a.Num)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			for _, h := range hits {
				fmt.Fprintf(&b, "%.1f  %s\n   %s\n", h.Score, h.Title, h.URL)
			}
			if b.Len() == 0 {
				return "no similar pages — index may be too thin (webx index/seed)", nil
			}
			return b.String(), nil
		}
		// Web mode — distill the page's signature, search live providers.
		page, err := idx.Get(ctx, a.URL)
		if err != nil {
			doc, ferr := fetch.Fetch(ctx, fetch.FetchRequest{URL: a.URL})
			if ferr != nil {
				return "", ferr
			}
			page = &index.Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown}
			_ = idx.Put(ctx, *page)
		}
		terms := index.SignatureTerms(page)
		if terms == "" {
			return "", fmt.Errorf("couldn't distill a signature from %s", a.URL)
		}
		resp := search.Search(ctx, search.Request{Query: terms, Num: a.Num + 4})
		return marshalSearch(resp), nil

	case "webx_wayback":
		var a struct {
			URL string `json:"url"`
			TS  string `json:"ts"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.URL == "" {
			return "", fmt.Errorf("required argument: url")
		}
		body, _, ts, err := index.FetchWayback(ctx, a.URL, a.TS)
		if err != nil {
			return "", err
		}
		text := strings.TrimSpace(string(body))
		if len(text) > 60000 {
			text = text[:60000] + "\n\n(truncated)"
		}
		return fmt.Sprintf("# %s\nwayback snapshot %s\n\n%s", a.URL, ts, text), nil

	case "webx_llms":
		var a struct {
			Host  string `json:"host"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Host == "" {
			return "", fmt.Errorf("required argument: host")
		}
		if a.Limit <= 0 {
			a.Limit = 200
		}
		host := strings.TrimPrefix(strings.TrimPrefix(a.Host, "https://"), "http://")
		host = strings.TrimSuffix(host, "/")
		idx, err := index.Open(index.DefaultPathEnv())
		if err != nil {
			return "", err
		}
		defer idx.Close()
		pages, err := idx.ListHost(ctx, host, a.Limit)
		if err != nil {
			return "", err
		}
		if len(pages) == 0 {
			return "", fmt.Errorf("no pages for %s in the index — run webx index %s first", host, host)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n> Generated by webx from a local index (%d pages).\n\n", host, len(pages))
		bySection := map[string][]index.Page{}
		var order []string
		for _, p := range pages {
			section := "Pages"
			if u, err := url.Parse(p.URL); err == nil {
				segs := strings.Split(strings.Trim(u.Path, "/"), "/")
				if segs[0] != "" {
					section = segs[0]
				}
			}
			if _, seen := bySection[section]; !seen {
				order = append(order, section)
			}
			bySection[section] = append(bySection[section], p)
		}
		for _, sec := range order {
			fmt.Fprintf(&b, "## %s\n\n", sec)
			for _, p := range bySection[sec] {
				title := p.Title
				if title == "" {
					title = p.URL
				}
				blurb := firstLine(p.Body, 120)
				if blurb != "" {
					fmt.Fprintf(&b, "- [%s](%s): %s\n", title, p.URL, blurb)
				} else {
					fmt.Fprintf(&b, "- [%s](%s)\n", title, p.URL)
				}
			}
			b.WriteString("\n")
		}
		return b.String(), nil

	case "webx_answer":
		var a struct {
			Query string `json:"query"`
			Num   int    `json:"num"`
			LLM   *bool  `json:"llm"`
		}
		if err := json.Unmarshal(args, &a); err != nil || a.Query == "" {
			return "", fmt.Errorf("required argument: query")
		}
		if a.Num <= 0 {
			a.Num = 5
		}
		resp := search.Search(ctx, search.Request{
			Query: a.Query, Num: a.Num, Scrape: true, ScrapeChars: 8000,
		})
		evidence := marshalAsk(a.Query, resp, 12000)
		useLLM := a.LLM == nil || *a.LLM
		if !useLLM {
			return evidence, nil
		}
		sys := "You answer questions using only the numbered web sources provided. " +
			"Cite claims with [n] markers matching the source numbers. Be precise; " +
			"say when sources disagree or are silent on part of the question."
		user := fmt.Sprintf("Question: %s\n\nSources:\n%s", a.Query, evidence)
		text, err := fetch.LLMChat(ctx, sys, user, false)
		if err != nil {
			return evidence + "\n\n(llm unavailable: " + err.Error() + ")", nil
		}
		return text + "\n\n---\n" + evidence, nil

	case "webx_batch":
		var a struct {
			URLs     []string `json:"urls"`
			MaxChars int      `json:"max_chars"`
			Fit      string   `json:"fit"`
		}
		if err := json.Unmarshal(args, &a); err != nil || len(a.URLs) == 0 {
			return "", fmt.Errorf("required argument: urls[]")
		}
		if a.MaxChars <= 0 {
			a.MaxChars = 8000
		}
		if len(a.URLs) > 20 {
			a.URLs = a.URLs[:20] // MCP calls are synchronous — bound the batch
		}
		type res struct {
			url, body string
			err       error
		}
		out := make([]res, len(a.URLs))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for i, u := range a.URLs {
			wg.Add(1)
			go func(i int, u string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				doc, err := fetch.Fetch(ctx, fetch.FetchRequest{URL: u, Fit: a.Fit})
				out[i].url = u
				if err != nil {
					out[i].err = err
					return
				}
				md := doc.Markdown
				if a.Fit != "" && doc.FitMarkdown != "" {
					md = doc.FitMarkdown
				}
				if len(md) > a.MaxChars {
					md = md[:a.MaxChars] + "…"
				}
				out[i].body = fmt.Sprintf("# %s\n%s\n\n%s", doc.Title, doc.FinalURL, md)
			}(i, u)
		}
		wg.Wait()
		var b strings.Builder
		for _, r := range out {
			if r.err != nil {
				fmt.Fprintf(&b, "## %s\nERROR: %s\n\n---\n\n", r.url, r.err)
				continue
			}
			fmt.Fprintf(&b, "%s\n\n---\n\n", r.body)
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("unknown tool %q", name)
}

// firstLine returns the first non-empty content line, de-markdowned and
// truncated — a one-line blurb for llms.txt entries.
func firstLine(body string, max int) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "#*>- ")
		line = strings.NewReplacer("*", "", "`", "", "&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(line)
		if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "!") {
			continue
		}
		if len(line) > max {
			line = line[:max-1] + "…"
		}
		return line
	}
	return ""
}

// multisetDiff — lines only in new (added) / only in old (removed).
func multisetDiff(old, new string) (added, removed []string) {
	count := map[string]int{}
	for _, l := range strings.Split(old, "\n") {
		count[l]++
	}
	for _, l := range strings.Split(new, "\n") {
		if count[l] > 0 {
			count[l]--
		} else {
			added = append(added, l)
		}
	}
	for l, n := range count {
		for ; n > 0; n-- {
			removed = append(removed, l)
		}
	}
	return added, removed
}

func marshalSearch(resp *search.Response) string {
	var b strings.Builder
	for name, e := range resp.Errors {
		fmt.Fprintf(&b, "[provider %s failed: %s]\n", name, e)
	}
	for i, r := range resp.Results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", r.Snippet)
		}
		for _, h := range r.Highlights {
			fmt.Fprintf(&b, "   > %s\n", h)
		}
		fmt.Fprintf(&b, "   via %s\n\n", strings.Join(r.Sources, "+"))
	}
	if b.Len() == 0 {
		return "no results"
	}
	return b.String()
}

func marshalAsk(question string, resp *search.Response, charBudget int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", question)
	n := 0
	for i, r := range resp.Results {
		if len(r.Highlights) == 0 && r.Content == "" && r.Snippet == "" {
			continue
		}
		var sec strings.Builder
		fmt.Fprintf(&sec, "## [%d] %s\n%s\n\n", i+1, r.Title, r.URL)
		if len(r.Highlights) > 0 {
			for _, h := range r.Highlights {
				fmt.Fprintf(&sec, "> %s\n\n", h)
			}
		} else if r.Content != "" {
			c := r.Content
			if len(c) > 1500 {
				c = c[:1500] + "…"
			}
			sec.WriteString(c + "\n\n")
		} else {
			fmt.Fprintf(&sec, "> %s\n\n", r.Snippet)
		}
		if b.Len()+sec.Len() > charBudget {
			break
		}
		b.WriteString(sec.String())
		n++
	}
	for name, e := range resp.Errors {
		fmt.Fprintf(&b, "[provider %s failed: %s]\n", name, e)
	}
	if n == 0 {
		return "no usable sources found"
	}
	return b.String()
}

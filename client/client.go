// Package client is the official Go SDK for webx — a thin typed wrapper over
// the webx/webxd HTTP API. The CLI uses it for --api mode; import it directly
// to drive a server from Go:
//
//	import "github.com/kasyap1234/webx/client"
//
//	c := client.New("http://localhost:8080")   // WEBX_API_KEY env is auto-sent
//	doc, err := c.Scrape(ctx, fetch.FetchRequest{URL: "https://example.com"})
package client

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

	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/search"
)

// Client is a thin JSON-over-HTTP wrapper for the serve routes.
type Client struct {
	Base string
	HTTP *http.Client
}

func New(base string) *Client {
	return &Client{
		Base: strings.TrimRight(base, "/"),
		HTTP: &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) post(ctx context.Context, path string, in, out any) error {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.auth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error != "" {
			return fmt.Errorf("%s: %s", resp.Status, e.Error)
		}
		return fmt.Errorf("%s", resp.Status)
	}
	return json.Unmarshal(raw, out)
}

// auth attaches the bearer key when WEBX_API_KEY is set — matches serve's
// auth middleware so --api just works against a secured server.
func (c *Client) auth(req *http.Request) {
	if key := os.Getenv("WEBX_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return err
	}
	c.auth(req)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Scrape calls POST /scrape and unwraps the document.
func (c *Client) Scrape(ctx context.Context, req fetch.FetchRequest) (*fetch.Document, error) {
	var out struct {
		Data fetch.Document `json:"data"`
	}
	body := map[string]any{
		"url": req.URL, "browser": req.Browser, "render": req.Render,
		"auto_render": req.AutoRender, "session": req.Session,
		"wait_for": req.WaitFor, "scrolls": req.Scrolls,
		"actions": req.Actions, "screenshot": req.Screenshot, "proxy": req.Proxy,
		"stealth": req.Stealth, "profile": req.Profile,
		"block_ads": req.BlockTrackers, "text_mode": req.BlockMedia,
		"mobile": req.Mobile, "locale": req.Locale, "timezone": req.Timezone,
		"capture_network": req.NetworkCapture, "capture_console": req.ConsoleCapture,
		"skip_tls_verification": req.SkipTLS,
		"page_session":          req.PageSession, "pierce_dom": req.PierceDOM,
		"scroll_selector": req.ScrollSelector, "scroll_by": req.ScrollBy,
		"fit":     req.Fit,
		"formats": clientFormats(req),
		"engine":  req.RenderEngine,
		"lang":    req.TranscriptLang,
		// CookieFile is a local path — the remote server can't read it,
		// so the client ships the file's contents as `cookies` text.
		"cookies":     clientCookieText(req),
		"retry_after": req.RetryAfter,
	}
	if req.ShotFullPage != nil || req.ShotQuality > 0 || req.ViewportW > 0 || req.ViewportH > 0 {
		shot := map[string]any{}
		if req.ShotFullPage != nil {
			shot["full_page"] = *req.ShotFullPage
		}
		if req.ShotQuality > 0 {
			shot["quality"] = req.ShotQuality
		}
		if req.ViewportW > 0 || req.ViewportH > 0 {
			shot["viewport"] = map[string]int{"width": req.ViewportW, "height": req.ViewportH}
		}
		body["screenshot_options"] = shot
	}
	err := c.post(ctx, "/scrape", body, &out)
	return &out.Data, err
}

// clientCookieText inlines a local cookies.txt for the remote path —
// the API contract is cookie *content*, never a server-side path.
func clientCookieText(req fetch.FetchRequest) string {
	if req.CookieText != "" {
		return req.CookieText
	}
	if req.CookieFile != "" {
		if b, err := os.ReadFile(req.CookieFile); err == nil {
			return string(b)
		}
	}
	return ""
}

// clientFormats maps FetchRequest wants onto the serve `formats` array so
// pdf/mhtml/images/html over --api work like the local path.
func clientFormats(req fetch.FetchRequest) []string {
	var fmts []string
	if req.WantPDF {
		fmts = append(fmts, "pdf")
	}
	if req.WantMHTML {
		fmts = append(fmts, "mhtml")
	}
	if req.WantHTML {
		fmts = append(fmts, "html")
	}
	if req.WantImages {
		fmts = append(fmts, "images")
	}
	if req.WantBrand {
		fmts = append(fmts, "branding")
	}
	if req.WantChunks {
		fmts = append(fmts, "chunks")
	}
	if req.WantA11y {
		fmts = append(fmts, "a11y")
	}
	if req.WantAgentReady {
		fmts = append(fmts, "agent_ready")
	}
	if req.WantLinks {
		fmts = append(fmts, "links")
	}
	if req.Summary {
		fmts = append(fmts, "summary")
	}
	if req.Transcript {
		fmts = append(fmts, "transcript")
	}
	if req.RedactPII {
		fmts = append(fmts, "redact_pii")
	}
	return fmts
}

// Search calls POST /search.
func (c *Client) Search(ctx context.Context, req search.Request) (*search.Response, error) {
	var out struct {
		Data       []search.Result   `json:"data"`
		Errors     map[string]string `json:"errors"`
		ProviderMs map[string]int64  `json:"provider_ms"`
		CacheHit   bool              `json:"cache_hit"`
	}
	body := map[string]any{
		"query": req.Query, "limit": req.Num, "providers": req.Providers,
		"scrape": req.Scrape, "rerank": req.Rerank, "site": req.Site,
		"domains": req.Domains, "exclude_domains": req.ExcludeDomains,
		"topic": req.Topic, "lang": req.Lang, "exact": req.Exact,
		"scrape_chars": req.ScrapeChars, "highlights_only": req.HighlightsOnly,
		"semantic": req.Semantic, "render": req.Render,
		"auto_render": req.AutoRender, "browser": req.Browser,
		"session": req.Session, "depth": req.Depth,
		"sources": req.Sources, "subpages": req.Subpages,
		"subpage_target": req.SubpageTarget, "collection": req.Collection,
	}
	if !req.After.IsZero() {
		body["after"] = req.After.Format(time.RFC3339)
	}
	if !req.Before.IsZero() {
		body["before"] = req.Before.Format(time.RFC3339)
	}
	err := c.post(ctx, "/search", body, &out)
	if err != nil {
		return nil, err
	}
	return &search.Response{
		Results: out.Data, Errors: out.Errors,
		ProviderMs: out.ProviderMs, CacheHit: out.CacheHit,
	}, nil
}

// AnswerRequest drives POST /answer — Exa's /answer parity.
type AnswerRequest struct {
	Query     string
	Num       int      // sources to cite (default 5)
	Providers []string // restrict the fused-search set
	Domains   []string // Exa includeDomains
	LLM       bool     // synthesize via WEBX_LLM_* (answer_type "llm" on success)
	MaxTokens int      // evidence budget
}

// Answer is the grounded-answer response — always carries extractive
// evidence + numbered citations; Answer is non-empty only when an LLM
// actually synthesized (AnswerType "llm").
type Answer struct {
	Text       string            `json:"answer"`
	Type       string            `json:"answer_type"` // llm|extractive
	EvidenceMD string            `json:"evidence_md"`
	Citations  []map[string]any  `json:"citations"`
	LLMError   string            `json:"llm_error"`
	Errors     map[string]string `json:"errors"`
	ProviderMs map[string]int64  `json:"provider_ms"`
	CacheHit   bool              `json:"cache_hit"`
}

// Answer calls POST /answer — grounded, cited answers for agents.
func (c *Client) Answer(ctx context.Context, req AnswerRequest) (*Answer, error) {
	var out Answer
	err := c.post(ctx, "/answer", map[string]any{
		"query": req.Query, "num": req.Num, "providers": req.Providers,
		"domains": req.Domains, "llm": req.LLM, "max_tokens": req.MaxTokens,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// storePage mirrors internal/store.JobPage without importing it — the client
// only needs the wire shape.
type storePage struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// Jobs lists recent jobs from GET /jobs.
func (c *Client) Jobs(ctx context.Context) ([]map[string]any, error) {
	var out struct {
		Jobs []map[string]any `json:"jobs"`
	}
	err := c.get(ctx, "/jobs", &out)
	return out.Jobs, err
}

// Cancel cancels a queued/running job via POST /crawl/{id}/cancel.
func (c *Client) Cancel(ctx context.Context, id string) error {
	return c.post(ctx, "/crawl/"+id+"/cancel", map[string]any{}, &map[string]any{})
}

// Batch submits POST /batch/scrape and polls to completion — same lifecycle
// as Crawl since both surface via GET /crawl/{id}.
func (c *Client) Batch(ctx context.Context, urls []string, opts map[string]any, onTick func(string, int, int)) ([]storePage, error) {
	var started struct {
		ID string `json:"id"`
	}
	if err := c.post(ctx, "/batch/scrape", map[string]any{
		"urls": urls, "options": opts,
	}, &started); err != nil {
		return nil, err
	}
	return c.poll(ctx, started.ID, onTick)
}

// CrawlRequest is the full /crawl shape — Crawl's positional form stays
// for back-compat; CrawlOpts carries the newer levers.
type CrawlRequest struct {
	URL, Goal     string
	Limit, Depth  int
	Semantic      bool
	Concurrency   int      // parallel page fetches (server-side default 4)
	IncludePaths  []string // regex allow-list on URL paths
	ExcludePaths  []string
	WebhookURL    string
	WebhookSecret string // HMAC-SHA256 — deliveries arrive signed (X-Webx-Signature)
}

// CrawlOpts submits a crawl job then polls to completion.
func (c *Client) CrawlOpts(ctx context.Context, r CrawlRequest, onTick func(string, int, int)) ([]storePage, error) {
	var started struct {
		ID string `json:"id"`
	}
	if err := c.post(ctx, "/crawl", map[string]any{
		"url": r.URL, "goal": r.Goal, "limit": r.Limit, "depth": r.Depth,
		"semantic": r.Semantic, "concurrency": r.Concurrency,
		"include_paths": r.IncludePaths, "exclude_paths": r.ExcludePaths,
		"webhook_url": r.WebhookURL, "webhook_secret": r.WebhookSecret,
	}, &started); err != nil {
		return nil, err
	}
	return c.poll(ctx, started.ID, onTick)
}

// Crawl calls POST /crawl then polls GET /crawl/{id} until done/failed —
// refactored onto poll so batch shares the lifecycle.
func (c *Client) Crawl(ctx context.Context, url, goal string, limit, depth int, semantic bool, onTick func(string, int, int)) ([]storePage, error) {
	return c.CrawlOpts(ctx, CrawlRequest{
		URL: url, Goal: goal, Limit: limit, Depth: depth, Semantic: semantic,
	}, onTick)
}

// JobStatus is the GET /crawl/{id} shape — Result carries the payload for
// research/extract/agent jobs; Pages is the current page window.
type JobStatus struct {
	Status    string      `json:"status"`
	Total     int         `json:"total"`
	Completed int         `json:"completed"`
	Error     string      `json:"error,omitempty"`
	Result    string      `json:"result,omitempty"`
	Data      []storePage `json:"data"`
}

// JobStatus reads one job's state — also the polling primitive for
// async extract/research/agent handles.
func (c *Client) JobStatus(ctx context.Context, id string) (*JobStatus, error) {
	var st JobStatus
	if err := c.get(ctx, "/crawl/"+id, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// JobError is one failed page of a job.
type JobError struct {
	URL    string `json:"url"`
	Error  string `json:"error,omitempty"`
	Status int    `json:"status,omitempty"`
}

// JobErrors lists a job's failed pages — GET /crawl/{id}/errors.
func (c *Client) JobErrors(ctx context.Context, id string) ([]JobError, error) {
	var out struct {
		Errors []JobError `json:"errors"`
	}
	err := c.get(ctx, "/crawl/"+id+"/errors", &out)
	return out.Errors, err
}

// AgentRequest drives POST /agent — goal-only extraction.
type AgentRequest struct {
	Goal          string         `json:"goal"`
	URL           string         `json:"url,omitempty"`    // optional site constraint
	Schema        map[string]any `json:"schema,omitempty"` // optional structured output
	Limit         int            `json:"limit,omitempty"`  // candidate pages to read (default 5)
	Render        bool           `json:"render,omitempty"`
	Async         bool           `json:"async"` // queue as a job — StartAgent without polling
	WebhookURL    string         `json:"webhook_url,omitempty"`
	WebhookSecret string         `json:"webhook_secret,omitempty"`
}

// Agent runs POST /agent synchronously → {success, data:{…, sources[]}}.
func (c *Client) Agent(ctx context.Context, r AgentRequest) (map[string]any, error) {
	r.Async = false
	var out map[string]any
	err := c.post(ctx, "/agent", r, &out)
	return out, err
}

// StartAgent queues an agent job — poll via JobStatus (Result holds the
// extraction payload once status is "done").
func (c *Client) StartAgent(ctx context.Context, r AgentRequest) (string, error) {
	r.Async = true
	var out struct {
		ID string `json:"id"`
	}
	err := c.post(ctx, "/agent", r, &out)
	return out.ID, err
}

// ExtractRequest drives POST /extract — css is deterministic (zero LLM),
// schema+prompt is the LLM path, Type reads schema.org entities.
// URL may be a wildcard "example.com/*" (requires Async); URLs batches.
type ExtractRequest struct {
	URL           string         `json:"url,omitempty"`
	URLs          []string       `json:"urls,omitempty"`
	Schema        map[string]any `json:"schema,omitempty"`
	CSS           map[string]any `json:"css,omitempty"`
	Prompt        string         `json:"prompt,omitempty"`
	Type          string         `json:"type,omitempty"`
	Render        bool           `json:"render,omitempty"`
	Async         bool           `json:"async"`
	WebhookURL    string         `json:"webhook_url,omitempty"`
	WebhookSecret string         `json:"webhook_secret,omitempty"`
}

// Extract runs POST /extract synchronously. For async/wildcard work use
// StartExtract + JobStatus.
func (c *Client) Extract(ctx context.Context, r ExtractRequest) (map[string]any, error) {
	r.Async = false
	var out map[string]any
	err := c.post(ctx, "/extract", r, &out)
	return out, err
}

// StartExtract queues an extraction — wildcards and url-lists live here.
func (c *Client) StartExtract(ctx context.Context, r ExtractRequest) (string, error) {
	r.Async = true
	var out struct {
		ID string `json:"id"`
	}
	err := c.post(ctx, "/extract", r, &out)
	return out.ID, err
}

// ResearchRequest drives POST /research.
type ResearchRequest struct {
	Query         string
	OutputSchema  map[string]any `json:"output_schema"`
	MaxSources    int            `json:"max_sources"`
	Async         bool           `json:"async"`
	WebhookURL    string         `json:"webhook_url"`
	WebhookSecret string         `json:"webhook_secret"`
}

// Research runs POST /research synchronously → {data: Report}.
func (c *Client) Research(ctx context.Context, r ResearchRequest) (map[string]any, error) {
	r.Async = false
	var out map[string]any
	err := c.post(ctx, "/research", r, &out)
	return out, err
}

// StartResearch queues a research job — Result carries the report.
func (c *Client) StartResearch(ctx context.Context, r ResearchRequest) (string, error) {
	r.Async = true
	var out struct {
		ID string `json:"id"`
	}
	err := c.post(ctx, "/research", r, &out)
	return out.ID, err
}

// Map lists a site's URLs — POST /map.
func (c *Client) Map(ctx context.Context, url string, limit int) ([]string, error) {
	var out struct {
		Links []string `json:"links"`
	}
	err := c.post(ctx, "/map", map[string]any{"url": url, "limit": limit}, &out)
	return out.Links, err
}

// Similar finds pages like the given URL — POST /similar.
func (c *Client) Similar(ctx context.Context, url string, limit int, web bool) (map[string]any, error) {
	var out map[string]any
	err := c.post(ctx, "/similar", map[string]any{"url": url, "limit": limit, "web": web}, &out)
	return out, err
}

// Wayback lists captures (At empty) or fetches the nearest snapshot — POST /wayback.
func (c *Client) Wayback(ctx context.Context, url, at string) (map[string]any, error) {
	var out map[string]any
	err := c.post(ctx, "/wayback", map[string]any{"url": url, "at": at}, &out)
	return out, err
}

// Verify fact-checks a claim — POST /verify.
func (c *Client) Verify(ctx context.Context, claim string) (map[string]any, error) {
	var out map[string]any
	err := c.post(ctx, "/verify", map[string]any{"claim": claim}, &out)
	return out, err
}

// Ready probes readiness — GET /ready (store ping + render reachability).
func (c *Client) Ready(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.get(ctx, "/ready", &out)
	return out, err
}

// Version reports the build — GET /version.
func (c *Client) Version(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.get(ctx, "/version", &out)
	return out, err
}

// poll waits on a job id until done/failed/cancelled.
func (c *Client) poll(ctx context.Context, id string, onTick func(string, int, int)) ([]storePage, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
		var st struct {
			Status    string      `json:"status"`
			Total     int         `json:"total"`
			Completed int         `json:"completed"`
			Error     string      `json:"error"`
			Data      []storePage `json:"data"`
		}
		if err := c.get(ctx, "/crawl/"+id, &st); err != nil {
			return nil, err
		}
		if onTick != nil {
			onTick(st.Status, st.Completed, st.Total)
		}
		switch st.Status {
		case "done":
			return st.Data, nil
		case "failed", "cancelled":
			return st.Data, fmt.Errorf("job %s: %s", st.Status, st.Error)
		}
	}
}

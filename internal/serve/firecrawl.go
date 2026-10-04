package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kasyap1234/webx/internal/store"
	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/index"
	"github.com/kasyap1234/webx/web/render"
	"github.com/kasyap1234/webx/web/search"
)

// ── Firecrawl /v2 byte-compat ────────────────────────────────────────────────
// Field names match https://docs.firecrawl.dev/api-reference so Firecrawl
// SDKs can point at a self-hosted webx by only changing base_url + api_key.

// v2ScrapeOptions is the shared options block Firecrawl nests under
// scrapeOptions on crawl/batch/search.
type v2ScrapeOptions struct {
	Formats          v2Formats  `json:"formats"`
	OnlyMainContent  *bool      `json:"onlyMainContent"`
	IncludeTags      []string   `json:"includeTags"`
	ExcludeTags      []string   `json:"excludeTags"`
	WaitFor          int        `json:"waitFor"` // ms — mapped to wait_for via sleep action
	Timeout          int        `json:"timeout"` // ms
	Actions          *v2Actions `json:"actions"`
	MaxAge           int64      `json:"maxAge"` // ms
	Proxy            string     `json:"proxy"`  // "basic"|"stealth"|"auto" — mapped to render escalation
	StoreInCache     *bool      `json:"storeInCache"`
	Parsers          []any      `json:"parsers"`
	Mobile           bool       `json:"mobile"`
	SkipTLS          bool       `json:"skipTlsVerification"`
	RemoveBase64Imgs bool       `json:"removeBase64Images"`
	Location         *struct {
		Country   string   `json:"country"`
		Languages []string `json:"languages"`
	} `json:"location"`
}

// v2Formats accepts Firecrawl's mixed formats array — bare strings
// ("markdown", "screenshot@fullPage") and typed objects
// ({"type":"screenshot","fullPage":true,"quality":80,"viewport":{w,h}},
//
//	{"type":"json","schema":{...},"prompt":"..."}).
type v2Format struct {
	name   string
	shot   *shotOpts
	schema map[string]any
	prompt string
}
type v2Formats []v2Format

func (fs *v2Formats) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		var s string // tolerate formats:"markdown"
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*fs = v2Formats{{name: strings.ToLower(s)}}
		return nil
	}
	for _, r := range raw {
		var name string
		if err := json.Unmarshal(r, &name); err == nil {
			*fs = append(*fs, v2Format{name: strings.ToLower(name)})
			continue
		}
		var obj struct {
			Type     string `json:"type"`
			FullPage *bool  `json:"fullPage"`
			Quality  int    `json:"quality"`
			Viewport *struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			} `json:"viewport"`
			Schema map[string]any `json:"schema"`
			Prompt string         `json:"prompt"`
		}
		if err := json.Unmarshal(r, &obj); err != nil {
			return err
		}
		f := v2Format{name: strings.ToLower(obj.Type), schema: obj.Schema, prompt: obj.Prompt}
		if f.name == "screenshot" {
			f.shot = &shotOpts{FullPage: obj.FullPage, Quality: obj.Quality}
			if obj.Viewport != nil {
				f.shot.Viewport.Width, f.shot.Viewport.Height = obj.Viewport.Width, obj.Viewport.Height
			}
		}
		*fs = append(*fs, f)
	}
	return nil
}

func (fs v2Formats) has(name string) bool {
	for _, f := range fs {
		if f.name == name {
			return true
		}
	}
	return false
}

func (fs v2Formats) shot() *shotOpts {
	for _, f := range fs {
		if f.name == "screenshot" && f.shot != nil {
			return f.shot
		}
	}
	return nil
}

// jsonSpec returns the schema/prompt for a {"type":"json"} format object.
func (fs v2Formats) jsonSpec() (map[string]any, string, bool) {
	for _, f := range fs {
		if f.name == "json" {
			return f.schema, f.prompt, true
		}
	}
	return nil, "", false
}

// v2Actions mirrors Firecrawl's structured actions object → our DSL.
type v2Actions struct {
	Waits []struct {
		Milliseconds int `json:"milliseconds"`
	} `json:"-"`
	// Firecrawl actions arrive as an array of typed objects:
	// [{"type":"wait","milliseconds":500},{"type":"click","selector":"#x"},
	//  {"type":"write","text":"hi","selector":"#q"},{"type":"press","key":"Enter"},
	//  {"type":"scroll","direction":"down"},{"type":"screenshot"},
	//  {"type":"executeJavascript","script":"..."},{"type":"pdf"}]
	raw []map[string]any
}

// UnmarshalJSON accepts Firecrawl's action array form.
func (a *v2Actions) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &a.raw)
}

// toDSL converts structured actions to our pipe DSL.
func (a *v2Actions) toDSL() string {
	var parts []string
	for _, act := range a.raw {
		typ, _ := act["type"].(string)
		sel, _ := act["selector"].(string)
		switch typ {
		case "wait":
			if sel != "" {
				parts = append(parts, "wait:"+sel)
			} else if ms, ok := act["milliseconds"].(float64); ok && ms > 0 {
				parts = append(parts, "sleep:"+itoa(int(ms)))
			}
		case "click":
			parts = append(parts, "click:"+sel)
		case "write":
			text, _ := act["text"].(string)
			parts = append(parts, "type:"+sel+"="+text)
		case "press":
			key, _ := act["key"].(string)
			parts = append(parts, "press:"+strings.ToLower(key))
		case "scroll":
			parts = append(parts, "scroll")
		case "scrape", "screenshot":
			parts = append(parts, "screenshot")
		case "executeJavascript":
			script, _ := act["script"].(string)
			parts = append(parts, "eval:"+script)
		}
	}
	return strings.Join(parts, " | ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// toFetchRequest maps v2 options onto our internal fetch request.
func (o *v2ScrapeOptions) toFetchRequest() fetch.FetchRequest {
	if o == nil {
		return fetch.FetchRequest{}
	}
	wantShot := o.Formats.has("screenshot") || o.Formats.has("screenshot@fullpage")
	actions := ""
	if o.Actions != nil {
		actions = o.Actions.toDSL()
	}
	// waitFor(ms) → a leading sleep action so content has time to load
	if o.WaitFor > 0 {
		actions = "sleep:" + itoa(o.WaitFor) + orEmpty(actions)
	}
	render := actions != "" || wantShot ||
		o.Proxy == "stealth" || o.Proxy == "auto"
	req := fetch.FetchRequest{
		Render:           render,
		Actions:          actions,
		Screenshot:       wantShot,
		Timeout:          time.Duration(o.Timeout) * time.Millisecond,
		IncludeSelectors: o.IncludeTags,
		ExcludeSelectors: o.ExcludeTags,
		WantHTML:         o.Formats.has("html") || o.Formats.has("rawhtml"),
		WantLinks:        o.Formats.has("links"),
		Summary:          o.Formats.has("summary"),
		Fit:              "",
	}
	o.Formats.shot().shotFields(&req)
	return req
}

func orEmpty(s string) string {
	if s == "" {
		return ""
	}
	return " | " + s
}

// v2Doc reshapes a Document into Firecrawl's data object — field names are
// the compat contract.
func v2Doc(doc *fetch.Document, fmts map[string]bool) map[string]any {
	data := map[string]any{
		"markdown": doc.Markdown,
		"metadata": map[string]any{
			"title":      doc.Title,
			"language":   doc.Language,
			"sourceURL":  doc.FinalURL,
			"statusCode": doc.StatusCode,
			"error":      nil,
		},
	}
	if doc.Excerpt != "" {
		data["metadata"].(map[string]any)["description"] = doc.Excerpt
	}
	if fmts["html"] || fmts["rawhtml"] {
		data["html"] = doc.HTML
	}
	if fmts["links"] {
		data["links"] = doc.Links
	}
	if fmts["screenshot"] || fmts["screenshot@fullpage"] {
		data["screenshot"] = doc.ScreenshotB64
	}
	if fmts["summary"] {
		data["summary"] = doc.Summary
	}
	if doc.FitMarkdown != "" {
		data["fitMarkdown"] = doc.FitMarkdown
	}
	return data
}

func fmtSet(formats v2Formats) map[string]bool {
	m := map[string]bool{}
	for _, f := range formats {
		m[f.name] = true
	}
	if len(m) == 0 {
		m["markdown"] = true
	}
	return m
}

// v2Scrape handles POST /v2/scrape.
func (s *Server) v2Scrape(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
		v2ScrapeOptions
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {url}")
		return
	}
	if err := s.guard.CheckURL(req.URL); err != nil {
		writeErr(w, http.StatusForbidden, "target refused: "+err.Error())
		return
	}
	opts := req.v2ScrapeOptions

	// maxAge — serve the indexed snapshot when fresh.
	if opts.MaxAge > 0 {
		if idx := s.index(); idx != nil {
			if page, err := idx.Get(r.Context(), req.URL); err == nil &&
				time.Since(page.FetchedAt).Milliseconds() < opts.MaxAge {
				data := map[string]any{
					"markdown": page.Body,
					"metadata": map[string]any{
						"title": page.Title, "sourceURL": page.URL,
						"cachedAt":   page.FetchedAt.UTC().Format("2006-01-02T15:04:05Z"),
						"statusCode": 200,
					},
				}
				writeJSON(w, map[string]any{"success": true, "data": data, "warning": "cached"})
				return
			}
		}
	}

	freq := opts.toFetchRequest()
	freq.URL = req.URL
	if req.Actions != nil {
		if _, err := render.ParseActions(freq.Actions); err != nil {
			writeErr(w, http.StatusBadRequest, "actions: "+err.Error())
			return
		}
	}
	doc, err := fetchFn(r.Context(), freq)
	if err != nil {
		writeFetchErr(w, err)
		return
	}
	data := v2Doc(doc, fmtSet(opts.Formats))
	// {"type":"json","schema":..,"prompt":..} → LLM extraction on the doc we
	// already fetched.
	if schema, prompt, ok := opts.Formats.jsonSpec(); ok {
		res, err := fetch.Extract(r.Context(), fetch.ExtractRequest{
			URL: doc.FinalURL, Markdown: doc.Markdown, Title: doc.Title,
			Schema: schema, Prompt: prompt,
		})
		if err != nil {
			data["metadata"].(map[string]any)["error"] = "json format: " + err.Error()
		} else {
			data["json"] = res.Data
		}
	}
	writeJSON(w, map[string]any{"success": true, "data": data})
}

// v2Search handles POST /v2/search — {query, limit, sources, scrapeOptions}
// → {data:{web:[...]}}.
func (s *Server) v2Search(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query         string           `json:"query"`
		Limit         int              `json:"limit"`
		Sources       []any            `json:"sources"`
		Country       string           `json:"country"`
		Location      string           `json:"location"`
		Timeout       int              `json:"timeout"`
		ScrapeOptions *v2ScrapeOptions `json:"scrapeOptions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Query == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {query}")
		return
	}
	resp := searchFn(r.Context(), search.Request{
		Query: req.Query, Num: req.Limit,
	})
	web := make([]map[string]any, 0, len(resp.Results))
	for i, res := range resp.Results {
		item := map[string]any{
			"title":       res.Title,
			"url":         res.URL,
			"description": res.Snippet,
			"position":    i + 1,
		}
		// Firecrawl inline-scrapes when scrapeOptions asks for it.
		if req.ScrapeOptions != nil && len(req.ScrapeOptions.Formats) > 0 {
			freq := req.ScrapeOptions.toFetchRequest()
			freq.URL = res.URL
			if doc, err := fetchFn(r.Context(), freq); err == nil {
				d := v2Doc(doc, fmtSet(req.ScrapeOptions.Formats))
				delete(d, "metadata")
				for k, v := range d {
					item[k] = v
				}
			}
		}
		web = append(web, item)
	}
	writeJSON(w, map[string]any{"success": true, "data": map[string]any{"web": web}})
}

// v2Map handles POST /v2/map — {url, search?, limit} → {links:[{url,title?}]}.
func (s *Server) v2Map(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL               string `json:"url"`
		Search            string `json:"search"`
		Limit             int    `json:"limit"`
		IncludeSubdomains bool   `json:"includeSubdomains"`
		IgnoreSitemap     bool   `json:"ignoreSitemap"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {url}")
		return
	}
	u := req.URL
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	base, err := url.Parse(u)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	urls, err := index.DiscoverURLs(r.Context(), base, req.Limit)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	links := make([]map[string]any, 0, len(urls))
	for _, l := range urls {
		if req.Search != "" && !strings.Contains(strings.ToLower(l), strings.ToLower(req.Search)) {
			continue
		}
		links = append(links, map[string]any{"url": l})
	}
	writeJSON(w, map[string]any{"success": true, "links": links})
}

// v2Crawl queues a crawl job — {url, includePaths, excludePaths, maxDepth,
// limit, webhook, scrapeOptions} → {id, url}.
func (s *Server) v2Crawl(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no job store configured")
		return
	}
	var req struct {
		URL           string           `json:"url"`
		IncludePaths  []string         `json:"includePaths"`
		ExcludePaths  []string         `json:"excludePaths"`
		MaxDepth      int              `json:"maxDepth"`
		Limit         int              `json:"limit"`
		Webhook       any              `json:"webhook"` // string or {url}
		ScrapeOptions *v2ScrapeOptions `json:"scrapeOptions"`
		IgnoreQuery   bool             `json:"ignoreQueryParameters"`
		AllowExternal bool             `json:"allowExternalLinks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {url}")
		return
	}
	if !strings.Contains(req.URL, "://") {
		req.URL = "https://" + req.URL
	}
	if err := s.guard.CheckURL(req.URL); err != nil {
		writeErr(w, http.StatusForbidden, "target refused: "+err.Error())
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 30
	}
	hook, hookSecret := "", ""
	switch wh := req.Webhook.(type) {
	case string:
		hook = wh
	case map[string]any:
		hook, _ = wh["url"].(string)
		hookSecret, _ = wh["secret"].(string) // HMAC key — X-Webx-Signature
	}
	if prev := s.idempotentJob(r, "crawl"); prev != nil {
		writeJSON(w, map[string]any{
			"success": true, "id": prev.ID, "url": "/v2/crawl/" + prev.ID,
			"warning": "idempotent replay — returning existing job",
		})
		return
	}
	params, _ := json.Marshal(map[string]any{
		"url": req.URL, "limit": limit, "depth": req.MaxDepth,
		"include_paths": req.IncludePaths, "exclude_paths": req.ExcludePaths,
		"webhook_url": hook, "webhook_secret": hookSecret,
	})
	job := &store.Job{ID: store.NewID(), Kind: "crawl", Total: limit, Params: injectIdem(params, r)}
	if err := s.store.CreateJob(r.Context(), job); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "id": job.ID, "url": "/v2/crawl/" + job.ID})
}

// v2Batch queues a batch scrape — {urls, ...options, webhook} → {id, url}.
func (s *Server) v2Batch(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no job store configured")
		return
	}
	var req struct {
		URLs    []string         `json:"urls"`
		Webhook any              `json:"webhook"`
		Options *v2ScrapeOptions `json:"scrapeOptions"`
	}
	// Firecrawl v2 puts scrape fields at top level too — tolerate both.
	var raw map[string]any
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &raw)
	if err := json.Unmarshal(body, &req); err != nil || len(req.URLs) == 0 {
		writeErr(w, http.StatusBadRequest, "body must be JSON {urls:[...]}")
		return
	}
	if len(req.URLs) > 200 {
		writeErr(w, http.StatusBadRequest, "max 200 urls per batch")
		return
	}
	var kept []string
	for _, u := range req.URLs {
		if s.guard.CheckURL(u) == nil {
			kept = append(kept, u)
		}
	}
	if len(kept) == 0 {
		writeErr(w, http.StatusForbidden, "all urls refused by target policy")
		return
	}
	req.URLs = kept
	hook, hookSecret := "", ""
	switch wh := req.Webhook.(type) {
	case string:
		hook = wh
	case map[string]any:
		hook, _ = wh["url"].(string)
		hookSecret, _ = wh["secret"].(string)
	}
	if prev := s.idempotentJob(r, "batch"); prev != nil {
		writeJSON(w, map[string]any{"success": true, "id": prev.ID, "url": "/v2/batch/scrape/" + prev.ID})
		return
	}
	params, _ := json.Marshal(map[string]any{
		"urls": req.URLs, "webhook_url": hook, "webhook_secret": hookSecret,
		"options": map[string]any{
			"render": raw["render"], "formats": raw["formats"],
		},
	})
	job := &store.Job{ID: store.NewID(), Kind: "batch", Total: len(req.URLs), Params: injectIdem(params, r)}
	if err := s.store.CreateJob(r.Context(), job); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"success": true, "id": job.ID,
		"url": "/v2/batch/scrape/" + job.ID, "invalidURLs": []string{},
	})
}

// v2JobStatus reports crawl/batch progress in Firecrawl's shape:
// {status: scraping|completed|failed|cancelled, data:[{markdown,metadata}]}.
// Pages paginate via ?limit=&offset= like the native status route.
func (s *Server) v2JobStatus(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no job store configured")
		return
	}
	job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "job not found")
		return
	}
	limit, offset := pageParams(r, 500, 5000)
	pages, _ := s.store.Pages(r.Context(), job.ID, limit, offset)
	status := map[store.Status]string{
		store.Queued:    "scraping",
		store.Running:   "scraping",
		store.Completed: "completed",
		store.Failed:    "failed",
		store.Cancelled: "cancelled",
	}[job.Status]
	data := make([]map[string]any, 0, len(pages))
	for _, p := range pages {
		item := map[string]any{
			"markdown": p.Body,
			"metadata": map[string]any{
				"sourceURL": p.URL, "title": p.Title, "statusCode": 200,
			},
		}
		var meta map[string]any
		if json.Unmarshal(p.Meta, &meta) == nil {
			for k, v := range meta {
				item["metadata"].(map[string]any)[k] = v
			}
		}
		data = append(data, item)
	}
	out := map[string]any{
		"success":   true,
		"status":    status,
		"total":     job.Total,
		"completed": job.Done,
		"data":      data,
	}
	if job.Error != "" {
		out["error"] = job.Error
	}
	writeJSON(w, out)
}

// v2Extract runs extraction over one or more urls — {urls, prompt, schema}
// → {success, data:{...}}. CSS selector schemas run the free deterministic
// path; JSON schemas go through the configured LLM.
func (s *Server) v2Extract(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URLs          []string         `json:"urls"`
		Prompt        string           `json:"prompt"`
		Schema        map[string]any   `json:"schema"`
		CSS           map[string]any   `json:"css"`
		ScrapeOptions *v2ScrapeOptions `json:"scrapeOptions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.URLs) == 0 {
		writeErr(w, http.StatusBadRequest, "body must be JSON {urls:[...]}")
		return
	}
	for _, u := range req.URLs {
		if err := s.guard.CheckURL(u); err != nil {
			writeErr(w, http.StatusForbidden, "target refused: "+err.Error())
			return
		}
	}
	results := make([]map[string]any, 0, len(req.URLs))
	for _, u := range req.URLs {
		item := map[string]any{"url": u}
		if len(req.CSS) > 0 {
			freq := fetch.FetchRequest{URL: u, WantHTML: true}
			if req.ScrapeOptions != nil {
				freq = req.ScrapeOptions.toFetchRequest()
				freq.URL, freq.WantHTML = u, true
			}
			doc, err := fetchFn(r.Context(), freq)
			if err != nil {
				item["error"] = err.Error()
				if c := fetch.ErrorCode(err); c != "" {
					item["code"] = c
				}
			} else if data, err := fetch.ExtractCSS([]byte(doc.HTML), req.CSS); err != nil {
				item["error"] = err.Error()
			} else {
				item["data"] = data
			}
		} else {
			res, err := fetch.Extract(r.Context(), fetch.ExtractRequest{
				URL: u, Schema: req.Schema, Prompt: req.Prompt,
			})
			if err != nil {
				item["error"] = err.Error()
			} else {
				item["data"] = res.Data
			}
		}
		results = append(results, item)
	}
	data := map[string]any{"extractions": results}
	if len(results) == 1 {
		data = results[0] // single-url shape matches Firecrawl's flat data
	}
	writeJSON(w, map[string]any{"success": true, "data": data})
}

// v2Agent mirrors POST /v2/agent — {prompt, urls?, schema?, webhook?} → async
// job. Firecrawl's agent is always async: submit returns an id, clients poll
// GET /v2/agent/{id} (or get a webhook push).
func (s *Server) v2Agent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt     string         `json:"prompt"`
		URLs       []string       `json:"urls"`
		Schema     map[string]any `json:"schema"`
		Model      string         `json:"model"`      // accepted — we run the configured LLM
		MaxCredits int            `json:"maxCredits"` // accepted — bound maps to our page limit
		Webhook    string         `json:"webhook"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Prompt == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {prompt}")
		return
	}
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "agent needs a job store")
		return
	}
	areq := agentReq{
		Goal:    req.Prompt,
		Schema:  req.Schema,
		URLs:    req.URLs,
		Webhook: req.Webhook,
		Async:   true,
	}
	if len(req.URLs) == 1 {
		areq.URL = req.URLs[0] // single-site hint → discovery
	}
	if req.MaxCredits > 0 && req.MaxCredits < 10 {
		areq.Limit = req.MaxCredits // credits ≈ pages read
	}
	for _, u := range req.URLs {
		if err := s.guard.CheckURL(u); err != nil {
			writeErr(w, http.StatusForbidden, "target refused: "+err.Error())
			return
		}
	}
	params, _ := json.Marshal(areq)
	job := &store.Job{ID: store.NewID(), Kind: "agent", Total: 1, Params: injectIdem(params, r)}
	if err := s.store.CreateJob(r.Context(), job); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"success": true, "id": job.ID, "statusUrl": "/v2/agent/" + job.ID,
	})
}

// v2AgentStatus reports agent job progress in Firecrawl's shape:
// {success, status: processing|completed|failed, data, expiresAt}.
func (s *Server) v2AgentStatus(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no job store configured")
		return
	}
	job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "job not found")
		return
	}
	status := map[store.Status]string{
		store.Queued:    "processing",
		store.Running:   "processing",
		store.Completed: "completed",
		store.Failed:    "failed",
		store.Cancelled: "cancelled",
	}[job.Status]
	out := map[string]any{
		"success":   true,
		"status":    status,
		"expiresAt": job.UpdatedAt.Add(24 * time.Hour).UTC().Format("2006-01-02T15:04:05Z"),
	}
	if job.Result != "" {
		var data any
		if json.Unmarshal([]byte(job.Result), &data) == nil {
			out["data"] = data
		}
	}
	if job.Error != "" {
		out["error"] = job.Error
	}
	writeJSON(w, out)
}

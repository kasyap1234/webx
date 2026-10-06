package serve

// jobs.go — async job kinds beyond crawl/batch (research, wildcard
// extract, agent) plus the operational endpoints (ready, metrics,
// version). Job runners write their primary payload into job.Result and
// per-URL evidence into job_pages so the same status/pagination/SSE
// machinery serves every kind.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/kasyap1234/webx/internal/store"
	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/index"
	"github.com/kasyap1234/webx/web/render"
	"github.com/kasyap1234/webx/web/research"
	"github.com/kasyap1234/webx/web/search"
)

// Version is the build version reported by /version — cmd/webx and webxd
// stamp it at startup; "dev" means an unstamped build.
var Version = "dev"

// versionString resolves what /version reports: the stamped Version when
// goreleaser/ldflags set one, else the Go module's own build info — which
// is how `go install …@latest` binaries identify themselves honestly
// instead of answering "dev" forever.
func versionString() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return Version
}

// ── operational endpoints ────────────────────────────────────────────────

// ready is the readiness probe — distinct from /health's liveness: the
// process is up, but is the store reachable and a render backend around?
// Orchestrators should gate traffic on this, not on /health.
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	checks := map[string]any{"render": render.Available()}
	ready := true
	if s.store == nil {
		checks["store"] = "disabled"
	} else if _, err := s.store.ListJobs(r.Context(), 1); err != nil {
		checks["store"] = "error: " + err.Error()
		ready = false
	} else {
		checks["store"] = "ok"
	}
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ready": ready, "checks": checks})
}

// metricsHandler exposes Prometheus-format counters — process-local
// (per-replica), so aggregation happens in the scraper.
func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	serverMetrics.render(w)
	if s.store != nil {
		// queue depth gauges — appended after the counter section so a
		// missing store never breaks the exposition.
		var queued, running int64
		if jobs, err := s.store.ListJobs(r.Context(), 500); err == nil {
			for _, j := range jobs {
				switch j.Status {
				case store.Queued:
					queued++
				case store.Running:
					running++
				}
			}
		}
		fmt.Fprintf(w, "# HELP webx_jobs Job counts by status (recent 500)\n")
		fmt.Fprintf(w, "# TYPE webx_jobs gauge\n")
		fmt.Fprintf(w, "webx_jobs{status=\"queued\"} %d\n", queued)
		fmt.Fprintf(w, "webx_jobs{status=\"running\"} %d\n", running)
	}
}

func (s *Server) version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"version": versionString(), "name": "webx"})
}

// ── async research ───────────────────────────────────────────────────────

// runResearchJob executes a queued research job — same pipeline as the
// synchronous /research route, result stored on the job record.
func (s *Server) runResearchJob(ctx context.Context, job *store.Job) {
	var p struct {
		Query        string         `json:"query"`
		MaxSources   int            `json:"max_sources"`
		SubQueries   int            `json:"sub_queries"`
		OutputSchema map[string]any `json:"output_schema"`
		Webhook      string         `json:"webhook_url"`
		Secret       string         `json:"webhook_secret"`
	}
	if err := json.Unmarshal(job.Params, &p); err != nil || p.Query == "" {
		s.failJob(job.ID, "bad params: need query")
		return
	}
	rep, err := research.Run(ctx, p.Query, research.Options{
		MaxSources: p.MaxSources, SubQueries: p.SubQueries, Schema: p.OutputSchema,
	}, nil)
	if err != nil {
		s.failJob(job.ID, err.Error())
		return
	}
	raw, _ := json.Marshal(rep)
	_ = s.store.UpdateJob(ctx, job.ID, func(j *store.Job) { j.Result = string(raw) })
	// Sources land as pages too — the pagination/SSE/progress views stay
	// meaningful for long reports.
	for _, src := range rep.Sources {
		meta, _ := json.Marshal(map[string]any{"kind": "source"})
		_ = s.store.PutPage(ctx, job.ID, store.JobPage{
			URL: src.URL, Title: src.Title, Body: src.Excerpt, Meta: meta,
		})
	}
	s.finishJob(ctx, job, false, p.Webhook, p.Secret)
}

// ── async extract (wildcard + multi-url) ─────────────────────────────────

// runExtractJob handles "extract" jobs — wildcard domains
// (example.com/* → map → extract each discovered page) or an explicit
// URL list. Extraction honors css-selector schemas (free) and LLM schemas.
func (s *Server) runExtractJob(ctx context.Context, job *store.Job, cancelled func() bool) {
	var p struct {
		URL     string         `json:"url"`
		URLs    []string       `json:"urls"`
		Schema  map[string]any `json:"schema"`
		CSS     map[string]any `json:"css"`
		Prompt  string         `json:"prompt"`
		Browser bool           `json:"browser"`
		Render  bool           `json:"render"`
		Session string         `json:"session"`
		Limit   int            `json:"limit"`
		Webhook string         `json:"webhook_url"`
		Secret  string         `json:"webhook_secret"`
	}
	if err := json.Unmarshal(job.Params, &p); err != nil {
		s.failJob(job.ID, "bad params: "+err.Error())
		return
	}

	// Expand wildcards — "example.com/*" maps the site first, like
	// Firecrawl's extract job does.
	var targets []string
	for _, u := range append([]string{p.URL}, p.URLs...) {
		if u == "" {
			continue
		}
		if strings.HasSuffix(u, "/*") || strings.HasSuffix(u, "*") {
			host := strings.TrimSuffix(strings.TrimSuffix(u, "/*"), "*")
			if !strings.Contains(host, "://") {
				host = "https://" + host
			}
			base, err := url.Parse(host)
			if err != nil {
				continue
			}
			limit := p.Limit
			if limit <= 0 {
				limit = 50
			}
			if urls, err := index.DiscoverURLs(ctx, base, limit); err == nil {
				targets = append(targets, urls...)
			} else {
				s.failJob(job.ID, "map "+host+": "+err.Error())
				return
			}
		} else {
			targets = append(targets, u)
		}
	}
	if len(targets) == 0 {
		s.failJob(job.ID, "no urls resolved")
		return
	}
	_ = s.store.UpdateJob(ctx, job.ID, func(j *store.Job) { j.Total = len(targets) })

	// Fan out with a small worker pool — extraction is fetch-bound, not CPU.
	const workers = 4
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, u := range targets {
		if cancelled() {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			defer func() { <-sem }()
			s.extractOne(ctx, job.ID, u, p.Schema, p.CSS, p.Prompt, p.Browser, p.Render, p.Session)
		}(u)
	}
	wg.Wait()
	s.finishJob(ctx, job, cancelled(), p.Webhook, p.Secret)
}

// extractOne runs the css-or-llm extraction for one URL into the job's
// page list — failures become error-meta pages, not job failures.
func (s *Server) extractOne(ctx context.Context, jobID, u string, schema, css map[string]any,
	prompt string, browser, render bool, session string) {
	if len(css) > 0 {
		doc, err := fetchFn(ctx, fetch.FetchRequest{
			URL: u, Browser: browser, Session: session, Render: render, WantHTML: true,
		})
		if err != nil {
			meta, _ := json.Marshal(map[string]any{"error": err.Error(), "code": fetch.ErrorCode(err)})
			_ = s.store.PutPage(ctx, jobID, store.JobPage{URL: u, Meta: meta})
			return
		}
		data, err := fetch.ExtractCSS([]byte(doc.HTML), css)
		if err != nil {
			meta, _ := json.Marshal(map[string]any{"error": err.Error()})
			_ = s.store.PutPage(ctx, jobID, store.JobPage{URL: u, Meta: meta})
			return
		}
		body, _ := json.Marshal(data)
		_ = s.store.PutPage(ctx, jobID, store.JobPage{
			URL: u, Title: doc.Title, Body: string(body),
			Meta: json.RawMessage(`{"extractor":"css"}`),
		})
		return
	}
	res, err := fetch.Extract(ctx, fetch.ExtractRequest{
		URL: u, Schema: schema, Prompt: prompt, Browser: browser, Session: session,
	})
	if err != nil {
		meta, _ := json.Marshal(map[string]any{"error": err.Error(), "code": fetch.ErrorCode(err)})
		_ = s.store.PutPage(ctx, jobID, store.JobPage{URL: u, Meta: meta})
		return
	}
	body, _ := json.Marshal(res.Data)
	meta, _ := json.Marshal(map[string]any{"extractor": "llm", "model": res.Model, "ok": res.OK, "err": res.Err})
	_ = s.store.PutPage(ctx, jobID, store.JobPage{
		URL: u, Title: res.Title, Body: string(body), Meta: meta,
	})
}

// ── /agent — goal-driven extraction ──────────────────────────────────────

// agentReq drives the goal-only endpoint — Firecrawl /agent parity:
// hand us a goal, we find the pages and extract what's asked.
type agentReq struct {
	Goal    string         `json:"goal"`
	URL     string         `json:"url"`    // optional site constraint
	URLs    []string       `json:"urls"`   // explicit candidate pages (skips discovery)
	Schema  map[string]any `json:"schema"` // optional structured output
	Limit   int            `json:"limit"`  // candidate pages to read, default 5
	Async   bool           `json:"async"`  // queue as a job when a store is attached
	Render  bool           `json:"render"` // render candidate pages
	Webhook string         `json:"webhook_url"`
	Secret  string         `json:"webhook_secret"`
}

// agent composes primitives: site given → map + pick candidates; no site
// → fused search for candidates. Each candidate is fit-scraped then
// LLM-extracted toward the goal. Provenance always comes back.
func (s *Server) agent(w http.ResponseWriter, r *http.Request) {
	var req agentReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Goal == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {goal}")
		return
	}
	if req.URL != "" {
		if !strings.Contains(req.URL, "://") {
			req.URL = "https://" + req.URL
		}
		if err := s.guard.CheckURL(req.URL); err != nil {
			writeErr(w, http.StatusForbidden, "target refused: "+err.Error())
			return
		}
	}
	if req.Async {
		if s.store == nil {
			writeErr(w, http.StatusServiceUnavailable, "async needs a job store")
			return
		}
		params, _ := json.Marshal(req)
		job := &store.Job{ID: store.NewID(), Kind: "agent", Total: 1, Params: injectIdem(params, r)}
		if err := s.store.CreateJob(r.Context(), job); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"success": true, "id": job.ID, "url": "/crawl/" + job.ID})
		return
	}
	out, err := s.runAgent(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, out)
}

// runAgentJob is the queued form of /agent.
func (s *Server) runAgentJob(ctx context.Context, job *store.Job) {
	var req agentReq
	if err := json.Unmarshal(job.Params, &req); err != nil {
		s.failJob(job.ID, "bad params: "+err.Error())
		return
	}
	out, err := s.runAgent(ctx, req)
	if err != nil {
		s.failJob(job.ID, err.Error())
		return
	}
	raw, _ := json.Marshal(out["data"])
	_ = s.store.UpdateJob(ctx, job.ID, func(j *store.Job) { j.Result = string(raw) })
	if sources, ok := out["sources"].([]map[string]any); ok {
		for _, src := range sources {
			u, _ := src["url"].(string)
			t, _ := src["title"].(string)
			_ = s.store.PutPage(ctx, job.ID, store.JobPage{URL: u, Title: t})
		}
	}
	s.finishJob(ctx, job, false, req.Webhook, req.Secret)
}

// runAgent does the actual goal → answer composition, bounded by Limit.
// One retry round when every extraction misses — widen the candidate set
// and try again. Literature on agent loops agrees on bounded iterations
// with a verifiable stop: two rounds max, success = a non-empty answer.
func (s *Server) runAgent(ctx context.Context, req agentReq) (map[string]any, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}

	var candidates []string
	expand := false // second round pulls the deeper candidate slice
	for round := 0; round < 2; round++ {
		var err error
		candidates, err = s.agentCandidates(ctx, req, limit, expand)
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			return nil, fmt.Errorf("no candidate pages found for goal")
		}
		out := s.agentExtract(ctx, req, candidates)
		if agentSucceeded(out) || round == 1 {
			return out, nil
		}
		expand = true
	}
	return nil, fmt.Errorf("unreachable")
}

// agentCandidates picks the pages to read — explicit URLs, site discovery,
// or fused search. expand pulls the deeper slice for the retry round.
func (s *Server) agentCandidates(ctx context.Context, req agentReq, limit int, expand bool) ([]string, error) {
	if len(req.URLs) > 1 {
		c := req.URLs
		if len(c) > limit {
			c = c[:limit]
		}
		return c, nil
	}
	if req.URL != "" {
		base, err := url.Parse(req.URL)
		if err != nil {
			return nil, err
		}
		urls, err := index.DiscoverURLs(ctx, base, 200)
		if err != nil {
			return nil, fmt.Errorf("map %s: %w", req.URL, err)
		}
		n := limit
		if expand {
			n = limit * 2 // retry reaches past the obvious pages
		}
		c := pickByGoal(urls, req.Goal, n)
		if expand && len(c) > limit {
			c = c[limit:] // skip what round 1 already read
		}
		if len(c) == 0 && !expand {
			c = []string{req.URL}
		}
		return c, nil
	}
	resp := searchFn(ctx, search.Request{Query: req.Goal, Num: limit * 2})
	c := make([]string, 0, limit*2)
	for _, r := range resp.Results {
		c = append(c, r.URL)
	}
	if expand && len(c) > limit {
		c = c[limit:]
	} else if len(c) > limit {
		c = c[:limit]
	}
	return c, nil
}

// agentAnswer is one candidate's extraction outcome.
type agentAnswer struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// agentExtract fit-scrapes each candidate then LLM-extracts toward the goal.
func (s *Server) agentExtract(ctx context.Context, req agentReq, candidates []string) map[string]any {
	answers := make([]agentAnswer, 0, len(candidates))
	sources := make([]map[string]any, 0, len(candidates))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	for _, u := range candidates {
		wg.Add(1)
		sem <- struct{}{}
		go func(u string) {
			defer wg.Done()
			defer func() { <-sem }()
			doc, err := fetchFn(ctx, fetch.FetchRequest{
				URL: u, Fit: req.Goal, Render: req.Render,
			})
			if err != nil {
				mu.Lock()
				answers = append(answers, agentAnswer{URL: u, Error: err.Error()})
				mu.Unlock()
				return
			}
			mu.Lock()
			sources = append(sources, map[string]any{"url": doc.FinalURL, "title": doc.Title})
			mu.Unlock()
			// Generic schema when the caller didn't pin one — the model
			// shapes the JSON; we only insist it answers the goal.
			schema := req.Schema
			if schema == nil {
				schema = map[string]any{
					"type": "object",
					"properties": map[string]any{
						"answer":   map[string]any{"type": "string", "description": "direct answer to the goal"},
						"evidence": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"found":    map[string]any{"type": "boolean"},
					},
				}
			}
			// Feed the already-scraped page back — no second fetch.
			md := doc.FitMarkdown
			if md == "" {
				md = doc.Markdown
			}
			res, err := fetch.Extract(ctx, fetch.ExtractRequest{
				URL: doc.FinalURL, Title: doc.Title, Markdown: md,
				Schema: schema, Prompt: req.Goal,
			})
			mu.Lock()
			if err != nil {
				answers = append(answers, agentAnswer{URL: doc.FinalURL, Title: doc.Title, Error: err.Error()})
			} else {
				answers = append(answers, agentAnswer{URL: doc.FinalURL, Title: doc.Title, Data: res.Data})
			}
			mu.Unlock()
		}(u)
	}
	wg.Wait()
	return map[string]any{
		"success": true,
		"goal":    req.Goal,
		"data":    answers,
		"sources": sources,
	}
}

// agentSucceeded reports whether any extraction produced real data —
// the verifiable stop for the retry loop.
func agentSucceeded(out map[string]any) bool {
	answers, _ := out["data"].([]agentAnswer)
	for _, a := range answers {
		if a.Data == nil {
			continue
		}
		// The default schema's found:false is an honest miss, not success.
		if m, ok := a.Data.(map[string]any); ok {
			if f, ok := m["found"].(bool); ok && !f {
				continue
			}
		}
		return true
	}
	return false
}

// pickByGoal scores discovered URLs by goal-term hits in the path —
// "pricing" goals land on /pricing pages without an LLM routing call.
func pickByGoal(urls []string, goal string, limit int) []string {
	terms := strings.Fields(strings.ToLower(goal))
	type scored struct {
		u string
		s int
	}
	list := make([]scored, 0, len(urls))
	for _, u := range urls {
		pu, err := url.Parse(u)
		if err != nil {
			continue
		}
		path := strings.ToLower(pu.Path + " " + pu.RawQuery)
		sc := 0
		for _, t := range terms {
			if len(t) > 2 && strings.Contains(path, t) {
				sc++
			}
		}
		list = append(list, scored{u, sc})
	}
	var out []string
	// two passes: scored hits first, then fill with discovery order.
	for want := 0; want < 2 && len(out) < limit; want++ {
		for _, c := range list {
			if len(out) >= limit {
				break
			}
			if want == 0 && c.s == 0 {
				continue
			}
			if want == 1 && c.s > 0 {
				continue
			}
			out = append(out, c.u)
		}
	}
	return out
}

// ── helpers ──────────────────────────────────────────────────────────────

// batchWorkers bounds the batch job's page fetch fan-out.
const batchWorkers = 4

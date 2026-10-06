// Package serve runs webx's HTTP API — shared by the lite `webx serve`
// (SQLite store, in-process workers) and the durable `webxd` (Postgres
// store, multi-worker). Routes mirror Firecrawl's shape so existing
// integrations can point at a local webx instead: POST /scrape,
// POST /search, POST /map, POST /crawl (async) → GET /crawl/{id}.
package serve

import (
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kasyap1234/webx/internal/license"
	"github.com/kasyap1234/webx/internal/mcp"
	"github.com/kasyap1234/webx/internal/store"
	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/index"
	"github.com/kasyap1234/webx/web/render"
	"github.com/kasyap1234/webx/web/research"
	"github.com/kasyap1234/webx/web/search"
)

// searchFn and fetchFn are the outbound web entries — vars so tests can
// stub providers/origins without network.
var (
	searchFn = search.Search
	fetchFn  = fetch.Fetch
)

// Server exposes the toolkit over HTTP.
type Server struct {
	Mux     *http.ServeMux
	store   store.Store
	guard   *TargetGuard
	idxOnce sync.Once
	idx     *index.Index
	idxErr  error
	// billing substrate
	license *license.License
	envKey  string
	meter   *meterQueue
	conc    *concLimits
}

// index lazily opens the local FTS5 corpus — powers max_age caching,
// change_tracking, and /similar. Nil-safe: callers degrade when absent.
func (s *Server) index() *index.Index {
	s.idxOnce.Do(func() {
		s.idx, s.idxErr = index.Open(index.DefaultPathEnv())
	})
	if s.idxErr != nil {
		return nil
	}
	return s.idx
}

// New builds the route table. st may be nil — async job endpoints then
// return 503; the stateless routes (scrape/search/map/extract) still work.
func New(st store.Store) *Server {
	lic, err := license.LoadEnv()
	if err != nil {
		slog.Warn("webx: license rejected (running free tier)", "err", err)
	}
	s := &Server{
		Mux: http.NewServeMux(), store: st, guard: NewTargetGuard(),
		license: lic, envKey: os.Getenv("WEBX_API_KEY"),
		conc: newConcLimits(),
	}
	if st != nil {
		s.meter = newMeterQueue(st)
	}
	s.Mux.HandleFunc("GET /health", s.health)
	s.Mux.HandleFunc("GET /ready", s.ready)
	s.Mux.HandleFunc("GET /metrics", s.metricsHandler)
	s.Mux.HandleFunc("GET /version", s.version)
	s.Mux.HandleFunc("GET /license", s.licenseInfo)
	s.Mux.HandleFunc("POST /scrape", s.scrape)
	s.Mux.HandleFunc("POST /search", s.search)
	s.Mux.HandleFunc("POST /answer", s.answer)
	s.Mux.HandleFunc("POST /map", s.mapURLs)
	s.Mux.HandleFunc("POST /extract", s.extract)
	s.Mux.HandleFunc("POST /research", s.research)
	s.Mux.HandleFunc("POST /verify", s.verify)
	s.Mux.HandleFunc("POST /similar", s.similar)
	s.Mux.HandleFunc("POST /wayback", s.wayback)
	s.Mux.HandleFunc("POST /v2/scrape", s.v2Scrape)
	s.Mux.HandleFunc("POST /v2/search", s.v2Search)
	s.Mux.HandleFunc("POST /v2/map", s.v2Map)
	s.Mux.HandleFunc("POST /v2/crawl", s.v2Crawl)
	s.Mux.HandleFunc("GET /v2/crawl/{id}", s.v2JobStatus)
	s.Mux.HandleFunc("POST /v2/batch/scrape", s.v2Batch)
	s.Mux.HandleFunc("GET /v2/batch/scrape/{id}", s.v2JobStatus)
	s.Mux.HandleFunc("POST /v2/extract", s.v2Extract)
	s.Mux.HandleFunc("POST /v2/agent", s.v2Agent)
	s.Mux.HandleFunc("GET /v2/agent/{id}", s.v2AgentStatus)
	// Firecrawl v1 SDKs hit /v1 paths — alias the same handlers so both
	// generations of SDKs work against a self-hosted webx.
	s.Mux.HandleFunc("POST /v1/scrape", s.v2Scrape)
	s.Mux.HandleFunc("POST /v1/search", s.v2Search)
	s.Mux.HandleFunc("POST /v1/map", s.v2Map)
	s.Mux.HandleFunc("POST /v1/crawl", s.v2Crawl)
	s.Mux.HandleFunc("GET /v1/crawl/{id}", s.v2JobStatus)
	s.Mux.HandleFunc("POST /v1/batch/scrape", s.v2Batch)
	s.Mux.HandleFunc("GET /v1/batch/scrape/{id}", s.v2JobStatus)
	s.Mux.HandleFunc("POST /v1/extract", s.v2Extract)
	s.Mux.HandleFunc("POST /v1/agent", s.v2Agent)
	s.Mux.HandleFunc("GET /v1/agent/{id}", s.v2AgentStatus)
	s.Mux.HandleFunc("GET /openapi.json", s.openapi)
	s.Mux.HandleFunc("GET /doctor", s.doctor)
	// Jina-style reader — the zero-setup front door: curl /r/<url> → markdown.
	// {url...} matches /r/ too (empty remainder prints usage).
	s.Mux.HandleFunc("GET /r/{url...}", s.reader)
	s.Mux.HandleFunc("GET /pages/md", s.pageMD)
	s.Mux.HandleFunc("GET /crawl/{id}/events", s.jobEvents)
	s.Mux.HandleFunc("POST /crawl", s.crawlStart)
	s.Mux.HandleFunc("GET /crawl/{id}", s.crawlStatus)
	s.Mux.HandleFunc("GET /crawl/{id}/errors", s.jobErrors)
	s.Mux.HandleFunc("POST /crawl/{id}/retry", s.jobRetry)
	s.Mux.HandleFunc("POST /crawl/{id}/cancel", s.jobCancel)
	s.Mux.HandleFunc("POST /agent", s.agent)
	s.Mux.HandleFunc("POST /batch/scrape", s.batchStart)
	s.Mux.HandleFunc("GET /jobs", s.jobs)
	// tenancy + billing substrate
	s.Mux.HandleFunc("POST /keys", s.keysCreate)
	s.Mux.HandleFunc("GET /keys", s.keysList)
	s.Mux.HandleFunc("PATCH /keys/{id}", s.keysPatch)
	s.Mux.HandleFunc("DELETE /keys/{id}", s.keysDelete)
	s.Mux.HandleFunc("GET /usage", s.usage)
	s.Mux.HandleFunc("GET /audit", s.audit)
	// MCP streamable HTTP transport — same tools as `webx mcp` over stdio,
	// exposed for remote agents (claude mcp add --transport http).
	s.Mux.Handle("/mcp", mcp.StreamableHandler())
	s.Mux.HandleFunc("POST /schedules", s.schedulesCreate)
	s.Mux.HandleFunc("GET /schedules", s.schedulesList)
	s.Mux.HandleFunc("PATCH /schedules/{id}", s.schedulesPatch)
	s.Mux.HandleFunc("DELETE /schedules/{id}", s.schedulesDelete)
	return s
}

// Handler wraps the mux in the tenancy chain: request-id → panic guard →
// CORS → body cap → gzip → auth (env key or api_keys table) → monthly
// quota → rate limit (per-key RPM, store-shared when available) →
// concurrency cap → mux → meter+audit+metrics. /health and /ready stay
// open for probes.
func (s *Server) Handler() http.Handler {
	rl := newRateLimiter(rateRPM())
	cors := corsFromEnv()
	open := map[string]bool{"/health": true, "/ready": true, "/metrics": true, "/version": true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := newRequestID(r)
		w.Header().Set("X-Request-Id", reqID)
		r = r.WithContext(context.WithValue(r.Context(), reqIDKey, reqID))

		s.recoverGuard(w, r, func() {
			if cors != nil && !cors.handle(w, r) {
				return // preflight answered
			}
			if open[r.URL.Path] {
				s.Mux.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodPost || r.Method == http.MethodPut ||
				r.Method == http.MethodPatch {
				r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
			}
			if wantGzip(r) {
				gzw, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
				defer gzw.Close()
				w = &gzipResponseWriter{ResponseWriter: w, gz: gzw}
			}

			// auth — open when nothing is configured, else env-key or table key
			key, ok := s.resolveKey(r)
			if s.authRequired(r.Context()) && !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]any{
					"success": false, "error": "missing/invalid Authorization: Bearer key"})
				return
			}
			if key != nil {
				r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, key))
			}
			// monthly quota — the paid-tier cap; over-limit → 402 like the
			// payment gates we detect on other hosts. Env key is exempt.
			if key != nil && key.ID != envKeyID && key.MonthlyUnits > 0 && s.store != nil {
				if used, err := s.store.MonthUnits(r.Context(), key.ID); err == nil && used >= key.MonthlyUnits {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusPaymentRequired)
					json.NewEncoder(w).Encode(map[string]any{
						"success": false, "code": "quota_exceeded",
						"error": fmt.Sprintf("monthly quota %d units used", key.MonthlyUnits)})
					return
				}
			}
			// rate limit — key RPM overrides the global bucket, keyed by
			// key-id so per-IP sharing one key doesn't dodge the cap.
			// When the store supports it, keyed limits are shared across
			// replicas (approximate: one minute window, see RateCheck).
			rpm := 0
			limitKey := clientIP(r)
			allowed := true
			if key != nil {
				limitKey = "key:" + key.ID
				rpm = key.RPM
				if rpm > 0 && s.store != nil {
					if ok, err := s.store.RateCheck(r.Context(), limitKey, rpm, 60); err == nil {
						allowed = ok // shared check wins; local bucket skipped
					} else {
						allowed = rl.allow(limitKey, rpm) // store down → local fallback
					}
				} else {
					allowed = rl.allow(limitKey, rpm)
				}
			} else {
				allowed = rl.allow(limitKey, rpm)
			}
			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "5")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]any{
					"success": false, "error": "rate limit exceeded"})
				return
			}
			// concurrency — global cap (WEBX_MAX_CONCURRENCY) + per-key.
			release, ok := s.conc.acquire(key)
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]any{
					"success": false, "code": "concurrency_limit",
					"error": "concurrency limit reached — retry shortly"})
				return
			}
			defer release()

			cw := &statusWriter{ResponseWriter: w, status: 200}
			s.Mux.ServeHTTP(cw, r)
			ms := time.Since(start).Milliseconds()
			serverMetrics.observe(r.URL.Path, ms, cw.status)

			keyID := "anon"
			if key != nil {
				keyID = key.ID
			}
			if s.meter != nil {
				s.meter.record(keyID, r.URL.Path, meterUnits(r.URL.Path, cw.status), ms)
			}
			if s.license.Allows(license.FeatAudit) && s.store != nil {
				_ = s.store.PutAudit(context.Background(), store.AuditEvent{
					KeyID: keyID, Method: r.Method, Path: r.URL.Path,
					Status: cw.status, Ms: ms,
				})
			}
		})
	})
}

// statusWriter captures the response code for metering/audit.
// Flush/Unwrap forward to the underlying writer so SSE (and anything else
// streaming through ResponseController) keeps working through the wrap.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// concLimits bounds in-flight requests — globally via WEBX_MAX_CONCURRENCY
// and per-key via key.Concurrency. The Firecrawl/Steel tier lever.
type concLimits struct {
	global chan struct{}
	mu     sync.Mutex
	perKey map[string]chan struct{}
}

func newConcLimits() *concLimits {
	var g chan struct{}
	if n := envInt("WEBX_MAX_CONCURRENCY", 0); n > 0 {
		g = make(chan struct{}, n)
	}
	return &concLimits{global: g, perKey: map[string]chan struct{}{}}
}

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

// acquire takes a global + per-key slot; returns a release func.
func (c *concLimits) acquire(k *store.APIKey) (func(), bool) {
	var held []chan struct{}
	try := func(ch chan struct{}) bool {
		if ch == nil {
			return true
		}
		select {
		case ch <- struct{}{}:
			held = append(held, ch)
			return true
		default:
			return false
		}
	}
	if !try(c.global) {
		return func() {}, false
	}
	if k != nil && k.Concurrency > 0 {
		c.mu.Lock()
		ch := c.perKey[k.ID]
		if ch == nil {
			ch = make(chan struct{}, k.Concurrency)
			c.perKey[k.ID] = ch
		}
		c.mu.Unlock()
		if !try(ch) {
			for _, h := range held {
				<-h
			}
			return func() {}, false
		}
	}
	return func() {
		for _, h := range held {
			<-h
		}
	}, true
}

// rateRPM reads WEBX_RATE_RPM; 0 → 240 (a generous single-tenant default —
// the knob exists so deploys can tighten it, not to throttle dev work).
func rateRPM() int {
	if v := os.Getenv("WEBX_RATE_RPM"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return 240
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// rateLimiter is a per-key token bucket — one refill of `perMin` tokens per
// minute. Buckets for quiet IPs are reaped lazily on each request sweep.
type rateLimiter struct {
	mu      sync.Mutex
	perMin  float64
	buckets map[string]*bucket
	lastGC  time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(perMin int) *rateLimiter {
	return &rateLimiter{perMin: float64(perMin), buckets: map[string]*bucket{}}
}

// allow takes one token for key at rate rpm (0 → the limiter's default).
// Per-key RPM overrides live on the same bucket map, keyed "key:<id>".
func (rl *rateLimiter) allow(key string, rpm int) bool {
	perMin := rl.perMin
	if rpm > 0 {
		perMin = float64(rpm)
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b := rl.buckets[key]
	if b == nil {
		b = &bucket{tokens: perMin, last: now}
		rl.buckets[key] = b
	}
	b.tokens += float64(now.Sub(b.last)) / float64(time.Minute) * perMin
	if b.tokens > perMin {
		b.tokens = perMin
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	// sweep idle buckets hourly so a long-lived server doesn't grow the map
	if now.Sub(rl.lastGC) > time.Hour {
		rl.lastGC = now
		for k, v := range rl.buckets {
			if now.Sub(v.last) > time.Hour {
				delete(rl.buckets, k)
			}
		}
	}
	return true
}

// RunWorkers starts n claim-loop workers consuming queued jobs from the
// store until ctx cancels. Callers pick n by edition: lite gets a couple,
// webxd gets a pool sized to CPU/render budget.
func (s *Server) RunWorkers(ctx context.Context, n int) {
	if s.store == nil || n <= 0 {
		return
	}
	for i := 0; i < n; i++ {
		go s.worker(ctx)
	}
}

func (s *Server) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		job, err := s.store.ClaimJob(ctx)
		if errors.Is(err, store.ErrNoJob) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}
		if err != nil {
			slog.Error("webx worker: claim failed", "err", err)
			time.Sleep(5 * time.Second)
			continue
		}
		s.runJob(ctx, job)
	}
}

// runJob executes one claimed job; failures land on the job record.
// Cancelled jobs stop cooperatively: each persisted page re-checks the job
// status and cancels the job context, so cancel takes effect at page
// granularity — durable results up to the cancel point are kept.
func (s *Server) runJob(ctx context.Context, job *store.Job) {
	jctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cancelled := func() bool {
		j, err := s.store.GetJob(ctx, job.ID)
		return err == nil && j.Status == store.Cancelled
	}
	var webhook, hookSecret string
	var generic struct {
		Webhook       string `json:"webhook_url"`
		WebhookSecret string `json:"webhook_secret"`
	}
	_ = json.Unmarshal(job.Params, &generic)
	webhook, hookSecret = generic.Webhook, generic.WebhookSecret

	switch job.Kind {
	case "crawl":
		var p struct {
			URL          string   `json:"url"`
			Goal         string   `json:"goal"`
			Limit        int      `json:"limit"`
			Depth        int      `json:"depth"`
			Semantic     bool     `json:"semantic"`
			Concurrency  int      `json:"concurrency"`
			IncludePaths []string `json:"include_paths"`
			ExcludePaths []string `json:"exclude_paths"`
		}
		if err := json.Unmarshal(job.Params, &p); err != nil {
			s.failJob(job.ID, "bad params: "+err.Error())
			return
		}
		var embedder func(context.Context, string) ([]float32, error)
		if p.Semantic {
			embedder = index.EmbedderFromEnv()
			if embedder == nil {
				s.failJob(job.ID, "semantic:true needs WEBX_EMBED_MODEL (+optional WEBX_EMBED_BASE/KEY)")
				return
			}
		}
		_, err := fetch.Crawl(jctx, fetch.CrawlRequest{
			Start: p.URL, Query: p.Goal, Limit: p.Limit, Depth: p.Depth,
			Concurrency:  p.Concurrency,
			SameHost:     true,
			IncludePaths: p.IncludePaths, ExcludePaths: p.ExcludePaths,
			Embedder: embedder,
			Progress: func(pg fetch.CrawlPage) {
				meta, _ := json.Marshal(map[string]any{
					"final_url": pg.FinalURL, "depth": pg.Depth,
					"relevance": pg.Relevance, "relevant": pg.Relevant,
				})
				_ = s.store.PutPage(jctx, job.ID, store.JobPage{
					URL: pg.URL, Title: pg.Title, Body: pg.Markdown, Meta: meta,
				})
				if cancelled() {
					cancel()
				}
			},
		}, nil)
		if err != nil && !cancelled() {
			s.failJob(job.ID, err.Error())
			return
		}
		s.finishJob(ctx, job, cancelled(), webhook, hookSecret)

	case "batch":
		var p struct {
			URLs    []string  `json:"urls"`
			Options scrapeReq `json:"options"`
		}
		if err := json.Unmarshal(job.Params, &p); err != nil || len(p.URLs) == 0 {
			s.failJob(job.ID, "bad params: need urls[]")
			return
		}
		// Worker pool over the URL list — PutPage is atomic in the store
		// (done = done+1), so fan-out is safe; sqlite WAL serializes writes.
		// Workers keep draining on cancel so the feeder never wedges.
		var wg sync.WaitGroup
		urlCh := make(chan string)
		for w := 0; w < batchWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for u := range urlCh {
					if cancelled() {
						continue
					}
					freq := fetch.FetchRequest{
						URL: u, Browser: p.Options.Browser, Session: p.Options.Session,
						Render:     p.Options.Render || p.Options.Actions != "" || p.Options.Screenshot,
						AutoRender: p.Options.AutoRender,
						WaitFor:    p.Options.WaitFor, Scrolls: p.Options.Scrolls,
						Actions: p.Options.Actions, Screenshot: p.Options.Screenshot,
						Proxy: p.Options.Proxy, Fit: p.Options.Fit,
						IncludeSelectors: p.Options.Include, ExcludeSelectors: p.Options.Exclude,
					}
					p.Options.ShotOpts.shotFields(&freq)
					doc, ferr := fetchFn(jctx, freq)
					if ferr != nil {
						meta, _ := json.Marshal(map[string]any{"error": ferr.Error()})
						_ = s.store.PutPage(jctx, job.ID, store.JobPage{URL: u, Meta: meta})
						continue
					}
					meta, _ := json.Marshal(map[string]any{
						"final_url": doc.FinalURL, "tier_used": doc.TierUsed,
						"status_code": doc.StatusCode, "extractor": doc.Extractor,
					})
					body := doc.Markdown
					if doc.ScreenshotB64 != "" {
						body = body + "\n\nscreenshot_b64:" + doc.ScreenshotB64
					}
					_ = s.store.PutPage(jctx, job.ID, store.JobPage{
						URL: doc.URL, Title: doc.Title, Body: body, Meta: meta,
					})
				}
			}()
		}
		for _, u := range p.URLs {
			if cancelled() {
				break
			}
			urlCh <- u
		}
		close(urlCh)
		wg.Wait()
		s.finishJob(ctx, job, cancelled(), webhook, hookSecret)

	case "research":
		s.runResearchJob(jctx, job)

	case "extract":
		s.runExtractJob(jctx, job, cancelled)

	case "agent":
		s.runAgentJob(jctx, job)

	default:
		s.failJob(job.ID, "unknown job kind: "+job.Kind)
	}
}

// finishJob records the terminal state + fires the optional webhook.
func (s *Server) finishJob(ctx context.Context, job *store.Job, wasCancelled bool, webhook, secret string) {
	if wasCancelled {
		// already marked cancelled by the cancel endpoint — just fire webhook
	} else {
		_ = s.store.UpdateJob(ctx, job.ID, func(j *store.Job) { j.Status = store.Completed })
	}
	if webhook != "" {
		go s.fireWebhook(webhook, job.ID, secret)
	}
}

func (s *Server) failJob(id, msg string) {
	_ = s.store.UpdateJob(context.Background(), id, func(j *store.Job) {
		j.Status, j.Error = store.Failed, msg
	})
}

// fireWebhook POSTs the final job record to the caller's endpoint with
// retries (3 attempts, 2s/8s backoff) and an HMAC-SHA256 signature when a
// secret is configured — the job's webhook_secret param, else
// WEBX_WEBHOOK_SECRET. Recipients verify X-Webx-Signature over the raw
// body so completion notices can't be spoofed by anyone who knows the URL.
func (s *Server) fireWebhook(hook, jobID, secret string) {
	job, err := s.store.GetJob(context.Background(), jobID)
	if err != nil {
		return
	}
	pages, _ := s.store.Pages(context.Background(), jobID, 500, 0)
	payload, _ := json.Marshal(map[string]any{
		"id": job.ID, "kind": job.Kind, "status": job.Status,
		"total": job.Total, "completed": job.Done, "error": job.Error,
		"result": job.Result,
		"data":   pages,
	})
	if secret == "" {
		secret = os.Getenv("WEBX_WEBHOOK_SECRET")
	}
	var sig string
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(payload)
		sig = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * 2 * time.Second) // 2s, 8s
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook,
			strings.NewReader(string(payload)))
		if err != nil {
			cancel()
			return // malformed target URL — retrying can't help
		}
		req.Header.Set("Content-Type", "application/json")
		if sig != "" {
			req.Header.Set("X-Webx-Signature", sig)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			cancel()
			if resp.StatusCode < 400 {
				return
			}
			if resp.StatusCode < 500 {
				return // 4xx = the receiver's decision — retries won't change it
			}
			continue // 5xx — receiver may recover
		}
		cancel()
	}
	slog.Warn("webx webhook: delivery failed after retries",
		"job", jobID, "url", hook)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "name": "webx", "ts": time.Now().UTC()})
}

type scrapeReq struct {
	URL         string    `json:"url"`
	Formats     []string  `json:"formats"` // ["markdown"] default; also html,links,screenshot,summary
	Browser     bool      `json:"browser"`
	Render      bool      `json:"render"`
	AutoRender  bool      `json:"auto_render"`
	Session     string    `json:"session"`
	WaitFor     string    `json:"wait_for"`
	WaitMs      int       `json:"wait_ms"` // fixed delay after load — delayed JS injects
	Scrolls     int       `json:"scrolls"`
	Actions     string    `json:"actions"` // "click:.a | wait:.q | screenshot"
	Screenshot  bool      `json:"screenshot"`
	ShotOpts    *shotOpts `json:"screenshot_options"` // Firecrawl v2 shape
	Proxy       string    `json:"proxy"`
	Stealth     bool      `json:"stealth"`
	Profile     string    `json:"profile"`
	BlockAds    bool      `json:"block_ads"` // tracker/ad request interception
	TextMode    bool      `json:"text_mode"` // drop images/fonts/media/css
	Mobile      bool      `json:"mobile"`
	Locale      string    `json:"locale"`
	Timezone    string    `json:"timezone"`
	Network     bool      `json:"capture_network"` // record XHR traffic
	Console     bool      `json:"capture_console"` // record console.* messages
	SkipTLS     bool      `json:"skip_tls_verification"`
	PageSession string    `json:"page_session"` // named live tab across renders
	PierceDOM   bool      `json:"pierce_dom"`   // flatten shadow roots + same-origin iframes
	ScrollSel   string    `json:"scroll_selector"`
	ScrollBy    float64   `json:"scroll_by"`
	Fit         string    `json:"fit"` // BM25-filter markdown to this query
	MaxChars    int       `json:"max_chars"`
	MaxTokens   int       `json:"max_tokens"` // ~4 chars/token cap (Jina X-Token-Budget style)
	Include     []string  `json:"include_tags"`
	Exclude     []string  `json:"exclude_tags"`
	MaxAge      int64     `json:"max_age"` // ms — serve the cached index copy if younger
	ChangeTrack bool      `json:"change_tracking"`
	Engine      string    `json:"engine"`  // "light" → Lightpanda CDP render (default chrome)
	Lang        string    `json:"lang"`    // preferred transcript caption language
	Cookies     string    `json:"cookies"` // Netscape-format cookie text — content, never a path
	RetryAfter  bool      `json:"retry_after"`
	ZDR         bool      `json:"zdr"` // zero data retention — no index/cache persistence (enterprise)
}

// shotOpts mirrors Firecrawl's screenshot_options — full_page, jpeg
// quality, and an explicit viewport for responsive-layout captures.
type shotOpts struct {
	FullPage *bool `json:"full_page"`
	Quality  int   `json:"quality"`
	Viewport struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"viewport"`
}

// shotFields spreads the options onto a FetchRequest.
func (o *shotOpts) shotFields(req *fetch.FetchRequest) {
	if o == nil {
		return
	}
	req.ShotFullPage = o.FullPage
	req.ShotQuality = o.Quality
	req.ViewportW = o.Viewport.Width
	req.ViewportH = o.Viewport.Height
}

// scrape is Firecrawl-compatible in spirit: {url} → {data:{markdown,...}}.
func (s *Server) scrape(w http.ResponseWriter, r *http.Request) {
	var req scrapeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "body must be JSON {url}")
		return
	}
	// Empty URL is legal only as "act on the open page_session tab".
	if req.URL == "" && req.PageSession == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {url}")
		return
	}
	if req.URL != "" {
		if err := s.guard.CheckURL(req.URL); err != nil {
			writeErr(w, http.StatusForbidden, "target refused: "+err.Error())
			return
		}
	}
	if req.Actions != "" {
		if _, err := render.ParseActions(req.Actions); err != nil {
			writeErr(w, http.StatusBadRequest, "actions: "+err.Error())
			return
		}
	}
	// ZDR is the enterprise retention promise — refuse rather than silently
	// persist when the license doesn't cover it.
	if req.ZDR && !s.license.Allows(license.FeatZDR) {
		writeLicenseErr(w, "zdr requires an enterprise license")
		return
	}

	// max_age fast path — a fresh indexed snapshot beats a refetch.
	if req.MaxAge > 0 {
		if idx := s.index(); idx != nil {
			if page, err := idx.Get(r.Context(), req.URL); err == nil &&
				time.Since(page.FetchedAt) < time.Duration(req.MaxAge)*time.Millisecond {
				writeJSON(w, map[string]any{"success": true, "data": map[string]any{
					"url": req.URL, "final_url": page.URL, "title": page.Title,
					"markdown": page.Body, "text_length": len(page.Body),
					"cached_at": page.FetchedAt.UTC().Format(time.RFC3339), "extractor": "index",
				}})
				return
			}
		}
	}

	fmts := map[string]bool{}
	for _, f := range req.Formats {
		fmts[strings.ToLower(f)] = true
	}
	wantShot := req.Screenshot || req.ShotOpts != nil ||
		fmts["screenshot"] || fmts["screenshot@fullpage"]
	wantPDF := fmts["pdf"]
	wantMHTML := fmts["mhtml"]
	wantA11y := fmts["a11y"]

	freq := fetch.FetchRequest{
		URL: req.URL, Browser: req.Browser, Session: req.Session,
		Render:     req.Render || req.Actions != "" || wantShot || wantPDF || wantMHTML || wantA11y || req.Network || req.Console,
		AutoRender: req.AutoRender,
		WaitFor:    req.WaitFor, WaitMs: req.WaitMs, Scrolls: req.Scrolls,
		Actions: req.Actions, Screenshot: wantShot, Proxy: req.Proxy,
		Stealth: req.Stealth, Profile: req.Profile,
		BlockTrackers: req.BlockAds, BlockMedia: req.TextMode,
		Mobile: req.Mobile, Locale: req.Locale, Timezone: req.Timezone,
		NetworkCapture: req.Network, ConsoleCapture: req.Console,
		WantPDF: wantPDF, WantMHTML: wantMHTML, SkipTLS: req.SkipTLS,
		PageSession: req.PageSession, PierceDOM: req.PierceDOM,
		ScrollSelector: req.ScrollSel, ScrollBy: req.ScrollBy,
		Fit:              req.Fit,
		IncludeSelectors: req.Include, ExcludeSelectors: req.Exclude,
		WantHTML:   fmts["html"] || fmts["rawhtml"],
		WantLinks:  fmts["links"],
		WantImages: fmts["images"],
		WantBrand:  fmts["branding"],
		Summary:    fmts["summary"],
		WantChunks: fmts["chunks"], WantA11y: wantA11y,
		WantAgentReady: fmts["agent_ready"],
		Transcript:     fmts["transcript"], TranscriptLang: req.Lang,
		RedactPII:    fmts["redact_pii"],
		RenderEngine: req.Engine,
		CookieText:   req.Cookies, RetryAfter: req.RetryAfter,
	}
	req.ShotOpts.shotFields(&freq)
	doc, err := fetchFn(r.Context(), freq)
	if err != nil {
		writeFetchErr(w, err)
		return
	}
	maxChars := req.MaxChars
	if req.MaxTokens > 0 && (maxChars == 0 || req.MaxTokens*4 < maxChars) {
		maxChars = req.MaxTokens * 4
	}
	if maxChars > 0 && len(doc.Markdown) > maxChars {
		doc.Markdown = doc.Markdown[:maxChars]
		doc.Truncated = true
	}

	// Flatten the document fields for Firecrawl-ish clients; extras merge on.
	docJSON, _ := json.Marshal(doc)
	var flat map[string]any
	_ = json.Unmarshal(docJSON, &flat)
	if req.ChangeTrack && !req.ZDR {
		flat["change_tracking"] = s.changeTrack(r.Context(), req.URL, doc)
	}
	if req.ZDR {
		flat["zdr"] = true // receipt for the retention promise
	}
	writeJSON(w, map[string]any{"success": true, "data": flat})
}

// changeTrack diffs the fresh document against the indexed snapshot and
// stores the new one — Firecrawl's changeTracking, backed by our index.
func (s *Server) changeTrack(ctx context.Context, rawURL string, doc *fetch.Document) map[string]any {
	out := map[string]any{"change_status": "new"}
	idx := s.index()
	if idx == nil {
		out["change_status"] = "unavailable"
		out["error"] = "no index"
		return out
	}
	prev, err := idx.Get(ctx, rawURL)
	if err == nil && prev.Body != "" {
		out["previous_scrape_at"] = prev.FetchedAt.UTC().Format(time.RFC3339)
		if prev.Body == doc.Markdown {
			out["change_status"] = "same"
		} else {
			out["change_status"] = "changed"
			a, rm := countDiff(prev.Body, doc.Markdown)
			out["added_lines"] = a
			out["removed_lines"] = rm
		}
	}
	_ = idx.Put(ctx, index.Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown})
	return out
}

// countDiff reports line-level +/− counts between two texts (LCS-lite:
// multiset difference — good enough for a change signal).
func countDiff(oldText, newText string) (added, removed int) {
	old := map[string]int{}
	for _, l := range strings.Split(oldText, "\n") {
		old[l]++
	}
	for _, l := range strings.Split(newText, "\n") {
		if old[l] > 0 {
			old[l]--
		} else {
			added++
		}
	}
	for _, c := range old {
		removed += c
	}
	return added, removed
}

type searchReq struct {
	Query          string   `json:"query"`
	Limit          int      `json:"limit"`
	Providers      []string `json:"providers"`
	Scrape         bool     `json:"scrape"`
	ScrapeChars    int      `json:"scrape_chars"`    // cap per-result content chars
	HighlightsOnly bool     `json:"highlights_only"` // excerpts only, no full content (token-saver)
	Rerank         bool     `json:"rerank"`
	Site           string   `json:"site"`
	Domains        []string `json:"domains"`         // Exa includeDomains
	ExcludeDomains []string `json:"exclude_domains"` // Exa excludeDomains
	After          string   `json:"after"`           // RFC3339 or YYYY-MM-DD — Exa startPublishedDate
	Before         string   `json:"before"`          // Exa endPublishedDate
	Topic          string   `json:"topic"`           // "news" → freshness + news providers
	Lang           string   `json:"lang"`
	Location       string   `json:"location"` // ISO country hint (Exa userLocation / FC location)
	Category       string   `json:"category"` // vertical preset: developer → code/docs providers only
	Answer         bool     `json:"answer"`   // cited extractive answer (Tavily include_answer)
	Exact          bool     `json:"exact"`    // phrase-match verbatim
	Semantic       bool     `json:"semantic"`
	Fresh          bool     `json:"fresh"`          // bypass the result cache — still writes
	Render         bool     `json:"render"`         // render every scraped result
	AutoRender     bool     `json:"auto_render"`    // escalate scrapes on JS-shells/bot-walls
	Browser        bool     `json:"browser"`        // Chrome-fingerprint transport for scrapes
	Session        string   `json:"session"`        // cookie jar for provider calls + scrapes
	Depth          string   `json:"depth"`          // fast | basic | advanced (Tavily search_depth)
	Sources        []string `json:"sources"`        // web (default) | news | images
	Subpages       int      `json:"subpages"`       // subpages to crawl per top result (Exa)
	SubpageTarget  []string `json:"subpage_target"` // keywords steering subpage picks
	Collection     string   `json:"collection"`     // named local-index corpus
}

// search answers Firecrawl's /search shape: {query} → {data:[{url,...}]} —
// plus webx extras (sources, errors) that clients can ignore harmlessly.
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	var req searchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Query == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {query}")
		return
	}
	after, err := parseDate(req.After)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "after: "+err.Error())
		return
	}
	before, err := parseDate(req.Before)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "before: "+err.Error())
		return
	}
	scrape, highlightsOnly := req.Scrape, req.HighlightsOnly
	if req.Answer && !scrape {
		// Answers need evidence — scrape top results at highlights depth.
		scrape, highlightsOnly = true, true
	}
	resp := searchFn(r.Context(), search.Request{
		Query: req.Query, Num: req.Limit, Providers: req.Providers,
		Site: req.Site, Scrape: scrape, ScrapeChars: req.ScrapeChars,
		HighlightsOnly: highlightsOnly, Rerank: req.Rerank, Answer: req.Answer,
		Domains: req.Domains, ExcludeDomains: req.ExcludeDomains,
		After: after, Before: before, Topic: req.Topic, Lang: req.Lang,
		Location: req.Location, Category: req.Category,
		Exact: req.Exact, Semantic: req.Semantic, Fresh: req.Fresh,
		Render: req.Render, AutoRender: req.AutoRender,
		Browser: req.Browser, Session: req.Session,
		Depth: req.Depth, Sources: req.Sources,
		Subpages: req.Subpages, SubpageTarget: req.SubpageTarget,
		Collection: req.Collection,
	})
	out := map[string]any{"success": true}
	// Typed sources get a Firecrawl/Exa-shaped grouped payload; a plain
	// web search keeps the flat array for backward compatibility.
	if typed := typedSources(req.Sources); typed {
		out["data"] = groupByType(resp.Results)
	} else {
		out["data"] = resp.Results
	}
	if len(resp.Errors) > 0 {
		out["errors"] = resp.Errors
	}
	if len(resp.ProviderMs) > 0 {
		out["provider_ms"] = resp.ProviderMs
	}
	if resp.CacheHit {
		out["cache_hit"] = true
	}
	if resp.Answer != "" {
		out["answer"] = resp.Answer
	}
	writeJSON(w, out)
}

// typedSources reports whether the request asks for non-web verticals.
func typedSources(sources []string) bool {
	for _, s := range sources {
		if s == "news" || s == "images" {
			return true
		}
	}
	return false
}

// groupByType buckets fused results by vertical — web results stay
// untyped in the "web" bucket so mixed sources never lose hits.
func groupByType(results []search.Result) map[string]any {
	out := map[string]any{"web": []search.Result{}, "news": []search.Result{}, "images": []search.Result{}}
	for _, r := range results {
		switch r.Type {
		case "news":
			out["news"] = append(out["news"].([]search.Result), r)
		case "images":
			out["images"] = append(out["images"].([]search.Result), r)
		default:
			out["web"] = append(out["web"].([]search.Result), r)
		}
	}
	return out
}

// parseDate accepts RFC3339 or bare YYYY-MM-DD (Exa's startPublishedDate
// takes ISO-8601; bare dates are friendlier for agents typing requests).
func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", s)
}

type answerReq struct {
	Query     string   `json:"query"`
	Num       int      `json:"num"`       // sources to cite (default 5)
	Providers []string `json:"providers"` // restrict the fused-search set
	Domains   []string `json:"domains"`   // Exa includeDomains
	LLM       bool     `json:"llm"`       // synthesize via WEBX_LLM_* (else extractive only)
	MaxTokens int      `json:"max_tokens"`
}

// answer is Exa's /answer + Tavily's include_answer — the product they
// bill for. We ground every response in extractive evidence (free,
// deterministic) and only mark answer_type:"llm" when a configured LLM
// actually synthesized; when none did the caller gets citations they can
// synthesize themselves instead of a confident-sounding guess.
func (s *Server) answer(w http.ResponseWriter, r *http.Request) {
	var req answerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Query == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {query}")
		return
	}
	num := req.Num
	if num <= 0 {
		num = 5
	}
	resp := searchFn(r.Context(), search.Request{
		Query: req.Query, Num: num, Providers: req.Providers,
		Domains: req.Domains, Scrape: true, ScrapeChars: 8000,
	})

	budget := req.MaxTokens * 4
	if budget <= 0 {
		budget = 12000
	}
	cites, evidence := answerSources(resp, budget)

	out := map[string]any{
		"success":     true,
		"answer":      nil,
		"answer_type": "extractive",
		"evidence_md": evidence,
		"citations":   cites,
	}
	if req.LLM && evidence != "" {
		sys := "You answer questions using only the numbered web sources provided. " +
			"Cite claims with [n] markers matching the source numbers. Be precise; " +
			"say when sources disagree or are silent on part of the question."
		user := fmt.Sprintf("Question: %s\n\nSources:\n%s", req.Query, evidence)
		if text, err := fetch.LLMChat(r.Context(), sys, user, false); err == nil {
			out["answer"] = text
			out["answer_type"] = "llm"
			// ALCE-style groundedness: every [n] marker must tag a sentence
			// the source actually supports. Cheap lexical proxy, honest flag.
			v, u := VerifyCitations(text, evidenceSections(evidence))
			if len(v)+len(u) > 0 {
				out["citation_check"] = map[string]any{"verified": v, "unverified": u}
			}
		} else {
			out["llm_error"] = err.Error()
		}
	}
	if len(resp.Errors) > 0 {
		out["errors"] = resp.Errors
	}
	if len(resp.ProviderMs) > 0 {
		out["provider_ms"] = resp.ProviderMs
	}
	if resp.CacheHit {
		out["cache_hit"] = true
	}
	writeJSON(w, out)
}

// answerSources renders the numbered-source evidence block (same shape as
// `webx ask`) and the parallel citations array — [n] markers line up.
func answerSources(resp *search.Response, budget int) ([]map[string]any, string) {
	var cites []map[string]any
	var b strings.Builder
	n := 0
	for _, r := range resp.Results {
		if len(r.Highlights) == 0 && r.Content == "" && r.Snippet == "" {
			continue
		}
		var sec strings.Builder
		fmt.Fprintf(&sec, "## [%d] %s\n%s\n\n", n+1, r.Title, r.URL)
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
		if b.Len()+sec.Len() > budget {
			break
		}
		b.WriteString(sec.String())
		n++
		cite := map[string]any{"n": n, "url": r.URL, "title": r.Title}
		if len(r.Highlights) > 0 {
			cite["highlights"] = r.Highlights
		}
		if r.Snippet != "" {
			cite["snippet"] = r.Snippet
		}
		if !r.Published.IsZero() {
			cite["published"] = r.Published.Format(time.RFC3339)
		}
		cites = append(cites, cite)
	}
	return cites, b.String()
}

type mapReq struct {
	URL   string `json:"url"`
	Limit int    `json:"limit"`
}

func (s *Server) mapURLs(w http.ResponseWriter, r *http.Request) {
	var req mapReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {url}")
		return
	}
	u := req.URL
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	if err := s.guard.CheckURL(u); err != nil {
		writeErr(w, http.StatusForbidden, "target refused: "+err.Error())
		return
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
	writeJSON(w, map[string]any{"success": true, "links": urls})
}

type extractReq struct {
	URL     string         `json:"url"`
	URLs    []string       `json:"urls"` // multi-url — or "example.com/*" wildcard
	Schema  map[string]any `json:"schema"`
	CSS     map[string]any `json:"css"` // selector schema — no-LLM deterministic path
	Prompt  string         `json:"prompt"`
	Browser bool           `json:"browser"`
	Render  bool           `json:"render"`
	Session string         `json:"session"`
	Async   bool           `json:"async"` // queue as an extract job (required for wildcards)
	Limit   int            `json:"limit"` // wildcard discovery cap (default 50)
	Webhook string         `json:"webhook_url"`
	Secret  string         `json:"webhook_secret"`
}

func (s *Server) extract(w http.ResponseWriter, r *http.Request) {
	var req extractReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.URL == "" && len(req.URLs) == 0) {
		writeErr(w, http.StatusBadRequest, "body must be JSON {url, schema}")
		return
	}
	wildcard := strings.Contains(req.URL, "*")
	for _, u := range req.URLs {
		if strings.Contains(u, "*") {
			wildcard = true
		}
	}
	// Wildcards and explicit async go through the job pipeline — a domain
	// crawl+extract can take minutes, far past a polite request timeout.
	if req.Async || wildcard || len(req.URLs) > 1 {
		if s.store == nil {
			writeErr(w, http.StatusServiceUnavailable,
				"multi-url/wildcard extract needs a job store (webx serve --store or webxd)")
			return
		}
		if !wildcard {
			if err := s.guard.CheckURL(req.URL); err != nil && req.URL != "" {
				writeErr(w, http.StatusForbidden, "target refused: "+err.Error())
				return
			}
		}
		params, _ := json.Marshal(req)
		job := &store.Job{ID: store.NewID(), Kind: "extract", Params: injectIdem(params, r)}
		if err := s.store.CreateJob(r.Context(), job); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"success": true, "id": job.ID, "url": "/crawl/" + job.ID})
		return
	}
	if err := s.guard.CheckURL(req.URL); err != nil {
		writeErr(w, http.StatusForbidden, "target refused: "+err.Error())
		return
	}
	// CSS-selector extraction — deterministic, zero LLM cost (Crawl4AI's
	// JsonCssExtractionStrategy equivalent).
	if len(req.CSS) > 0 {
		doc, err := fetchFn(r.Context(), fetch.FetchRequest{
			URL: req.URL, Browser: req.Browser, Session: req.Session,
			Render: req.Render, WantHTML: true,
		})
		if err != nil {
			writeFetchErr(w, err)
			return
		}
		data, err := fetch.ExtractCSS([]byte(doc.HTML), req.CSS)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]any{"success": true, "data": data, "url": doc.FinalURL})
		return
	}
	res, err := fetch.Extract(r.Context(), fetch.ExtractRequest{
		URL: req.URL, Schema: req.Schema, Prompt: req.Prompt,
		Browser: req.Browser, Session: req.Session,
	})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": res.OK, "data": res.Data, "model": res.Model, "err": res.Err})
}

// research runs the deep-research pipeline — Tavily's /research equivalent.
// async:true queues it as a job: reports take minutes and webhooks/SSE
// fit long runs better than a held-open request.
func (s *Server) research(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Query        string         `json:"query"`
		MaxSources   int            `json:"max_sources"`
		SubQueries   int            `json:"sub_queries"`
		OutputSchema map[string]any `json:"output_schema"` // Tavily parity
		Async        bool           `json:"async"`
		Webhook      string         `json:"webhook_url"`
		Secret       string         `json:"webhook_secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.Query == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {query}")
		return
	}
	if p.Async {
		if s.store == nil {
			writeErr(w, http.StatusServiceUnavailable, "async needs a job store")
			return
		}
		params, _ := json.Marshal(p)
		job := &store.Job{ID: store.NewID(), Kind: "research", Params: injectIdem(params, r)}
		if err := s.store.CreateJob(r.Context(), job); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"success": true, "id": job.ID, "url": "/crawl/" + job.ID})
		return
	}
	rep, err := research.Run(r.Context(), p.Query,
		research.Options{MaxSources: p.MaxSources, SubQueries: p.SubQueries, Schema: p.OutputSchema}, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "data": rep})
}

// verify fact-checks a claim — Jina's g.jina.ai grounding equivalent.
func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Claim      string `json:"claim"`
		MaxSources int    `json:"max_sources"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.Claim == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {claim}")
		return
	}
	v, err := research.Verify(r.Context(), p.Claim, p.MaxSources, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "data": v})
}

// wayback replays an archived capture — {url} lists snapshots,
// {url, at} fetches the nearest one extracted like a live page.
func (s *Server) wayback(w http.ResponseWriter, r *http.Request) {
	var p struct {
		URL string `json:"url"`
		At  string `json:"at"` // yyyy|yyyymmdd|yyyymmddhhmmss — nearest capture
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.URL == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {url}")
		return
	}
	target := p.URL
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	if p.At == "" {
		snaps, err := index.WaybackSnapshots(r.Context(), target, "", "", 200)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, map[string]any{"success": true, "data": snaps})
		return
	}
	body, ct, ts, err := index.FetchWayback(r.Context(), target, p.At)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	doc, err := fetch.ExtractFromBody(body, ct, target)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	doc.TierUsed = "wayback"
	doc.Warnings = append([]string{"wayback capture " + ts + " — content may be stale or rewritten"}, doc.Warnings...)
	writeJSON(w, map[string]any{"success": true, "data": doc})
}

// similar finds pages like a given URL — Exa's findSimilar. Default mode
// searches the local index (FTS over our corpus); web:true distills the
// page's signature terms and hits live providers, which is closer to
// Exa's actual product for URLs you've never indexed.
func (s *Server) similar(w http.ResponseWriter, r *http.Request) {
	var p struct {
		URL   string `json:"url"`
		Limit int    `json:"limit"`
		Web   bool   `json:"web"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.URL == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {url}")
		return
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 10
	}
	idx := s.index()
	if !p.Web {
		if idx == nil {
			writeErr(w, http.StatusServiceUnavailable, "no local index — run webx index/seed first")
			return
		}
		hits, err := index.Similar(r.Context(), idx, p.URL, limit)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, map[string]any{"success": true, "data": hits})
		return
	}

	// Web mode: fetch the page (or reuse its index entry), distill the
	// signature, query live providers, drop the page itself.
	var page *index.Page
	if idx != nil {
		page, _ = idx.Get(r.Context(), p.URL)
	}
	if page == nil {
		doc, ferr := fetchFn(r.Context(), fetch.FetchRequest{URL: p.URL})
		if ferr != nil {
			writeErr(w, http.StatusBadGateway, "fetch source page: "+ferr.Error())
			return
		}
		page = &index.Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown}
	}
	terms := index.SignatureTerms(page)
	if terms == "" {
		writeErr(w, http.StatusUnprocessableEntity, "couldn't distill a signature from "+p.URL)
		return
	}
	resp := searchFn(r.Context(), search.Request{Query: terms, Num: limit + 4})
	self := strings.TrimSuffix(strings.TrimPrefix(
		strings.TrimPrefix(strings.TrimPrefix(page.URL, "https://"), "http://"), "www."), "/")
	out := make([]search.Result, 0, limit)
	for _, res := range resp.Results {
		u := strings.TrimSuffix(strings.TrimPrefix(
			strings.TrimPrefix(strings.TrimPrefix(res.URL, "https://"), "http://"), "www."), "/")
		if u == self {
			continue
		}
		out = append(out, res)
		if len(out) >= limit {
			break
		}
	}
	payload := map[string]any{"success": true, "data": out}
	if len(resp.Errors) > 0 {
		payload["errors"] = resp.Errors
	}
	writeJSON(w, payload)
}

// openapi serves the embedded OpenAPI 3 spec — generated-client tooling
// and humans both get the full route surface from one file.
//
//go:embed openapi.json
var openapiSpec []byte

func (s *Server) openapi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(openapiSpec)
}

// pageMD serves a stored snapshot back by URL — Olostep's markdown_hosted_url
// equivalent, backed by our index instead of a page store.
// GET /pages/md?url=https://… → text/markdown body (404 when not indexed).
func (s *Server) pageMD(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("url")
	if u == "" {
		writeErr(w, http.StatusBadRequest, "?url= required")
		return
	}
	idx := s.index()
	if idx == nil {
		writeErr(w, http.StatusServiceUnavailable, "no index")
		return
	}
	page, err := idx.Get(r.Context(), u)
	if err != nil || page.Body == "" {
		writeErr(w, http.StatusNotFound, "not indexed: "+u)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("X-Source-URL", page.URL)
	w.Header().Set("X-Fetched-At", page.FetchedAt.UTC().Format(time.RFC3339))
	_, _ = w.Write([]byte(page.Body))
}

// jobEvents streams job progress as SSE — Firecrawl's crawl-ws/Tavily stream
// equivalent. data: {id,status,completed,total,pages} every ~1s until the
// job reaches a terminal state, then the connection closes.
func (s *Server) jobEvents(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no job store configured")
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	id := r.PathValue("id")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	for {
		job, err := s.store.GetJob(r.Context(), id)
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: {\"error\":%q}\n\n", err.Error())
			fl.Flush()
			return
		}
		pages, _ := s.store.Pages(r.Context(), id, 1, 0)
		payload, _ := json.Marshal(map[string]any{
			"id": job.ID, "status": job.Status, "kind": job.Kind,
			"completed": job.Done, "total": job.Total,
			"has_pages": len(pages) > 0, "error": job.Error,
		})
		fmt.Fprintf(w, "data: %s\n\n", payload)
		fl.Flush()
		switch job.Status {
		case store.Completed, store.Failed, store.Cancelled:
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(1200 * time.Millisecond):
		}
	}
}

// crawlStart queues an adaptive crawl — Firecrawl's async /crawl shape:
// {url, ...} → {success, id, url}.
func (s *Server) crawlStart(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no job store configured")
		return
	}
	var p struct {
		URL         string `json:"url"`
		Goal        string `json:"goal"`
		Limit       int    `json:"limit"`
		Depth       int    `json:"depth"`
		Semantic    bool   `json:"semantic"`    // embedding-scored relevance (WEBX_EMBED_*)
		Concurrency int    `json:"concurrency"` // parallel page fetches
		Webhook     string `json:"webhook_url"`
		Secret      string `json:"webhook_secret"` // HMAC key for signed webhooks
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.URL == "" {
		writeErr(w, http.StatusBadRequest, "body must be JSON {url, goal?}")
		return
	}
	if !strings.Contains(p.URL, "://") {
		p.URL = "https://" + p.URL
	}
	if err := s.guard.CheckURL(p.URL); err != nil {
		writeErr(w, http.StatusForbidden, "target refused: "+err.Error())
		return
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 30
	}
	if prev := s.idempotentJob(r, "crawl"); prev != nil {
		writeJSON(w, map[string]any{
			"success": true, "id": prev.ID, "url": "/v1/crawl/" + prev.ID,
			"warning": "idempotent replay — returning existing job",
		})
		return
	}
	params, _ := json.Marshal(p)
	job := &store.Job{ID: store.NewID(), Kind: "crawl", Total: limit, Params: injectIdem(params, r)}
	if err := s.store.CreateJob(r.Context(), job); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"success": true, "id": job.ID,
		"url": "/v1/crawl/" + job.ID, // firecrawl-style status path
	})
}

// batchStart queues a multi-URL scrape job — {urls:[...], options:{…}} → job.
func (s *Server) batchStart(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no job store configured")
		return
	}
	var p struct {
		URLs    []string  `json:"urls"`
		Options scrapeReq `json:"options"`
		Webhook string    `json:"webhook_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || len(p.URLs) == 0 {
		writeErr(w, http.StatusBadRequest, "body must be JSON {urls:[...]}")
		return
	}
	if len(p.URLs) > 200 {
		writeErr(w, http.StatusBadRequest, "max 200 urls per batch")
		return
	}
	var kept []string
	for _, u := range p.URLs {
		if s.guard.CheckURL(u) == nil {
			kept = append(kept, u)
		}
	}
	if len(kept) == 0 {
		writeErr(w, http.StatusForbidden, "all urls refused by target policy")
		return
	}
	p.URLs = kept
	if prev := s.idempotentJob(r, "batch"); prev != nil {
		writeJSON(w, map[string]any{
			"success": true, "id": prev.ID, "url": "/v1/crawl/" + prev.ID,
			"warning": "idempotent replay — returning existing job",
		})
		return
	}
	params, _ := json.Marshal(p)
	job := &store.Job{ID: store.NewID(), Kind: "batch", Total: len(p.URLs), Params: injectIdem(params, r)}
	if err := s.store.CreateJob(r.Context(), job); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"success": true, "id": job.ID,
		"url": "/v1/crawl/" + job.ID,
	})
}

// idempotentJob returns an existing job for an Idempotency-Key header —
// retries of the same POST don't spawn duplicate crawls. The key rides in
// job params; we scan recent jobs for a match.
func (s *Server) idempotentJob(r *http.Request, kind string) *store.Job {
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len(key) > 128 {
		return nil
	}
	jobs, err := s.store.ListJobs(r.Context(), 100)
	if err != nil {
		return nil
	}
	for _, j := range jobs {
		if j.Kind != kind {
			continue
		}
		var p struct {
			Idem string `json:"_idem"`
		}
		if json.Unmarshal(j.Params, &p) == nil && p.Idem == key {
			return j
		}
	}
	return nil
}

// injectIdem merges the Idempotency-Key into raw job params JSON.
func injectIdem(params json.RawMessage, r *http.Request) json.RawMessage {
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len(key) > 128 {
		return params
	}
	var m map[string]any
	_ = json.Unmarshal(params, &m)
	if m == nil {
		m = map[string]any{}
	}
	m["_idem"] = key
	out, _ := json.Marshal(m)
	return out
}

// jobCancel marks a queued/running job cancelled — the worker stops at the
// next page boundary; pages already fetched stay queryable.
func (s *Server) jobCancel(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no job store configured")
		return
	}
	id := r.PathValue("id")
	job, err := s.store.GetJob(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "job not found")
		return
	}
	if job.Status != store.Queued && job.Status != store.Running {
		writeErr(w, http.StatusConflict, "job already "+string(job.Status))
		return
	}
	if err := s.store.UpdateJob(r.Context(), id, func(j *store.Job) {
		j.Status = store.Cancelled
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "id": id, "status": "cancelled"})
}

// crawlStatus reports job progress + pages — GET /crawl/{id} →
// {status, total, completed, data:[...]}. Pages paginate via
// ?limit=&offset= so big crawls don't wedge into one response.
func (s *Server) crawlStatus(w http.ResponseWriter, r *http.Request) {
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
	out := map[string]any{
		"success":    true,
		"status":     string(job.Status),
		"total":      job.Total,
		"completed":  job.Done,
		"created_at": job.CreatedAt,
		"data":       pages,
		"page":       map[string]int{"limit": limit, "offset": offset, "count": len(pages)},
	}
	if len(pages) == limit {
		out["next_offset"] = offset + limit // more pages may exist
	}
	if job.Error != "" {
		out["error"] = job.Error
	}
	if job.Result != "" {
		out["result"] = job.Result // research/extract/agent job payloads
	}
	writeJSON(w, out)
}

// pageParams parses ?limit=&offset= with sane bounds.
func pageParams(r *http.Request, defLimit, maxLimit int) (int, int) {
	limit := defLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &limit); err != nil || n == 0 {
			limit = defLimit
		}
	}
	if limit <= 0 {
		limit = defLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	offset := 0
	fmt.Sscanf(r.URL.Query().Get("offset"), "%d", &offset)
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// jobErrors surfaces just the failed pages — the crawl errors view — so
// a 500-page crawl doesn't bury the 3 pages that refused.
func (s *Server) jobErrors(w http.ResponseWriter, r *http.Request) {
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
	errs := make([]map[string]any, 0)
	for _, p := range pages {
		var meta map[string]any
		if json.Unmarshal(p.Meta, &meta) != nil {
			continue
		}
		if e, ok := meta["error"].(string); ok && e != "" {
			errs = append(errs, map[string]any{
				"url": p.URL, "error": e,
				"code": meta["code"], "failed_at": p.CreatedAt,
			})
		}
	}
	writeJSON(w, map[string]any{
		"success": true, "job": job.ID, "status": string(job.Status),
		"errors": errs,
		"page":   map[string]int{"limit": limit, "offset": offset, "count": len(errs)},
	})
}

// jobRetry requeues a job's failed pages as a fresh batch job — crawl
// frontier state isn't persisted so a same-job resume isn't honest; a
// batch of exactly the failed URLs is the faithful retry semantics.
func (s *Server) jobRetry(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no job store configured")
		return
	}
	job, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "job not found")
		return
	}
	pages, _ := s.store.Pages(r.Context(), job.ID, 5000, 0)
	var failed []string
	for _, p := range pages {
		var meta map[string]any
		if json.Unmarshal(p.Meta, &meta) != nil {
			continue
		}
		if e, ok := meta["error"].(string); ok && e != "" && s.guard.CheckURL(p.URL) == nil {
			failed = append(failed, p.URL)
		}
	}
	if len(failed) == 0 {
		writeErr(w, http.StatusBadRequest, "job has no failed pages to retry")
		return
	}
	params, _ := json.Marshal(map[string]any{
		"urls":        failed,
		"webhook_url": r.URL.Query().Get("webhook_url"),
		"_retry_of":   job.ID,
	})
	nj := &store.Job{ID: store.NewID(), Kind: "batch", Total: len(failed), Params: params}
	if err := s.store.CreateJob(r.Context(), nj); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"success": true, "id": nj.ID, "retry_of": job.ID,
		"retried": len(failed), "url": "/crawl/" + nj.ID,
	})
}

func (s *Server) jobs(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no job store configured")
		return
	}
	js, err := s.store.ListJobs(r.Context(), 50)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "jobs": js})
}

func (s *Server) doctor(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"success": true, "providers": search.Diagnose(r.Context(), "")})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"success": false, "error": msg})
}

// writeLicenseErr is writeErr for gated features — the 402 body carries where
// to buy a license so a blocked request is also the upsell.
func writeLicenseErr(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(map[string]any{
		"success": false, "error": msg, "upgrade": upgradeURL(),
	})
}

// upgradeURL is where a free-tier deployment's operators buy a license.
// Operators reselling webx override it with WEBX_UPGRADE_URL.
func upgradeURL() string {
	if u := os.Getenv("WEBX_UPGRADE_URL"); u != "" {
		return u
	}
	return "https://github.com/kasyap1234/webx#pricing"
}

// licenseInfo — GET /license. Buyers verify their tier is active; we surface
// only what an operator needs to confirm (never the signature/email).
func (s *Server) licenseInfo(w http.ResponseWriter, r *http.Request) {
	l := s.license
	out := map[string]any{
		"success":  true,
		"tier":     l.TierName(),
		"upgrade":  upgradeURL(),
		"features": []string{},
	}
	if l != nil {
		out["id"] = l.ID
		out["expires"] = l.Expires
		out["features"] = l.Features
	}
	writeJSON(w, out)
}

// writeFetchErr surfaces a fetch failure with its stable machine code —
// payment_required carries the gate details so an agent's wallet layer can
// act; blocked_by_policy stays 403; everything else is a 502 upstream.
func writeFetchErr(w http.ResponseWriter, err error) {
	code := fetch.ErrorCode(err)
	status := http.StatusBadGateway
	switch code {
	case fetch.CodeBlockedByPolicy:
		status = http.StatusForbidden
	case fetch.CodePaymentRequired:
		status = http.StatusPaymentRequired
	case fetch.CodeInvalidURL, fetch.CodeUnsupportedType:
		status = http.StatusBadRequest
	case fetch.CodeRateLimited:
		status = http.StatusTooManyRequests
	}
	body := map[string]any{"success": false, "error": err.Error()}
	if code != "" {
		body["code"] = code
	}
	if p := fetch.PaymentOf(err); p != nil {
		body["payment"] = p
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

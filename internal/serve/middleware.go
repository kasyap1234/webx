package serve

// middleware.go — cross-cutting HTTP concerns kept out of the handler:
// request IDs, panic recovery, body size caps, CORS, gzip, and request
// metrics. All of it wraps the mux in Server.Handler so every route —
// including Firecrawl compat — gets the same treatment.

import (
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxRequestBody caps JSON bodies. The largest legit payload is a batch
// (≤200 urls) or inline cookies — single-digit MB tops. Anything bigger is
// a bug or a DoS; MaxBytesReader turns it into a decode error the
// handlers already report as 400.
const maxRequestBody = 4 << 20 // 4 MiB

// reqIDCKey carries the request id through context — distinct name from
// billing.go's ctxKey so the two context slots never collide.
type reqIDCKey struct{}

var reqIDKey reqIDCKey

// RequestID returns the id assigned to this request's context ("" if none).
func RequestID(r *http.Request) string {
	if v, ok := r.Context().Value(reqIDKey).(string); ok {
		return v
	}
	return ""
}

// newRequestID honors an inbound X-Request-Id (bounded length, printable)
// so calls can be correlated through an agent's logs; else mint one.
func newRequestID(r *http.Request) string {
	if in := r.Header.Get("X-Request-Id"); in != "" && len(in) <= 64 && strings.IndexFunc(in, func(r rune) bool {
		return r < 0x20 || r > 0x7e
	}) == -1 {
		return in
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}

// ── CORS ────────────────────────────────────────────────────────────────

// corsConfig parses WEBX_CORS_ORIGINS — comma-separated origins or "*".
// Empty (default) = CORS off: an API for agents/CLIs shouldn't answer
// browser cross-origin calls unless the operator opts in.
type corsConfig struct {
	any     bool
	origins map[string]bool
}

func corsFromEnv() *corsConfig {
	v := os.Getenv("WEBX_CORS_ORIGINS")
	if v == "" {
		return nil
	}
	c := &corsConfig{origins: map[string]bool{}}
	for _, o := range strings.Split(v, ",") {
		o = strings.TrimSpace(o)
		if o == "*" {
			c.any = true
		} else if o != "" {
			c.origins[o] = true
		}
	}
	return c
}

// handle applies CORS headers; returns false when a preflight was answered.
func (c *corsConfig) handle(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // non-browser request — nothing to do
	}
	if !c.any && !c.origins[origin] {
		// Disallowed origin: still serve the request (curl doesn't care)
		// but emit no ACAO so browsers block it.
	} else if c.any {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
	}
	if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
		if c.any || c.origins[origin] {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-Id")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	return true
}

// ── gzip ────────────────────────────────────────────────────────────────

// gzipResponseWriter compresses responses when the client accepts gzip.
// SSE routes (/crawl/{id}/events) bypass it — buffering would delay events.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz    *gzip.Writer
	wrote bool
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if !g.wrote {
		g.wrote = true
		g.Header().Set("Content-Encoding", "gzip")
		g.Header().Del("Content-Length")
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wrote {
		g.WriteHeader(http.StatusOK)
	}
	return g.gz.Write(b)
}

func wantGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") &&
		!strings.HasSuffix(r.URL.Path, "/events")
}

// ── metrics ─────────────────────────────────────────────────────────────

// metrics keeps tiny in-process counters — a Prometheus exposition without
// the client dependency. Values are process-local (documented as such).
type metrics struct {
	started  time.Time
	requests atomic.Int64
	errs     atomic.Int64 // 5xx responses
	sumMs    atomic.Int64
	units    atomic.Int64 // metered credit-units
	fetches  atomic.Int64
	byPath   sync.Map // path -> *pathStats
}

type pathStats struct {
	count atomic.Int64
	sumMs atomic.Int64
	errs  atomic.Int64
}

var serverMetrics = &metrics{started: time.Now()}

func (m *metrics) observe(path string, ms int64, status int) {
	m.requests.Add(1)
	m.sumMs.Add(ms)
	if status >= 500 {
		m.errs.Add(1)
	}
	v, _ := m.byPath.LoadOrStore(path, &pathStats{})
	ps := v.(*pathStats)
	ps.count.Add(1)
	ps.sumMs.Add(ms)
	if status >= 500 {
		ps.errs.Add(1)
	}
}

// render emits Prometheus text exposition format.
func (m *metrics) render(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	var b strings.Builder
	b.WriteString("# HELP webx_requests_total HTTP requests by route\n")
	b.WriteString("# TYPE webx_requests_total counter\n")
	m.byPath.Range(func(k, v any) bool {
		ps := v.(*pathStats)
		path := strings.ReplaceAll(k.(string), `"`, `\"`)
		b.WriteString("webx_requests_total{route=\"" + path + "\"} " + itoa64(ps.count.Load()) + "\n")
		b.WriteString("webx_request_errors_total{route=\"" + path + "\"} " + itoa64(ps.errs.Load()) + "\n")
		b.WriteString("webx_request_ms_sum{route=\"" + path + "\"} " + itoa64(ps.sumMs.Load()) + "\n")
		return true
	})
	b.WriteString("# HELP webx_meter_units_total Metered credit units\n")
	b.WriteString("# TYPE webx_meter_units_total counter\n")
	b.WriteString("webx_meter_units_total " + itoa64(m.units.Load()) + "\n")
	b.WriteString("# HELP webx_uptime_seconds Process uptime\n")
	b.WriteString("# TYPE webx_uptime_seconds gauge\n")
	b.WriteString("webx_uptime_seconds " + itoa64(int64(time.Since(m.started).Seconds())) + "\n")
	_, _ = w.Write([]byte(b.String()))
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// ── wrappers ────────────────────────────────────────────────────────────

// recoverGuard turns a handler panic into a controlled 500 carrying the
// request id — one bad page must never take the server down.
func (s *Server) recoverGuard(w http.ResponseWriter, r *http.Request, fn func()) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("webx handler panic",
				"req", RequestID(r), "path", r.URL.Path, "panic", rec)
			writeErr(w, http.StatusInternalServerError, "internal error ("+RequestID(r)+")")
		}
	}()
	fn()
}

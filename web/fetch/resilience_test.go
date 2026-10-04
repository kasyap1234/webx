package fetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kasyap1234/webx/web/render"
)

func TestPaymentGate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Payment-Required", `{"accepts":[{"scheme":"x402","price":"$0.01"}]}`)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	_, err := Fetch(context.Background(), FetchRequest{URL: srv.URL})
	if err == nil {
		t.Fatal("want 402 error")
	}
	if c := ErrorCode(err); c != CodePaymentRequired {
		t.Fatalf("code = %q, want payment_required", c)
	}
	p := PaymentOf(err)
	if p == nil || p.Protocol != "x402" || p.Price != "$0.01" {
		t.Fatalf("payment = %+v, want x402/$0.01", p)
	}
}

// Vercel serves markdown-negotiated pages as text/plain (react.dev) —
// plain text must pass through as content, not die on unsupported_type.
func TestTextPlainPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/llms.txt" || r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("# Quick Start\n\nWelcome to the docs.\n"))
	}))
	defer srv.Close()

	doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL})
	if err != nil {
		t.Fatalf("text/plain rejected: %v", err)
	}
	if doc.Extractor != "text-plain" {
		t.Fatalf("extractor = %q", doc.Extractor)
	}
	if !strings.Contains(doc.Markdown, "Welcome to the docs") {
		t.Fatalf("markdown = %q", doc.Markdown)
	}
	if doc.AgentReady != nil && doc.AgentReady.MarkdownNative {
		t.Fatal("text/plain must not set markdown_native")
	}
}

// auto_render must escalate on bot-wall statuses (403/429/503), not just
// JS-shell 200s — medium/npmjs/claude.ai class sites bounce HTTP clients
// but let a real browser through. Found by bench/corpus-js.txt.
func TestAutoRenderEscalatesOn403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	orig := renderPage
	defer func() { renderPage = orig }()
	renderPage = func(ctx context.Context, req render.Request) (*render.Result, error) {
		return &render.Result{
			HTML:     []byte(`<html><title>R</title><body><article><h1>Recovered</h1><p>real content from chrome</p></article></body></html>`),
			Engine:   "chrome",
			FinalURL: req.URL,
		}, nil
	}

	doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL, AutoRender: true})
	if err != nil {
		t.Fatalf("auto_render didn't recover 403: %v", err)
	}
	if doc.TierUsed != "chrome" {
		t.Fatalf("tier = %q, want chrome", doc.TierUsed)
	}
	if !strings.Contains(doc.Markdown, "Recovered") {
		t.Fatalf("markdown = %q", doc.Markdown)
	}
	found := false
	for _, w := range doc.Warnings {
		if strings.Contains(w, "403") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no 403-recovery warning: %v", doc.Warnings)
	}

	// Without auto_render the 403 must stay an honest error.
	_, err = Fetch(context.Background(), FetchRequest{URL: srv.URL})
	if ErrorCode(err) != CodeHTTPStatus {
		t.Fatalf("plain fetch: code = %q, want http_status", ErrorCode(err))
	}
}

// Origins that ship HTML as octet-stream (or nothing) get rescued by
// body-sniffing; a genuinely unknown body still errors honestly.
func TestGenericContentTypeSniff(t *testing.T) {
	html := "<!doctype html><html><body><article><h1>Sniffed</h1><p>content</p></article></body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/llms.txt" || r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.URL.Path {
		case "/octet":
			w.Header().Set("Content-Type", "application/octet-stream")
		case "/none":
			w.Header()["Content-Type"] = []string{""} // stop Go auto-detecting
		case "/binary":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
			return
		}
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	for _, p := range []string{"/octet", "/none"} {
		doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL + p})
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if !strings.Contains(doc.Markdown, "Sniffed") {
			t.Fatalf("%s: markdown = %q", p, doc.Markdown)
		}
		var sniffWarn bool
		for _, w := range doc.Warnings {
			if strings.Contains(w, "sniffed") {
				sniffWarn = true
			}
		}
		if !sniffWarn {
			t.Fatalf("%s: no sniff warning in %v", p, doc.Warnings)
		}
	}

	// Binary body still fails cleanly — sniffing isn't a free pass.
	_, err := Fetch(context.Background(), FetchRequest{URL: srv.URL + "/binary"})
	if ErrorCode(err) != CodeUnsupportedType {
		t.Fatalf("binary: code = %q, want unsupported_type", ErrorCode(err))
	}
}

// Render-only flags on an HTTP-tier fetch must be reported, not silently
// dropped — silent ignores are the worst wrong answer for an agent.
func TestIgnoredRenderOptsWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/llms.txt" || r.URL.Path == "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><article><h1>P</h1><p>content here</p></article></body></html>`))
	}))
	defer srv.Close()

	doc, err := Fetch(context.Background(), FetchRequest{
		URL: srv.URL, WaitFor: ".x", Actions: "click:#a", Screenshot: true})
	if err != nil {
		t.Fatal(err)
	}
	var warn string
	for _, w := range doc.Warnings {
		if strings.Contains(w, "render options ignored") {
			warn = w
		}
	}
	if warn == "" {
		t.Fatalf("no ignored-options warning in %v", doc.Warnings)
	}
	for _, opt := range []string{"wait_for", "actions", "screenshot"} {
		if !strings.Contains(warn, opt) {
			t.Fatalf("warning %q missing %q", warn, opt)
		}
	}
}

// WantA11y documents "implies render" — a browser must actually launch.
func TestA11yImpliesRender(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><p>x</p></body></html>`))
	}))
	defer srv.Close()

	orig := renderPage
	defer func() { renderPage = orig }()
	renderCalled := false
	renderPage = func(ctx context.Context, req render.Request) (*render.Result, error) {
		renderCalled = true
		return &render.Result{
			HTML:     []byte(`<html><body><article><h1>A11y</h1><p>rendered</p></article></body></html>`),
			Engine:   "chrome",
			FinalURL: req.URL,
		}, nil
	}

	doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL, WantA11y: true})
	if err != nil {
		t.Fatal(err)
	}
	if !renderCalled {
		t.Fatal("a11y didn't imply render")
	}
	if doc.TierUsed != "chrome" {
		t.Fatalf("tier = %q", doc.TierUsed)
	}
}

func TestPaymentGateBare(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()
	_, err := Fetch(context.Background(), FetchRequest{URL: srv.URL})
	if ErrorCode(err) != CodePaymentRequired {
		t.Fatalf("code = %q", ErrorCode(err))
	}
	if p := PaymentOf(err); p == nil || p.Protocol != "unknown" {
		t.Fatalf("payment = %+v, want protocol unknown", p)
	}
}

func TestRetryAfter(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`<html><head><title>ok</title></head><body><p>recovered</p></body></html>`))
	}))
	defer srv.Close()

	doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL, RetryAfter: true})
	if err != nil {
		t.Fatal(err)
	}
	// The /llms.txt probe also hits the server — ≥2 calls is the signal.
	if doc.Markdown == "" || calls.Load() < 2 {
		t.Fatalf("calls=%d doc=%+v", calls.Load(), doc)
	}
}

func TestRetryAfterDisabled(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := Fetch(context.Background(), FetchRequest{URL: srv.URL}) // no RetryAfter flag
	if ErrorCode(err) != CodeRateLimited {
		t.Fatalf("code = %q, want rate_limited", ErrorCode(err))
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d — retried without consent", calls.Load())
	}
}

func TestRetryAfterTooLong(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "9999")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	_, err := Fetch(context.Background(), FetchRequest{URL: srv.URL, RetryAfter: true})
	if ErrorCode(err) != CodeRateLimited {
		t.Fatalf("code = %q, want rate_limited (over cap)", ErrorCode(err))
	}
}

func TestErrorCodeUnwrap(t *testing.T) {
	err := fmt.Errorf("outer: %w", ferr(CodeUnsupportedType, "not html", nil))
	if c := ErrorCode(err); c != CodeUnsupportedType {
		t.Fatalf("code through wrap = %q", c)
	}
	if c := ErrorCode(errors.New("plain")); c != "" {
		t.Fatalf("plain err code = %q, want empty", c)
	}
}

func TestParseCookies(t *testing.T) {
	txt := "# Netscape HTTP Cookie File\n" +
		"#HttpOnly_.example.com\tTRUE\t/\tTRUE\t1893456000\tsession\tabc123\n" +
		".example.com\tTRUE\t/\tFALSE\t1893456000\tpref\tdark\n" +
		"expired.com\tTRUE\t/\tFALSE\t1000000000\told\tgone\n"
	cs, err := ParseCookies(txt, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 {
		t.Fatalf("parsed %d cookies, want 3", len(cs))
	}
	forLive, _ := http.NewRequest("GET", "https://sub.example.com/x", nil)
	live := cookiesFor(cs, forLive.URL)
	if len(live) != 2 {
		t.Fatalf("live cookies for sub.example.com = %d, want 2 (expired dropped)", len(live))
	}
}

func TestCookieFileOnWire(t *testing.T) {
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" { // the llms.txt probe rides the same server unsigned
			gotCookie = r.Header.Get("Cookie")
		}
		w.Write([]byte(`<html><head><title>x</title></head><body><p>hi</p></body></html>`))
	}))
	defer srv.Close()

	su, _ := url.Parse(srv.URL)
	cf := fmt.Sprintf(".%s\tTRUE\t/\tFALSE\t1893456000\ttoken\ts3cr3t\n", su.Hostname())
	_, err := Fetch(context.Background(), FetchRequest{URL: srv.URL, CookieText: cf})
	if err != nil {
		t.Fatal(err)
	}
	if gotCookie != "token=s3cr3t" {
		t.Fatalf("cookie header = %q, want token=s3cr3t", gotCookie)
	}
}

func TestDurationMs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.Write([]byte(`<html><head><title>x</title></head><body><p>hi there</p></body></html>`))
	}))
	defer srv.Close()
	doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if doc.DurationMs < 5 {
		t.Fatalf("duration_ms = %d, want ≥10ms of honest wall time", doc.DurationMs)
	}
}

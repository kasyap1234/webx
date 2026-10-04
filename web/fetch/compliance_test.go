package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// The compliance probes should pick up tdmrep.json, ai.txt, AGENTS.md,
// sitemap lines and the A2A agent card — the accountable-agent surface.
func TestAgentReadyCompliance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><title>x</title></head><body><p>content here yes</p></body></html>`))
		case "/robots.txt":
			w.Write([]byte("User-agent: *\nDisallow:\nSitemap: https://x.test/s.xml\n"))
		case "/.well-known/tdmrep.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ver":"2.0","policy":[{"location":"/","tdm-reservation":1}]}`))
		case "/ai.txt":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("User-Agent: *\nDisallow: /private\n"))
		case "/AGENTS.md":
			w.Header().Set("Content-Type", "text/markdown")
			w.Write([]byte("# agents\nuse the api\n"))
		case "/.well-known/agent.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"name":"x agent","skills":[]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL, WantAgentReady: true})
	if err != nil {
		t.Fatal(err)
	}
	ar := doc.AgentReady
	if ar == nil {
		t.Fatal("agent_ready missing")
	}
	if !ar.TDMReserved {
		t.Error("tdm_reserved not detected from /.well-known/tdmrep.json")
	}
	if !ar.AITxt {
		t.Error("ai_txt not detected")
	}
	if !ar.AgentsMD {
		t.Error("agents_md not detected")
	}
	if ar.AgentCard == "" {
		t.Error("agent_card not detected")
	}
	if len(ar.Sitemaps) != 1 || ar.Sitemaps[0] != "https://x.test/s.xml" {
		t.Errorf("sitemaps = %v", ar.Sitemaps)
	}
}

// The tdm-reservation response header alone should set tdm_reserved — no
// well-known file needed (that's TDMRep's HTTP-header mode).
func TestTDMHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("tdm-reservation", "1")
		w.Write([]byte(`<html><head><title>x</title></head><body><p>hi content</p></body></html>`))
	}))
	defer srv.Close()
	doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL, WantAgentReady: true})
	if err != nil {
		t.Fatal(err)
	}
	if doc.AgentReady == nil || !doc.AgentReady.TDMReserved {
		t.Fatalf("tdm_reserved not set from header: %+v", doc.AgentReady)
	}
}

// probeWellKnown must not false-positive on 404s or wrong content-types.
func TestWellKnownNegative(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	if got := probeWellKnown(context.Background(), u, "/.well-known/tdmrep.json", "json"); got != "" {
		t.Fatalf("probeWellKnown 404 → %q, want \"\"", got)
	}
}

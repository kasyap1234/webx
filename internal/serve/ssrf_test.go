package serve

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGuardBlocksPrivateIPs(t *testing.T) {
	t.Setenv("WEBX_BLOCK_PRIVATE", "1")
	g := NewTargetGuard()
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data",
		"http://127.0.0.1:6379/",
		"http://localhost/admin",
		"http://10.0.0.5/",
		"http://192.168.1.1/",
		"http://[::1]/",
	} {
		if err := g.CheckURL(u); err == nil {
			t.Errorf("CheckURL(%s) allowed — want refused", u)
		}
	}
}

func TestGuardSchemeAndPolicy(t *testing.T) {
	g := NewTargetGuard()
	if err := g.CheckURL("file:///etc/passwd"); err == nil {
		t.Error("file:// scheme allowed")
	}
	if err := g.CheckURL("ftp://x/"); err == nil {
		t.Error("ftp:// scheme allowed")
	}

	g2 := &TargetGuard{deny: []string{"evil.com"}}
	if err := g2.CheckURL("http://evil.com/"); err == nil {
		t.Error("deny-listed domain allowed")
	}
	if err := g2.CheckURL("http://sub.evil.com/"); err == nil {
		t.Error("deny-listed subdomain allowed")
	}

	g3 := &TargetGuard{allow: []string{"docs.example.com"}, blockPrivate: false}
	if err := g3.CheckURL("https://other.com/"); err == nil {
		t.Error("non-allow-listed domain allowed under allow policy")
	}
	if err := g3.CheckURL("https://api.docs.example.com/"); err != nil {
		t.Errorf("allow-list subdomain refused: %v", err)
	}
}

func TestGuardPublicPasses(t *testing.T) {
	g := NewTargetGuard()
	// Literal public IP — no DNS needed, deterministic.
	if err := g.CheckURL("https://93.184.216.34/"); err != nil {
		t.Errorf("public IP refused: %v", err)
	}
}

func TestScrapeGuardBlocksSSRF(t *testing.T) {
	s := New(nil)
	req := httptest.NewRequest("POST", "/scrape",
		strings.NewReader(`{"url":"http://169.254.169.254/"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("SSRF target got %d, want 403", rec.Code)
	}
}

func TestScrapeGuardOptOut(t *testing.T) {
	t.Setenv("WEBX_BLOCK_PRIVATE", "0")
	s := New(nil)
	req := httptest.NewRequest("POST", "/scrape",
		strings.NewReader(`{"url":"http://127.0.0.1:1/"}`)) // refused by fetcher, not guard
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	// With the guard off the request reaches the fetcher — any code but 403
	// proves the guard didn't veto it.
	if rec.Code == 403 {
		t.Fatal("guard vetoed with WEBX_BLOCK_PRIVATE=0")
	}
}

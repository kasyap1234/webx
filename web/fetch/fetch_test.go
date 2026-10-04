package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const pageWithBoilerplate = `<!doctype html>
<html><head><title>Test Article</title></head>
<body>
<nav><a href="/home">Home</a> <a href="/about">About</a> NAVJUNK</nav>
<article>
<h1>Real Content Heading</h1>
<p>This is the main article body. It contains enough prose for the
readability extractor to score it as the primary content of the page.
Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod
tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim
veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea
commodo consequat. Duis aute irure dolor in reprehenderit in voluptate
velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint
occaecat cupidatat non proident, sunt in culpa qui officia deserunt
mollit anim id est laborum. Sed ut perspiciatis unde omnis iste natus
error sit voluptatem accusantium doloremque laudantium.</p>
</article>
<footer>FOOTERJUNK copyright 2026</footer>
</body></html>`

func serve(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchExtractsMainContent(t *testing.T) {
	srv := serve(t, "text/html; charset=utf-8", pageWithBoilerplate)

	doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !doc.Extracted {
		t.Error("expected Extracted=true")
	}
	if doc.Title != "Test Article" {
		t.Errorf("Title = %q, want %q", doc.Title, "Test Article")
	}
	if !strings.Contains(doc.Markdown, "Real Content Heading") {
		t.Error("markdown missing article body")
	}
	if strings.Contains(doc.Markdown, "FOOTERJUNK") {
		t.Error("markdown contains footer boilerplate")
	}
	if doc.TextLength != len(doc.Markdown) {
		t.Errorf("TextLength = %d, want %d", doc.TextLength, len(doc.Markdown))
	}
}

func TestFetchRawConvertsFullPage(t *testing.T) {
	srv := serve(t, "text/html; charset=utf-8", pageWithBoilerplate)

	doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL, Raw: true})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if doc.Extracted {
		t.Error("expected Extracted=false in raw mode")
	}
	if !strings.Contains(doc.Markdown, "FOOTERJUNK") {
		t.Error("raw mode should include full page incl. footer")
	}
	if doc.Title != "Test Article" {
		t.Errorf("Title = %q, want %q", doc.Title, "Test Article")
	}
}

func TestFetchBadPDF(t *testing.T) {
	// PDFs are supported — but a corrupt one surfaces an honest error.
	srv := serve(t, "application/pdf", "%PDF-1.4 fake")

	_, err := Fetch(context.Background(), FetchRequest{URL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "pdf") {
		t.Fatalf("expected pdf parse error, got %v", err)
	}
}

func TestFetchRejectsNonHTML(t *testing.T) {
	srv := serve(t, "application/zip", "PK\x03\x04 fake")

	_, err := Fetch(context.Background(), FetchRequest{URL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "unsupported content type") {
		t.Fatalf("expected unsupported content type error, got %v", err)
	}
}

func TestFetchStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	_, err := Fetch(context.Background(), FetchRequest{URL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("expected status 404 error, got %v", err)
	}
}

func TestFetchNotModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Modified-Since") == "" {
			t.Error("expected If-Modified-Since header")
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(srv.Close)

	doc, err := Fetch(context.Background(), FetchRequest{
		URL:             srv.URL,
		IfModifiedSince: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !doc.NotModified || doc.StatusCode != 304 {
		t.Fatalf("expected NotModified/304, got %+v", doc)
	}
}

func TestMDQuality(t *testing.T) {
	prose := "This is a full sentence of actual article content.\nAnother complete sentence follows with substance."
	if q := mdQuality(prose); q < 0.9 {
		t.Fatalf("prose should score high, got %v", q)
	}
	nav := "[Home](/)\n[Products](/p)\n[About](/a)\n[Contact](/c)\n[Blog](/b)\n[Careers](/j)"
	if q := mdQuality(nav); q > 0.3 {
		t.Fatalf("nav salad should score low, got %v", q)
	}
}

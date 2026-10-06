package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kasyap1234/webx/web/fetch"
)

// captureFetch stubs the outbound fetch with a canned doc and records the
// requests that reach it — lets tests assert URL reconstruction without
// touching the network.
func captureFetch(t *testing.T, doc *fetch.Document, err error) *[]fetch.FetchRequest {
	t.Helper()
	var got []fetch.FetchRequest
	stubFetch(t, func(_ context.Context, r fetch.FetchRequest) (*fetch.Document, error) {
		got = append(got, r)
		return doc, err
	})
	return &got
}

func TestReaderMarkdown(t *testing.T) {
	reqs := captureFetch(t, &fetch.Document{
		Title:    "Example Domain",
		FinalURL: "https://example.com/",
		Markdown: "hello world",
	}, nil)

	srv := httptest.NewServer(New(nil).Mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/r/https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	for _, want := range []string{"Title: Example Domain", "URL Source: https://example.com/", "Markdown Content:", "hello world"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %q", want, body)
		}
	}
	if len(*reqs) != 1 {
		t.Fatalf("fetch called %d times", len(*reqs))
	}
	got := (*reqs)[0]
	if got.URL != "https://example.com" {
		t.Fatalf("URL reconstruction wrong: %q", got.URL)
	}
	if !got.AutoRender {
		t.Fatal("reader should default AutoRender")
	}
}

func TestReaderCollapsedScheme(t *testing.T) {
	// ServeMux cleans // → /: /r/https://x arrives at the handler as
	// https:/x. The reconstruction must restore it.
	reqs := captureFetch(t, &fetch.Document{FinalURL: "https://example.com", Markdown: "x"}, nil)
	srv := New(nil)
	srv.Mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/r/https:/example.com", nil))
	if len(*reqs) != 1 || (*reqs)[0].URL != "https://example.com" {
		t.Fatalf("collapsed scheme not repaired: %+v", *reqs)
	}
}

func TestReaderQueryPassthrough(t *testing.T) {
	reqs := captureFetch(t, &fetch.Document{FinalURL: "https://example.com/?x=1&y=2", Markdown: "x"}, nil)
	srv := httptest.NewServer(New(nil).Mux)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/r/https://example.com?x=1&y=2")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(*reqs) != 1 || (*reqs)[0].URL != "https://example.com?x=1&y=2" {
		t.Fatalf("query not preserved: %+v", *reqs)
	}
}

func TestReaderSSRFRefused(t *testing.T) {
	reqs := captureFetch(t, &fetch.Document{Markdown: "x"}, nil)
	srv := New(nil)
	// cleaned-path form — what the handler sees after ServeMux's 307.
	for _, u := range []string{"/r/http:/169.254.169.254/latest/meta-data", "/r/http:/localhost:8080/admin", "/r/http:/10.0.0.1/"} {
		rec := httptest.NewRecorder()
		srv.Mux.ServeHTTP(rec, httptest.NewRequest("GET", u, nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: got %d want 403", u, rec.Code)
		}
	}
	if len(*reqs) != 0 {
		t.Fatal("SSRF targets reached fetch")
	}
}

func TestReaderJSON(t *testing.T) {
	captureFetch(t, &fetch.Document{Title: "T", FinalURL: "https://example.com", Markdown: "body"}, nil)
	srv := New(nil)
	req := httptest.NewRequest("GET", "/r/https:/example.com", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	srv.Mux.ServeHTTP(rec, req)
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			Title    string `json:"title"`
			Markdown string `json:"markdown"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || !out.Success || out.Data.Title != "T" {
		t.Fatalf("bad json: %v %s", err, rec.Body)
	}
}

func TestReaderTokenBudget(t *testing.T) {
	captureFetch(t, &fetch.Document{FinalURL: "https://example.com", Markdown: strings.Repeat("x", 10000)}, nil)
	srv := New(nil)
	req := httptest.NewRequest("GET", "/r/https:/example.com", nil)
	req.Header.Set("X-Token-Budget", "10") // ~40 chars
	rec := httptest.NewRecorder()
	srv.Mux.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), strings.Repeat("x", 100)) {
		t.Fatal("token budget not applied")
	}
}

func TestReaderUsageOnEmpty(t *testing.T) {
	srv := New(nil)
	rec := httptest.NewRecorder()
	srv.Mux.ServeHTTP(rec, httptest.NewRequest("GET", "/r/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "usage:") {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

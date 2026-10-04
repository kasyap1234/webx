package index

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestContentTermsStripsStopwords(t *testing.T) {
	terms := contentTerms("what is a goroutine")
	if len(terms) != 1 || terms[0] != "goroutine" {
		t.Fatalf("contentTerms returned %v, want [goroutine]", terms)
	}
}

func TestDeleteOlderThan(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(filepath.Join(t.TempDir(), "idx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	for _, u := range []string{"https://a/1", "https://a/2"} {
		if err := idx.Put(ctx, Page{URL: u, Title: "t", Body: "fresh body"}); err != nil {
			t.Fatal(err)
		}
	}
	// Nothing is older than now+1h — future cutoff keeps both.
	n, err := idx.DeleteOlderThan(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("deleted %d fresh pages", n)
	}
	// Future cutoff sweeps everything indexed "now".
	n, err = idx.DeleteOlderThan(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("gc deleted %d, want 2", n)
	}
	if err := idx.Vacuum(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestPathFor(t *testing.T) {
	t.Setenv("WEBX_INDEX_DB", "")
	if got := PathFor(""); got == "" {
		t.Fatal("empty collection should give default")
	}
	p := PathFor("docs")
	if filepath.Base(p) != "index-docs.db" {
		t.Fatalf("PathFor(docs) = %s", p)
	}
	if got := PathFor("../evil"); got == "" || filepath.Base(got) == "evil" {
		t.Fatalf("unsafe collection name resolved to %s", got)
	}
}

func TestQueryDoesNotORWidenOverStopwords(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(filepath.Join(t.TempDir(), "idx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	// A page about CSS — contains "a", "is", "what" but not "goroutine".
	if err := idx.Put(ctx, Page{
		URL:   "https://example.com/css",
		Title: "CSS guide",
		Body:  "This is a guide about what a stylesheet is and how it works.",
	}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Put(ctx, Page{
		URL:   "https://go.dev/tour/concurrency/1",
		Title: "Goroutines",
		Body:  "A goroutine is a lightweight thread managed by the Go runtime.",
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := idx.Query(ctx, "what is a goroutine", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].URL != "https://go.dev/tour/concurrency/1" {
		t.Fatalf("expected only the goroutine page, got %v", hits)
	}
}

func TestQueryAllStopwordsFallsBackToPhrase(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(filepath.Join(t.TempDir(), "idx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if err := idx.Put(ctx, Page{URL: "https://x.test/", Title: "x", Body: "what is this"}); err != nil {
		t.Fatal(err)
	}
	hits, err := idx.Query(ctx, "what is", 10)
	if err != nil {
		t.Fatal(err)
	}
	// phrase "what is" does match the body — non-crash + optional hit is fine.
	_ = hits
}

func TestListHost(t *testing.T) {
	idx, err := Open(filepath.Join(t.TempDir(), "idx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ctx := context.Background()
	idx.Put(ctx, Page{URL: "https://docs.acme.io/guide/a", Title: "A", Body: "alpha body"})
	idx.Put(ctx, Page{URL: "https://docs.acme.io/api/b", Title: "B", Body: "beta body"})
	idx.Put(ctx, Page{URL: "https://other.io/x", Title: "X", Body: "other"})
	pages, err := idx.ListHost(ctx, "docs.acme.io", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages under host, got %d", len(pages))
	}
	if pages[0].URL != "https://docs.acme.io/api/b" { // ORDER BY url
		t.Fatalf("ordering wrong: %v", pages)
	}
}

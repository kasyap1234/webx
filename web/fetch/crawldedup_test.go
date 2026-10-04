package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Two URLs serving identical body → the second is deduped, not stored twice.
func TestCrawlContentDedup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/":
			fmt.Fprintf(w, `<html><body><a href="/a">a</a> <a href="/b">b</a></body></html>`)
		default: // /a and /b serve the SAME content — print-view style dupe
			fmt.Fprintf(w, `<html><body><h1>Same page</h1><p>identical body text here</p></body></html>`)
		}
	}))
	defer srv.Close()
	res, err := Crawl(context.Background(), CrawlRequest{
		Start: srv.URL, Limit: 5, Depth: 2, RatePerSec: 100,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deduped < 1 {
		t.Fatalf("expected deduped pages, got %+v", res)
	}
	// /a and /b are content-identical — only one should survive.
	same := 0
	for _, p := range res.Pages {
		if p.URL == srv.URL+"/a" || p.URL == srv.URL+"/b" {
			same++
		}
	}
	if same != 1 {
		t.Fatalf("expected exactly one of /a,/b in pages, got %d: %+v", same, res.Pages)
	}
}

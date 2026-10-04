package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/search"
)

func TestScrape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/scrape" || r.Method != "POST" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["url"] != "https://a.com" {
			t.Errorf("url = %v", in["url"])
		}
		if in["cookies"] != "# netscape" {
			t.Errorf("cookie file not inlined: %v", in["cookies"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"url": "https://a.com", "markdown": "# A", "tier_used": "http"},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	doc, err := c.Scrape(context.Background(), fetch.FetchRequest{
		URL: "https://a.com", CookieText: "# netscape",
	})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Markdown != "# A" || doc.TierUsed != "http" {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestSearchAndError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":   []map[string]any{{"title": "T", "link": "https://x"}},
				"errors": map[string]string{"ddg": "timeout"},
			})
			return
		}
		w.WriteHeader(402)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "payment", "code": "payment_required",
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	resp, err := c.Search(context.Background(), search.Request{Query: "q", Num: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 || resp.Errors["ddg"] != "timeout" {
		t.Fatalf("resp = %+v", resp)
	}

	_, err = c.Scrape(context.Background(), fetch.FetchRequest{URL: "https://pay"})
	if err == nil || err.Error() == "" {
		t.Fatal("expected 402 error")
	}
}

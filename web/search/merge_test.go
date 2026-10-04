package search

import (
	"testing"
	"time"
)

func TestNormalizeURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://www.Example.com/path/?utm_source=x&a=1#frag", "https://example.com/path?a=1"},
		{"https://example.com/", "https://example.com/"},
		{"https://example.com/p?fbclid=zzz", "https://example.com/p"},
		{"not a url", ""},
	}
	for _, c := range cases {
		if got := normalizeURL(c.in); got != c.want {
			t.Errorf("normalizeURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFuseCanonicalizesSlugs(t *testing.T) {
	lists := map[string][]Result{
		"ddg": {{Title: "SO", URL: "https://stackoverflow.com/questions/76539371/how-to-solve-golang-fatal-error"}},
		"so":  {{Title: "SO", URL: "https://stackoverflow.com/questions/76539371"}},
	}
	got := fuse(lists, Request{Num: 10})
	if len(got) != 1 {
		t.Fatalf("slug-canonicalized SO URLs should dedupe to 1, got %d", len(got))
	}
	if len(got[0].Sources) != 2 {
		t.Errorf("Sources = %v, want ddg+so merged", got[0].Sources)
	}
}

func TestFuseDedupAndAgreement(t *testing.T) {
	lists := map[string][]Result{
		"ddg": {
			{Title: "A", URL: "https://foo.dev/page?utm_source=news"},
			{Title: "B", URL: "https://random-blog.example/post"},
		},
		"hn": {
			{Title: "A2", URL: "https://foo.dev/page"},
		},
	}
	got := fuse(lists, Request{Num: 10})

	if len(got) != 2 {
		t.Fatalf("got %d results, want 2 (dedup)", len(got))
	}
	top := got[0]
	if top.URL != "https://foo.dev/page" {
		t.Errorf("top result = %q, want the result two providers agreed on", top.URL)
	}
	if len(top.Sources) != 2 {
		t.Errorf("Sources = %v, want both providers", top.Sources)
	}
}

func TestFuseDocsDomainPrior(t *testing.T) {
	lists := map[string][]Result{
		"ddg": {
			{Title: "content farm", URL: "https://medium.com/x/post"},
			{Title: "official docs", URL: "https://developer.mozilla.org/en-US/docs/x"},
		},
	}
	got := fuse(lists, Request{Num: 10})
	if got[0].URL != "https://developer.mozilla.org/en-US/docs/x" {
		t.Errorf("docs domain should outrank content farm despite lower position; got %q first", got[0].URL)
	}
}

func TestFuseDedupesSourcesWithinProvider(t *testing.T) {
	lists := map[string][]Result{
		"hn": {
			{Title: "A", URL: "https://foo.dev/p"},
			{Title: "A again", URL: "https://foo.dev/p"}, // same provider, same URL
		},
	}
	got := fuse(lists, Request{Num: 10})
	if len(got) != 1 || len(got[0].Sources) != 1 {
		t.Fatalf("expected 1 result with 1 source, got %+v", got)
	}
}

func TestFuseSiteAndAfterFilters(t *testing.T) {
	lists := map[string][]Result{
		"ddg": {
			{Title: "old", URL: "https://a.dev/old", Published: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Title: "new", URL: "https://a.dev/new", Published: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Title: "other site", URL: "https://b.dev/x", Published: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
	got := fuse(lists, Request{
		Num:   10,
		Site:  "a.dev",
		After: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if len(got) != 1 || got[0].URL != "https://a.dev/new" {
		t.Fatalf("site+after filters: got %+v", got)
	}
}

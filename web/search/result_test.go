package search

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// omitzero (not omitempty) is what drops a zero time.Time — regression test
// for the 0001-01-01T00:00:00Z leak into search JSON.
func TestResultPublishedOmitzero(t *testing.T) {
	raw, err := json.Marshal(Result{Title: "t", URL: "https://x", Score: 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "published") {
		t.Fatalf("zero Published serialized: %s", raw)
	}
	raw, _ = json.Marshal(Result{Title: "t", URL: "https://x", Published: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)})
	if !strings.Contains(string(raw), `"published":"2024-01-01`) {
		t.Fatalf("real Published dropped: %s", raw)
	}
}

// With no WEBX_EMBED_MODEL configured, rerank falls back to lexical scoring —
// a snippet covering the query terms must outrank an equal-score bare one.
func TestRerankLexicalFallback(t *testing.T) {
	t.Setenv("WEBX_EMBED_MODEL", "")
	results := []Result{
		{Title: "Unrelated", URL: "https://a", Snippet: "cooking recipes and pasta", Score: 0.6},
		{Title: "Go maps", URL: "https://b", Snippet: "golang maps tutorial generics", Score: 0.5},
	}
	rerankResults(context.Background(), results, Request{Query: "golang generics"}, nil)
	if results[0].URL != "https://b" {
		t.Fatalf("lexical rerank didn't promote the relevant result: %+v", results)
	}
	if results[0].Score <= results[1].Score {
		t.Fatalf("rerank didn't reorder by score: %+v", results)
	}
}

// --category developer must restrict the resolved provider set to dev
// sources — web providers (ddg/wiki/brave/searxng) are excluded.
func TestCategoryDeveloperRestrictsProviders(t *testing.T) {
	ps := resolve(Request{Category: "developer"})
	if len(ps) == 0 {
		t.Skip("no dev providers resolvable in this env (need index or network)")
	}
	devOnly := map[string]bool{"so": true, "gh": true, "grep": true, "sg": true,
		"npm": true, "crates": true, "hn": true, "reddit": true, "index": true}
	for _, p := range ps {
		if !devOnly[p.Name()] {
			t.Fatalf("non-dev provider %q resolved under category developer", p.Name())
		}
	}
}

// include+exclude domains together is a caller error — the response must
// say so rather than silently intersecting to empty.
func TestMutualDomainExclusionWarns(t *testing.T) {
	resp := Search(context.Background(), Request{
		Query: "test", Domains: []string{"a.com"}, ExcludeDomains: []string{"b.com"},
	})
	if resp.Errors["request"] == "" {
		t.Fatalf("expected mutual-exclusion error, got %+v", resp.Errors)
	}
}

// composeAnswer picks the best query-covered excerpt per source.
func TestComposeAnswer(t *testing.T) {
	results := []Result{
		{Title: "A", URL: "https://a", Snippet: "unrelated text about cooking"},
		{Title: "B", URL: "https://b", Highlights: []string{"golang generics let you write type-safe code once"}},
	}
	ans := composeAnswer(results, "golang generics", 1800)
	if !strings.Contains(ans, "[1]") || !strings.Contains(ans, "https://b") {
		t.Fatalf("answer missing cited source: %q", ans)
	}
	if !strings.Contains(ans, "type-safe") {
		t.Fatalf("best excerpt not chosen: %q", ans)
	}
}

// When contents[i] is populated the snippet path is bypassed — a term-rich
// snippet must NOT earn the coverage boost once the page body was scraped.
func TestRerankPrefersScrapedContent(t *testing.T) {
	t.Setenv("WEBX_EMBED_MODEL", "")
	results := []Result{
		{Title: "A", URL: "https://a", Snippet: "golang generics golang generics", Score: 0.5},
		{Title: "B", URL: "https://b", Snippet: "golang generics", Score: 0.5},
	}
	// A's page body says nothing about the query; B has no scraped body, so
	// its snippet drives the coverage boost — B must outrank A.
	contents := map[int]string{0: "a long scraped page about cooking recipes and pasta dishes only"}
	rerankResults(context.Background(), results, Request{Query: "golang generics"}, contents)
	if results[0].URL != "https://b" {
		t.Fatalf("snippet coverage applied despite scraped content: %+v", results)
	}
}

// soupBlock gates excerpts/highlights: scraped sidebar chrome (dash-run
// separators, link-dense blocks) is rejected; short plain lines and prose
// with inline links survive.
func TestSoupBlock(t *testing.T) {
	cases := []struct {
		b    string
		want bool
	}{
		{"[Hot Q](x) ----- [Other](y) ---- [More](z)", true},                              // SO sidebar separator
		{"[Home](a) [Docs](b) [API](c) [Blog](d)", true},                                  // pure link nav
		{"Revolution ended in 1799.", false},                                              // short plain text
		{"The [Treaty](https://x) ended the war and reshaped Europe for decades.", false}, // prose + links
		{"", false},
	}
	for _, c := range cases {
		if got := soupBlock(c.b); got != c.want {
			t.Errorf("soupBlock(%q) = %v, want %v", c.b[:min(40, len(c.b))], got, c.want)
		}
	}
}

package eval

import (
	"context"
	"testing"
)

func TestScoreHit(t *testing.T) {
	urls := []string{
		"https://example.com/a",
		"https://docs.python.org/3/tutorial", // subdomain of expected host
		"https://stackoverflow.com/q/1",
	}
	rank, matched, ok := scoreHit(urls, []string{"python.org"})
	if !ok || rank != 2 || matched != "python.org" {
		t.Fatalf("got rank=%d matched=%q ok=%v", rank, matched, ok)
	}
	if _, _, ok := scoreHit(urls, []string{"github.com"}); ok {
		t.Fatal("unexpected hit")
	}
	// bare host must not suffix-match a longer host
	if _, _, ok := scoreHit([]string{"https://notstackoverflow.com/x"}, []string{"stackoverflow.com"}); ok {
		t.Fatal("suffix collision: notstackoverflow.com matched stackoverflow.com")
	}
}

func TestMarkerCoverage(t *testing.T) {
	cov, missing := markerCoverage("has alpha and beta", []string{"alpha", "beta", "gamma"})
	if cov != 2.0/3.0 || len(missing) != 1 || missing[0] != "gamma" {
		t.Fatalf("cov=%v missing=%v", cov, missing)
	}
	if cov, _ := markerCoverage("anything", nil); cov != 1 {
		t.Fatal("empty markers should score 1")
	}
}

func TestURLsFrom(t *testing.T) {
	// tavily shape
	u := urlsFrom([]byte(`{"results":[{"url":"https://a.com"},{"url":"https://b.com"}]}`), "results")
	if len(u) != 2 || u[0] != "https://a.com" {
		t.Fatalf("tavily parse: %v", u)
	}
	// firecrawl nested web shape
	u = urlsFrom([]byte(`{"data":{"web":[{"url":"https://c.com"}]}}`), "data", "web")
	if len(u) != 1 || u[0] != "https://c.com" {
		t.Fatalf("firecrawl parse: %v", u)
	}
	// garbage → nothing, no panic
	if u := urlsFrom([]byte(`not json`), "results"); u != nil {
		t.Fatalf("garbage parse: %v", u)
	}
}

func TestMarkdownFrom(t *testing.T) {
	md := markdownFrom([]byte(`{"data":{"markdown":"# Title\ncontent"}}`), "markdown")
	if md != "# Title\ncontent" {
		t.Fatalf("got %q", md)
	}
	// tavily results shape — longest text wins
	md = markdownFrom([]byte(`{"results":[{"raw_content":"long body text"}]}`), "raw_content")
	if md != "long body text" {
		t.Fatalf("got %q", md)
	}
}

func TestRunCite(t *testing.T) {
	set := CiteSet{Pairs: []CitePair{
		// numeric hallucination must be caught
		{Claim: "The bridge spans twenty kilometers", Evidence: "The bridge spans fifteen kilometers", Supported: false},
		// clearly supported
		{Claim: "The bridge opened in 1937", Evidence: "The bridge opened to traffic in 1937", Supported: true},
	}}
	rep := RunCite(set)
	if rep.FP != 0 {
		t.Fatalf("false-accept on numeric hallucination: %+v", rep)
	}
	if rep.TP != 1 || rep.TN != 1 {
		t.Fatalf("want tp=1 tn=1, got %+v", rep)
	}
	if rep.Precision != 1 {
		t.Fatalf("precision %v", rep.Precision)
	}
}

func TestEnginesForSkipsMissingKeys(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	t.Setenv("TAVILY_API_KEY", "")
	eng, skipped := EnginesFor([]string{"exa", "tavily", "firecrawl", "bogus"})
	if len(eng) != 1 || eng[0].Name() != "firecrawl" { // firecrawl has a default base, no key needed
		t.Fatalf("engines: %v", eng)
	}
	if len(skipped) != 3 { // exa + tavily (no key) + bogus (unknown)
		t.Fatalf("skipped: %v", skipped)
	}
}

// Firecrawl needs no key when pointed at a self-hosted base — make sure the
// engine still works unauthenticated.
func TestFirecrawlNoKey(t *testing.T) {
	t.Setenv("FIRECRAWL_BASE_URL", "http://localhost:39999")
	t.Setenv("FIRECRAWL_API_KEY", "")
	e := firecrawlEngine()
	if e == nil {
		t.Fatal("firecrawl engine should exist without a key (self-hostable)")
	}
	// connection refused is fine — the point is the client exists and errs cleanly
	if _, err := e.Search(context.Background(), "x", 1); err == nil {
		t.Fatal("expected connection error against nothing")
	}
}

func TestSetWriteRoundtrip(t *testing.T) {
	s := Set{Queries: []Case{
		{Query: `quote "test" \ path`, Expect: []string{"example.com"}},
		{Query: "plain query here", Expect: []string{"a.com", "b.com"}},
	}}
	f := t.TempDir() + "/set.yaml"
	if err := s.Write(f); err != nil {
		t.Fatal(err)
	}
	back, err := LoadSet(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Queries) != 2 || back.Queries[0].Query != s.Queries[0].Query ||
		back.Queries[1].Expect[1] != "b.com" {
		t.Fatalf("roundtrip mismatch: %+v", back.Queries)
	}
}

func TestAlphaOnly(t *testing.T) {
	if alphaOnly("!!! in math ???") != "inmath" {
		t.Fatal(alphaOnly("!!! in math ???"))
	}
}

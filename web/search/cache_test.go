package search

import (
	"testing"
	"time"
)

// Regression: cacheKey must differ whenever any field that changes the
// result set differs — a filtered query replaying an unfiltered cache was
// a live bug (only query/num/site/providers/after were hashed).
func TestCacheKeyCoversFilters(t *testing.T) {
	base := Request{Query: "golang slices", Num: 10}
	day := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	variants := map[string]Request{
		"domains":   {Query: base.Query, Num: 10, Domains: []string{"go.dev"}},
		"exclude":   {Query: base.Query, Num: 10, ExcludeDomains: []string{"medium.com"}},
		"after":     {Query: base.Query, Num: 10, After: day},
		"before":    {Query: base.Query, Num: 10, Before: day},
		"topic":     {Query: base.Query, Num: 10, Topic: "news"},
		"lang":      {Query: base.Query, Num: 10, Lang: "de"},
		"providers": {Query: base.Query, Num: 10, Providers: []string{"ddg"}},
		"site":      {Query: base.Query, Num: 10, Site: "go.dev"},
		"semantic":  {Query: base.Query, Num: 10, Semantic: true},
		"depth":     {Query: base.Query, Num: 10, Depth: "advanced"},
		"num":       {Query: base.Query, Num: 25},
	}
	k0 := cacheKey(base)
	for name, v := range variants {
		if cacheKey(v) == k0 {
			t.Errorf("cacheKey(%s) collides with unfiltered request — stale cache risk", name)
		}
	}
}

// Exact rewrites the provider query to a quoted phrase — the cache key
// must reflect that effective query, not the raw one.
func TestCacheKeyExact(t *testing.T) {
	a := Request{Query: "exact phrase", Num: 10, Exact: true}
	b := Request{Query: "exact phrase", Num: 10, Exact: false}
	if cacheKey(a) == cacheKey(b) {
		t.Error("exact and non-exact requests must not share a cache entry")
	}
	// A query already quoted must not double-wrap into a third key.
	c := Request{Query: `"exact phrase"`, Num: 10, Exact: true}
	if cacheKey(a) != cacheKey(c) {
		t.Error("pre-quoted exact query should key identically")
	}
}

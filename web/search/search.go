package search

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kasyap1234/webx/web/index"
)

// Request configures a metasearch query.
type Request struct {
	Query          string
	Num            int       // max results, 0 -> defaultNum
	Providers      []string  // provider names; empty -> defaults
	Site           string    // restrict to this domain
	Domains        []string  // keep only results under these domains (post-fusion)
	ExcludeDomains []string  // drop results under these domains
	After          time.Time // drop results published before this (when known)
	Before         time.Time // drop results published after this (when known)
	Topic          string    // "news" boosts freshness + news-category providers
	Lang           string    // provider-side language hint (searxng, wiki)
	Exact          bool      // phrase-match the query verbatim
	Scrape         bool      // fetch each result's page inline
	ScrapeChars    int       // cap per-result content chars, 0 -> defaultScrapeChars
	HighlightsOnly bool      // populate Highlights without full Content (token-saver)
	Rerank         bool      // re-sort by scraped-content relevance (needs Scrape)
	Browser        bool      // use the Chrome-fingerprint transport for inline scrapes
	Session        string    // persistent cookie jar for provider calls + scrapes
	Render         bool      // render every result page via Chrome when scraping
	AutoRender     bool      // escalate to Chrome only on detected JS-shells
	Semantic       bool      // index provider: fuse FTS5 with embedding cosine (needs WEBX_EMBED_MODEL)
	// Depth tunes the latency/quality tradeoff — Tavily search_depth /
	// Exa type parity: "fast" trims the provider set to the lowest-latency
	// sources, "advanced" adds the slower opt-in providers and scrapes
	// top results for per-source chunks. "" / "basic" = default behavior.
	Depth string
	// Sources selects result verticals beyond the default "web" — "news"
	// and "images" fan out to category-capable providers (searxng) and
	// come back typed on each Result.
	Sources []string
	// Subpages, when >0, crawls N sitemap-discovered subpages per top
	// result (Exa contents.subpages); SubpageTarget filters candidates.
	Subpages       int
	SubpageTarget  []string
	SubpageResults int // results that get subpages, 0 -> 3
	// Collection names a non-default local-index corpus
	// (~/.webx/index-<name>.db). "" = the default index.
	Collection string
}

// Result is a single fused search hit.
type Result struct {
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	Type       string    `json:"type,omitempty"` // web (default) | news | images — from Sources
	ImageURL   string    `json:"image_url,omitempty"`
	Thumbnail  string    `json:"thumbnail,omitempty"`
	Snippet    string    `json:"snippet,omitempty"`
	Sources    []string  `json:"sources"` // providers that returned this URL
	Score      float64   `json:"score"`
	RawScore   float64   `json:"-"` // provider-native score (e.g. bm25) — blended into Score during fusion
	Published  time.Time `json:"published,omitempty"`
	Content    string    `json:"content,omitempty"`    // populated by --scrape
	Highlights []string  `json:"highlights,omitempty"` // query-relevant passages
	Subpages   []Subpage `json:"subpages,omitempty"`   // Exa contents.subpages
}

// Subpage is one crawled page under a search result's site.
type Subpage struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Excerpt string `json:"excerpt,omitempty"` // query-fit excerpt (markdown)
}

// Response is the merged search output plus per-provider errors.
type Response struct {
	Query      string            `json:"query"`
	Results    []Result          `json:"results"`
	Errors     map[string]string `json:"errors,omitempty"`
	ProviderMs map[string]int64  `json:"provider_ms,omitempty"` // per-provider latency, wall ms
	CacheHit   bool              `json:"cache_hit,omitempty"`   // results replayed from cache, not live
	Deduped    int               `json:"deduped,omitempty"`     // near-duplicate results dropped
}

// Provider is one upstream search source.
type Provider interface {
	Name() string
	Search(ctx context.Context, req Request) ([]Result, error)
}

const defaultNum = 10

func sortResults(rs []Result) {
	sort.Slice(rs, func(i, j int) bool { return rs[i].Score > rs[j].Score })
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// registry maps provider names to constructors.
var registry = map[string]func() Provider{
	"index":   func() Provider { return &localIndex{path: index.DefaultPathEnv()} },
	"ddg":     func() Provider { return &duckduckgo{} },
	"hn":      func() Provider { return &hackernews{} },
	"so":      func() Provider { return &stackoverflow{} },
	"wiki":    func() Provider { return &wikipedia{} },
	"gh":      func() Provider { return &github{token: githubToken()} },
	"reddit":  func() Provider { return &reddit{} },
	"searxng": func() Provider { return &searxng{bases: searxngBases()} },
	"brave":   func() Provider { return &brave{apiKey: braveKey()} },
	"mojeek":  func() Provider { return &mojeek{apiKey: mojeekKey()} },
	"kagi":    func() Provider { return &kagi{token: kagiToken()} },
	"grep":    func() Provider { return &grepapp{} },
	"sg":      func() Provider { return &sourcegraph{} },
	"npm":     func() Provider { return &npmjs{} },
	"crates":  func() Provider { return &cratesio{} },
}

// Available lists provider names; names = nil means defaults.
func Available() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

func braveKey() string {
	if k := os.Getenv("WEBX_BRAVE_API_KEY"); k != "" {
		return k
	}
	return os.Getenv("BRAVE_API_KEY")
}

func mojeekKey() string {
	if k := os.Getenv("WEBX_MOJEEK_API_KEY"); k != "" {
		return k
	}
	return os.Getenv("MOJEEK_API_KEY")
}

func kagiToken() string {
	if k := os.Getenv("WEBX_KAGI_API_TOKEN"); k != "" {
		return k
	}
	return os.Getenv("KAGI_API_TOKEN")
}

// resolve returns the provider set for a request. Defaults are the free,
// key-less providers plus brave when an API key is configured; req.Depth
// widens ("advanced") or narrows ("fast") the fan-out.
func resolve(req Request) []Provider {
	names := req.Providers
	if len(names) == 0 {
		// Defaults: free, reliable providers. reddit/gh stay opt-in due to
		// unauthenticated rate limits; searxng/brave join when configured.
		names = []string{"ddg", "hn", "so", "wiki"}
		if index.Exists(index.PathFor(req.Collection)) {
			names = append([]string{"index"}, names...)
		}
		if len(searxngBases()) > 0 {
			names = append(names, "searxng")
		}
		if braveKey() != "" {
			names = append(names, "brave")
		}
	}
	switch req.Depth {
	case "fast":
		// Lowest-latency fan-out: the local index plus the single fastest
		// upstream. For chatty agents that would rather re-query than wait.
		names = []string{"ddg"}
		if index.Exists(index.PathFor(req.Collection)) {
			names = append([]string{"index"}, names...)
		}
	case "advanced":
		// Everything that can plausibly answer — the opt-in providers join
		// the defaults; unconfigured ones fail fast into errors{}.
		seen := map[string]bool{}
		var merged []string
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				merged = append(merged, n)
			}
		}
		for _, n := range []string{"reddit", "gh", "sg", "grep", "npm", "crates", "mojeek", "kagi"} {
			if !seen[n] {
				merged = append(merged, n)
			}
		}
		names = merged
	}
	var ps []Provider
	for _, n := range names {
		if n == "index" && req.Collection != "" {
			ps = append(ps, &localIndex{path: index.PathFor(req.Collection)})
			continue
		}
		if make, ok := registry[n]; ok {
			ps = append(ps, make())
		}
	}
	// Typed sources (news/images) ride category providers alongside the
	// web set — currently SearXNG verticals; unconfigured = honest error.
	for _, src := range req.Sources {
		switch src {
		case "news":
			ps = append(ps, &searxng{category: "news"})
		case "images":
			ps = append(ps, &searxng{category: "images"})
		}
	}
	return ps
}

// Search fans out to every resolved provider, tolerates individual failures,
// and returns fused, ranked results.
func Search(ctx context.Context, req Request) *Response {
	if req.Num <= 0 {
		req.Num = defaultNum
	}
	resp := &Response{Query: req.Query}

	// advanced depth implies the read-the-pages pass — per-source excerpts
	// are the Tavily "advanced" chunks_per_source equivalent — plus content
	// rerank so "advanced" means better ordering, not just more providers.
	if req.Depth == "advanced" {
		req.Rerank = true
		if !req.Scrape {
			req.Scrape = true
			req.HighlightsOnly = true
		}
	}

	ck := cacheKey(req)
	cached, cacheHit := cachedResponse(ctx, ck)
	if cacheHit && !req.Scrape && req.Subpages <= 0 {
		resp.Results = cached.Results
		resp.CacheHit = true
		return resp
	}

	providers := resolve(req)
	if len(providers) == 0 {
		resp.Errors = map[string]string{"search": "no providers enabled"}
		return resp
	}

	type outcome struct {
		name    string
		results []Result
		err     error
		ms      int64
	}
	ch := make(chan outcome, len(providers))
	for _, p := range providers {
		go func(p Provider) {
			// A provider panic (transport bug, bad response shape) must not
			// kill the process — under `serve` this goroutine is a daemon.
			defer func() {
				if rc := recover(); rc != nil {
					ch <- outcome{p.Name(), nil,
						fmt.Errorf("provider panic: %v", rc), 0}
				}
			}()
			r := req
			if r.Exact && !strings.HasPrefix(r.Query, `"`) {
				r.Query = `"` + r.Query + `"` // phrase-match at the provider
			}
			if rw, ok := p.(rewriter); ok {
				r = rw.Rewrite(r)
			}
			start := time.Now()
			res, err := p.Search(ctx, r)
			ch <- outcome{p.Name(), res, err, time.Since(start).Milliseconds()}
		}(p)
	}

	lists := make(map[string][]Result, len(providers))
	resp.ProviderMs = map[string]int64{}
	for range providers {
		o := <-ch
		resp.ProviderMs[o.name] = o.ms
		if o.err != nil {
			if resp.Errors == nil {
				resp.Errors = map[string]string{}
			}
			resp.Errors[o.name] = o.err.Error()
			continue
		}
		if len(o.results) > 0 {
			lists[o.name] = o.results
		}
	}

	if cacheHit {
		resp.Results = cached.Results
		resp.CacheHit = true
	} else {
		resp.Results = fuse(lists, req)
		storeResponse(ctx, ck, &Response{Results: resp.Results})
	}
	if req.Scrape {
		if req.ScrapeChars <= 0 {
			req.ScrapeChars = 2000
		}
		var dropped int
		resp.Results, dropped = enrich(ctx, resp.Results, req)
		resp.Deduped = dropped
	}

	// Subpages crawl linked pages under each top result's site — the
	// Exa contents.subpages feature. Runs last so enrichment/rerank see
	// the parent pages first.
	if req.Subpages > 0 {
		attachSubpages(ctx, resp.Results, req)
	}
	normalizeScores(resp.Results)
	return resp
}

// normalizeScores rescales fused scores to 0–1 against the top hit so
// callers can threshold on them — raw RRF sums are meaningless magnitudes.
func normalizeScores(results []Result) {
	if len(results) == 0 || results[0].Score <= 0 {
		return
	}
	top := results[0].Score
	for i := range results {
		results[i].Score /= top
	}
}

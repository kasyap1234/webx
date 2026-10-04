package search

import (
	"context"
	"net/url"
	"strings"
	"sync"

	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/index"
)

// subpages.go — Exa contents.subpages equivalent: for each top result,
// discover the site's URLs (sitemap/robots/HTML links) and scrape the N
// most target-relevant subpages into Result.Subpages. Opt-in and bounded —
// each result costs a sitemap hit plus Subpages page fetches.

const (
	maxSubpagesPerResult = 10
	maxSubpageResults    = 5
	subpageExcerptChars  = 800
)

// attachSubpages crawls subpages for the top results in parallel. Results
// are mutated in place (Subpages field) — caller passes the fused slice.
func attachSubpages(ctx context.Context, results []Result, req Request) {
	n := req.SubpageResults
	if n <= 0 {
		n = 3
	}
	if n > maxSubpageResults {
		n = maxSubpageResults
	}
	if n > len(results) {
		n = len(results)
	}
	per := req.Subpages
	if per > maxSubpagesPerResult {
		per = maxSubpagesPerResult
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		// Typed verticals (images/news) don't have crawlable subpages.
		if results[i].Type != "" && results[i].Type != "web" {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i].Subpages = crawlSubpages(ctx, results[i].URL, per, req)
		}(i)
	}
	wg.Wait()
}

// crawlSubpages maps one result's site and fit-scrapes the best candidates.
func crawlSubpages(ctx context.Context, resultURL string, per int, req Request) []Subpage {
	u, err := url.Parse(resultURL)
	if err != nil || u.Host == "" {
		return nil
	}
	base := &url.URL{Scheme: u.Scheme, Host: u.Host}
	urls, err := index.DiscoverURLs(ctx, base, 200)
	if err != nil || len(urls) == 0 {
		return nil
	}

	// Rank candidates by subpageTarget hits in the path — Exa prioritizes
	// keyword-matched pages ("pricing", "api") over sitemap order.
	norm := normalizeURL(resultURL)
	type cand struct {
		url   string
		score int
	}
	scored := make([]cand, 0, len(urls))
	for _, c := range urls {
		if normalizeURL(c) == norm {
			continue // the result page itself is not a subpage
		}
		cu, err := url.Parse(c)
		if err != nil {
			continue
		}
		path := strings.ToLower(cu.Path)
		score := 0
		for _, t := range req.SubpageTarget {
			t = strings.ToLower(strings.TrimSpace(t))
			if t != "" && strings.Contains(path, t) {
				score++
			}
		}
		scored = append(scored, cand{c, score})
	}
	// Targeted terms first (stable for equal scores → sitemap order).
	var picks []string
	for pass := 0; pass < 2 && len(picks) < per; pass++ {
		for _, c := range scored {
			if len(picks) >= per {
				break
			}
			if pass == 0 && c.score == 0 {
				continue
			}
			if pass == 1 && c.score > 0 {
				continue
			}
			picks = append(picks, c.url)
		}
	}

	fit := strings.Join(req.SubpageTarget, " ")
	if fit == "" {
		fit = req.Query
	}
	out := make([]Subpage, 0, len(picks))
	for _, p := range picks {
		doc, err := fetch.Fetch(ctx, fetch.FetchRequest{
			URL: p, Fit: fit,
			Browser: req.Browser, Session: req.Session,
		})
		if err != nil {
			continue // subpages are best-effort — a dead link isn't a search error
		}
		excerpt := doc.FitMarkdown
		if excerpt == "" {
			excerpt = doc.Excerpt
		}
		if len(excerpt) > subpageExcerptChars {
			excerpt = excerpt[:subpageExcerptChars]
		}
		out = append(out, Subpage{URL: doc.FinalURL, Title: doc.Title, Excerpt: excerpt})
	}
	return out
}

package index

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kasyap1234/webx/web/fetch"
)

// IndexOptions controls a domain indexing run.
type IndexOptions struct {
	DBPath        string
	Limit         int           // max pages
	Concurrency   int           // parallel fetches
	RatePerSec    float64       // politeness: requests per second, 0 -> defaultRate
	Timeout       time.Duration // per-page fetch timeout
	MaxDepth      int           // BFS fallback link depth, 0 -> defaultDepth
	CCrawl        string        // Common Crawl collection id ("" -> latest)
	StaleTTL      time.Duration // skip re-indexing pages fetched within this window, 0 -> always re-fetch
	RespectRobots bool          // honor each host's robots.txt Disallow rules
	IncludePaths  []string      // regex — only index matching URL paths
	ExcludePaths  []string      // regex — never index matching URL paths
}

// Stats reports an indexing run's outcome.
type Stats struct {
	Discovered int
	Pages      int
	Failed     int
}

const (
	defaultLimit       = 500
	defaultConcurrency = 4
	defaultRate        = 3.0 // req/s — polite for a single docs host
	defaultDepth       = 2
)

// IndexDomain crawls a domain's sitemap and indexes every page into the
// local FTS5 index. logf receives human progress messages.
func IndexDomain(ctx context.Context, rawurl string, opts IndexOptions, logf func(string, ...any)) (*Stats, error) {
	if !strings.Contains(rawurl, "://") {
		rawurl = "https://" + rawurl
	}
	base, err := url.Parse(rawurl)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", rawurl, err)
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultLimit
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = defaultConcurrency
	}
	if opts.RatePerSec <= 0 {
		opts.RatePerSec = defaultRate
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultDepth
	}
	if opts.DBPath == "" {
		opts.DBPath = DefaultPathEnv()
	}

	logf("discovering sitemaps for %s…", base.Host)
	urls, sitemapErr := DiscoverURLs(ctx, base, opts.Limit)

	idx, err := Open(opts.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer idx.Close()

	rate := time.NewTicker(time.Duration(float64(time.Second) / opts.RatePerSec))
	defer rate.Stop()
	var stats Stats

	if sitemapErr == nil {
		logf("found %d urls via sitemap (limit %d), indexing…", len(urls), opts.Limit)
		stats = crawlList(ctx, idx, urls, opts, rate, logf)
	} else {
		logf("no sitemap (%v) — falling back to link crawl, depth %d", sitemapErr, opts.MaxDepth)
		stats = crawlBFS(ctx, idx, base, opts, rate, logf)
	}
	return &stats, nil
}

// pathFilter builds an include/exclude URL-path checker from regex lists —
// returns nil when no filters are configured.
func pathFilter(include, exclude []string) (func(string) bool, error) {
	var inc, exc []*regexp.Regexp
	for _, p := range include {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("include_paths %q: %w", p, err)
		}
		inc = append(inc, re)
	}
	for _, p := range exclude {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("exclude_paths %q: %w", p, err)
		}
		exc = append(exc, re)
	}
	if len(inc) == 0 && len(exc) == 0 {
		return nil, nil
	}
	return func(raw string) bool {
		u, err := url.Parse(raw)
		if err != nil {
			return false
		}
		for _, re := range exc {
			if re.MatchString(u.Path) {
				return false
			}
		}
		if len(inc) == 0 {
			return true
		}
		for _, re := range inc {
			if re.MatchString(u.Path) {
				return true
			}
		}
		return false
	}, nil
}

// crawlList fetches a fixed URL list (the sitemap path).
func crawlList(ctx context.Context, idx *Index, urls []string, opts IndexOptions, rate *time.Ticker, logf func(string, ...any)) Stats {
	var (
		wg    sync.WaitGroup
		stats Stats
		mu    sync.Mutex
		sem   = make(chan struct{}, opts.Concurrency)
	)
	stats.Discovered = len(urls)

	var robots *fetch.RobotsChecker
	if opts.RespectRobots {
		robots = fetch.NewRobotsChecker()
	}
	filter, ferr := pathFilter(opts.IncludePaths, opts.ExcludePaths)
	if ferr != nil {
		logf("path filter: %v", ferr)
		return stats
	}

	for _, u := range urls {
		if idx.Fresh(ctx, u, opts.StaleTTL) {
			continue
		}
		if filter != nil && !filter(u) {
			continue
		}
		if robots != nil {
			if pu, perr := url.Parse(u); perr == nil && !robots.Allowed(ctx, pu) {
				continue
			}
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return stats
		case <-rate.C:
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			defer func() { <-sem }()
			doc, ferr := fetch.Fetch(ctx, fetch.FetchRequest{URL: u, Timeout: opts.Timeout})
			mu.Lock()
			defer mu.Unlock()
			if ferr != nil || idx.Put(ctx, Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown}) != nil {
				stats.Failed++
				return
			}
			stats.Pages++
			if stats.Pages%25 == 0 {
				logf("indexed %d/%d pages…", stats.Pages, len(urls))
			}
		}(u)
	}
	wg.Wait()
	return stats
}

// crawlBFS handles sites with no sitemap: follows same-host links found in
// fetched pages, bounded by depth and limit. Each fetched page is indexed.
func crawlBFS(ctx context.Context, idx *Index, base *url.URL, opts IndexOptions, rate *time.Ticker, logf func(string, ...any)) Stats {
	var (
		stats  Stats
		wg     sync.WaitGroup
		mu     sync.Mutex // guards stats + seen
		seen   = map[string]bool{base.String(): true}
		queued atomic.Int64
		active atomic.Int64
	)
	var robots *fetch.RobotsChecker
	if opts.RespectRobots {
		robots = fetch.NewRobotsChecker()
	}
	filter, _ := pathFilter(opts.IncludePaths, opts.ExcludePaths)
	type job struct {
		u     string
		depth int
	}
	jobs := make(chan job, opts.Limit*4)
	enqueue := func(j job) {
		select {
		case jobs <- j:
			queued.Add(1)
		default:
		}
	}
	enqueue(job{base.String(), 0})

	for w := 0; w < opts.Concurrency; w++ {
		wg.Go(func() {
			for j := range jobs {
				// active before queued-- so the drain check never sees 0/0 mid-handoff
				active.Add(1)
				queued.Add(-1)
				select {
				case <-ctx.Done():
					active.Add(-1)
					return
				case <-rate.C:
				}

				if filter != nil && !filter(j.u) {
					active.Add(-1)
					continue
				}
				if robots != nil {
					if pu, perr := url.Parse(j.u); perr == nil && !robots.Allowed(ctx, pu) {
						active.Add(-1)
						continue
					}
				}
				doc, ferr := fetch.Fetch(ctx, fetch.FetchRequest{URL: j.u, Timeout: opts.Timeout})
				mu.Lock()
				if ferr != nil {
					stats.Failed++
				} else if perr := idx.Put(ctx, Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown}); perr != nil {
					stats.Failed++
				} else {
					stats.Pages++
					if stats.Pages%25 == 0 {
						logf("indexed %d pages (depth %d)…", stats.Pages, j.depth)
					}
					if j.depth < opts.MaxDepth && stats.Pages+int(queued.Load()) < opts.Limit {
						for _, link := range pageLinks(doc.Markdown, base.Host) {
							if !seen[link] {
								seen[link] = true
								enqueue(job{link, j.depth + 1})
							}
						}
					}
				}
				mu.Unlock()
				active.Add(-1)
			}
		})
	}

	// Close jobs when the frontier drains; safe because enqueues only happen
	// while active>0, which keeps the queued+active invariant nonzero.
	for {
		select {
		case <-ctx.Done():
		default:
		}
		if ctx.Err() != nil && active.Load() == 0 {
			close(jobs)
			break
		}
		if queued.Load() == 0 && active.Load() == 0 {
			close(jobs)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	wg.Wait()
	mu.Lock()
	stats.Discovered = stats.Pages + stats.Failed
	mu.Unlock()
	return stats
}

var mdLinkRe = regexp.MustCompile(`\]\((https?://[^)\s]+)\)`)

// pageLinks extracts same-host absolute links from produced markdown.
func pageLinks(markdown, host string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mdLinkRe.FindAllStringSubmatch(markdown, 500) {
		u, err := url.Parse(m[1])
		if err != nil || u.Host != host {
			continue
		}
		u.Fragment = ""
		u.RawQuery = ""
		if skipExt(u.Path) || seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		out = append(out, u.String())
	}
	return out
}

var skipExts = []string{".png", ".jpg", ".jpeg", ".gif", ".svg", ".css", ".js",
	".ico", ".pdf", ".zip", ".tar", ".gz", ".mp4", ".webm", ".woff", ".woff2"}

func skipExt(path string) bool {
	p := strings.ToLower(path)
	for _, e := range skipExts {
		if strings.HasSuffix(p, e) {
			return true
		}
	}
	return false
}

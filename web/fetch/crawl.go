package fetch

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CrawlRequest configures an adaptive crawl — a relevance-scored BFS that
// follows links toward a goal instead of blanketing a domain (the
// crawl4ai "information foraging" idea): pages whose content scores toward
// Query get their links queued first, and the crawl stops once it has
// gathered enough relevant pages.
type CrawlRequest struct {
	Start         string
	Query         string          // goal — pages/links are scored toward it
	Limit         int             // max pages fetched, 0 -> 30
	Depth         int             // max link depth, 0 -> 3
	Threshold     float64         // relevance to count a page as a "find", 0 -> 0.35
	Target        int             // stop after this many pages above Threshold, 0 -> 8
	RatePerSec    float64         // politeness per host, 0 -> 3
	Concurrency   int             // parallel page fetches, 0 -> WEBX_CRAWL_CONCURRENCY or 4
	SameHost      bool            // default true — stay on the start host
	Timeout       time.Duration   // per page
	Progress      func(CrawlPage) // called per fetched page — job stores hook here
	RespectRobots bool            // honor each host's robots.txt Disallow rules
	IncludePaths  []string        // regex — only crawl matching URL paths
	ExcludePaths  []string        // regex — never crawl matching URL paths
	// Embedder, when set, upgrades page relevance from lexical term-match
	// to semantic cosine against the Query's embedding — "focused crawling"
	// over meaning, not keywords (the topic-crawler the literature never
	// shipped in a mainstream tool). One embed call per fetched page.
	Embedder func(context.Context, string) ([]float32, error)
}

// CrawlPage is one fetched page plus its relevance to the goal.
type CrawlPage struct {
	URL       string   `json:"url"`
	FinalURL  string   `json:"final_url"`
	Title     string   `json:"title,omitempty"`
	Depth     int      `json:"depth"`
	Relevance float64  `json:"relevance"`
	Relevant  bool     `json:"relevant"`
	Excerpt   string   `json:"excerpt,omitempty"`
	Markdown  string   `json:"markdown,omitempty"`
	Links     []string `json:"-"`
}

// CrawlResult is the crawl's outcome.
type CrawlResult struct {
	Goal         string      `json:"goal"`
	Pages        []CrawlPage `json:"pages"`
	Fetched      int         `json:"fetched"`
	Relevant     int         `json:"relevant"`
	Deduped      int         `json:"deduped"`       // pages dropped as content-identical
	StoppedEarly bool        `json:"stopped_early"` // sufficiency reached before limits
}

// Crawl runs a relevance-guided BFS: the frontier is scored by each page's
// relevance to req.Query, high-scoring pages get crawled deeper first, and
// the crawl stops when Target relevant pages are found — "adaptive" in the
// crawl4ai sense: it spends budget where the information is.
func Crawl(ctx context.Context, req CrawlRequest, logf func(string, ...any)) (*CrawlResult, error) {
	start, err := url.Parse(req.Start)
	if err != nil {
		return nil, err
	}
	if req.Limit <= 0 {
		req.Limit = 30
	}
	if req.Depth <= 0 {
		req.Depth = 3
	}
	if req.Threshold <= 0 {
		req.Threshold = 0.35
	}
	if req.Target <= 0 {
		req.Target = 8
	}
	if req.RatePerSec <= 0 {
		req.RatePerSec = 3
	}
	if req.Timeout <= 0 {
		req.Timeout = defaultTimeout
	}
	terms := crawlTerms(req.Query)
	// Semantic mode: embed the goal once; page scores blend cosine(topic)
	// with lexical coverage. Embedding failures degrade to lexical crawl —
	// logged once, not fatal.
	var topicVec []float32
	if req.Embedder != nil && req.Query != "" {
		if v, verr := req.Embedder(ctx, req.Query); verr == nil {
			topicVec = v
			if logf != nil {
				logf("crawl: semantic mode — pages scored by embedding cosine")
			}
		} else if logf != nil {
			logf("crawl: embedder failed (%v) — falling back to lexical relevance", verr)
		}
	}
	conc := req.Concurrency
	if conc <= 0 {
		conc = 4
		if v, err := strconv.Atoi(os.Getenv("WEBX_CRAWL_CONCURRENCY")); err == nil && v > 0 {
			conc = v
		}
	}
	seen := map[string]bool{normalizeLink(start.String()): true}
	seenHash := map[uint64]bool{}
	type frontierItem struct {
		url   string
		depth int
		score float64
	}
	frontier := []frontierItem{{start.String(), 0, 0}}
	res := &CrawlResult{Goal: req.Query}

	// Per-host politeness: one ticker per host. Each tick wakes exactly one
	// waiting goroutine, so N workers share one rate limit naturally —
	// concurrency doesn't multiply the request rate against a host.
	var rateMu sync.Mutex
	hostRates := map[string]*time.Ticker{}
	defer func() {
		for _, t := range hostRates {
			t.Stop()
		}
	}()
	rateFor := func(raw string) *time.Ticker {
		host := ""
		if u, err := url.Parse(raw); err == nil {
			host = strings.ToLower(u.Host)
		}
		rateMu.Lock()
		defer rateMu.Unlock()
		if t := hostRates[host]; t != nil {
			return t
		}
		t := time.NewTicker(time.Duration(float64(time.Second) / req.RatePerSec))
		hostRates[host] = t
		return t
	}

	var robots *RobotsChecker
	if req.RespectRobots {
		robots = NewRobotsChecker()
	}
	var include, exclude []*regexp.Regexp
	for _, p := range req.IncludePaths {
		if re, err := regexp.Compile(p); err == nil {
			include = append(include, re)
		} else {
			return nil, fmt.Errorf("include_paths %q: %w", p, err)
		}
	}
	for _, p := range req.ExcludePaths {
		if re, err := regexp.Compile(p); err == nil {
			exclude = append(exclude, re)
		} else {
			return nil, fmt.Errorf("exclude_paths %q: %w", p, err)
		}
	}
	pathOK := func(lu *url.URL) bool {
		p := lu.Path
		for _, re := range exclude {
			if re.MatchString(p) {
				return false
			}
		}
		if len(include) == 0 {
			return true
		}
		for _, re := range include {
			if re.MatchString(p) {
				return true
			}
		}
		return false
	}

	// Wave-based concurrent fetch: each wave pulls up to `conc` frontier
	// items, fetches them in parallel under the per-host tickers, then
	// merges discovered links back for the next round. Wave granularity
	// keeps the best-first ordering meaningful — a fully flat worker pool
	// would outrun the scorer and crawl breadth-first by accident.
	var mu sync.Mutex // guards seen, seenHash, res fields
	for len(frontier) > 0 && res.Fetched < req.Limit && res.Relevant < req.Target {
		wave := conc
		if wave > len(frontier) {
			wave = len(frontier)
		}
		if wave > req.Limit-res.Fetched {
			wave = req.Limit - res.Fetched
		}
		if wave <= 0 {
			break
		}
		items := frontier[:wave]
		frontier = frontier[wave:]

		// robots.txt filtering happens at selection time — sequential,
		// cached per host, and keeps disallowed URLs out of workers.
		var picks []frontierItem
		for _, it := range items {
			if robots != nil {
				if lu, err := url.Parse(it.url); err == nil && !robots.Allowed(ctx, lu) {
					if logf != nil {
						logf("crawl: %s disallowed by robots.txt — skipped", it.url)
					}
					continue
				}
			}
			picks = append(picks, it)
		}
		if len(picks) == 0 {
			continue
		}

		var newLinks []frontierItem
		var wg sync.WaitGroup
		for _, it := range picks {
			it := it
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case <-rateFor(it.url).C:
				case <-ctx.Done():
					return
				}
				doc, err := Fetch(ctx, FetchRequest{URL: it.url, Timeout: req.Timeout})
				if err != nil {
					mu.Lock()
					res.Fetched++
					mu.Unlock()
					if logf != nil {
						logf("crawl: %s: %v", it.url, err)
					}
					return
				}
				// Relevance scoring + embedding run outside the lock —
				// pure computation, the network call must not serialize.
				rel := crawlRelevance(doc, terms)
				if topicVec != nil {
					text := doc.Title + "\n" + doc.Markdown
					if len(text) > 4000 {
						text = text[:4000]
					}
					if pv, perr := req.Embedder(ctx, text); perr == nil {
						emb := cosineF32(topicVec, pv)
						rel = 0.55*emb + 0.45*rel
					}
				}
				// Content-hash dedup — same body under many URLs counts once.
				h := fnv.New64a()
				_, _ = h.Write([]byte(strings.Join(strings.Fields(doc.Markdown), " ")))
				page := CrawlPage{
					URL:       doc.URL,
					FinalURL:  doc.FinalURL,
					Title:     doc.Title,
					Depth:     it.depth,
					Relevance: rel,
					Relevant:  rel >= req.Threshold,
					Excerpt:   excerptOf(doc.Markdown, 300),
					Markdown:  doc.Markdown,
					Links:     extractLinks(doc.Markdown, doc.FinalURL),
				}
				mu.Lock()
				res.Fetched++
				if seenHash[h.Sum64()] {
					res.Deduped++
					mu.Unlock()
					if logf != nil {
						logf("crawl: %s deduped — same content as an earlier page", it.url)
					}
					return
				}
				seenHash[h.Sum64()] = true
				res.Pages = append(res.Pages, page)
				if page.Relevant {
					res.Relevant++
					if logf != nil {
						logf("crawl: %s relevant (%.2f) — %d/%d found", it.url, rel, res.Relevant, req.Target)
					}
				}
				// Queue unseen same-host links — goal-scored, deduped
				// against the shared seen map under the lock.
				if it.depth < req.Depth {
					for _, l := range page.Links {
						n := normalizeLink(l)
						if n == "" || seen[n] {
							continue
						}
						lu, err := url.Parse(l)
						if err != nil {
							continue
						}
						if req.SameHost && !sameHost(lu, start) {
							continue
						}
						if !pathOK(lu) {
							continue
						}
						seen[n] = true
						newLinks = append(newLinks, frontierItem{l, it.depth + 1, linkRelevance(l, terms)})
					}
				}
				if req.Progress != nil {
					req.Progress(page) // serialized — store writes stay ordered
				}
				mu.Unlock()
			}()
		}
		wg.Wait()
		// Merge the wave's discoveries and re-rank the frontier —
		// best-first within depth keeps politeness bounded.
		frontier = append(frontier, newLinks...)
		sort.SliceStable(frontier, func(i, j int) bool {
			if frontier[i].depth != frontier[j].depth {
				return frontier[i].depth < frontier[j].depth
			}
			return frontier[i].score > frontier[j].score
		})
	}
	res.StoppedEarly = res.Relevant >= req.Target && len(frontier) > 0
	// Completion order is nondeterministic under concurrency — present
	// pages depth-first, most-relevant first, for a stable output shape.
	sort.SliceStable(res.Pages, func(i, j int) bool {
		if res.Pages[i].Depth != res.Pages[j].Depth {
			return res.Pages[i].Depth < res.Pages[j].Depth
		}
		return res.Pages[i].Relevance > res.Pages[j].Relevance
	})
	return res, nil
}

// crawlTerms normalizes the goal into matchable tokens.
func crawlTerms(q string) []string {
	var out []string
	for _, w := range strings.Fields(strings.ToLower(q)) {
		w = strings.Trim(w, "\"'`.,:;!?()[]{}")
		if len(w) >= 3 {
			out = append(out, w)
		}
	}
	return out
}

// crawlRelevance scores a page against the goal terms — coverage-weighted
// term frequency, the same simple signal as search highlights.
func crawlRelevance(doc *Document, terms []string) float64 {
	if len(terms) == 0 {
		return 0
	}
	low := strings.ToLower(doc.Title + "\n" + doc.Markdown)
	covered := 0
	for _, t := range terms {
		if strings.Contains(low, t) {
			covered++
		}
	}
	return float64(covered) / float64(len(terms))
}

// cosineF32 — same math as index.cosine but local (fetch can't import index).
func cosineF32(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// linkRelevance guesses a link's value from its URL text — "docs/api/foo"
// scores on each term it contains.
func linkRelevance(raw string, terms []string) float64 {
	low := strings.ToLower(raw)
	s := 0.0
	for _, t := range terms {
		if strings.Contains(low, t) {
			s += 1.0
		}
	}
	return s
}

func sameHost(u *url.URL, base *url.URL) bool {
	h := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	b := strings.TrimPrefix(strings.ToLower(base.Host), "www.")
	return h == b
}

func normalizeLink(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	u.Fragment = ""
	u.RawFragment = ""
	u.Host = strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	if u.Path != "/" {
		u.Path = strings.TrimSuffix(u.Path, "/")
	}
	return u.String()
}

// extractLinks pulls absolute http(s) links out of fetched markdown — the
// converter already absolutizes them via WithDomain.
var mdLinkRe = regexp.MustCompile(`https?://[^\s)<>"'\]]+`)

func extractLinks(markdown, finalURL string) []string {
	raw := mdLinkRe.FindAllString(markdown, -1)
	out := raw[:0]
	for _, u := range raw {
		out = append(out, strings.TrimRight(u, ".,;:!?\\"))
	}
	return out
}

// excerptOf returns the first n chars around the first goal-ish block.
func excerptOf(md string, n int) string {
	md = strings.TrimSpace(md)
	if len(md) <= n {
		return md
	}
	return md[:n] + "…"
}

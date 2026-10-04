// Package eval measures webx's search quality over a fixed dev-query set —
// the honest answer to "is search any good?" that nobody else ships in a CLI.
package eval

import (
	"context"
	_ "embed"
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/search"
	"gopkg.in/yaml.v3"
)

//go:embed evalset.yaml
var defaultSet []byte

//go:embed extractset.yaml
var defaultExtractSet []byte

// Case is one query with acceptable-answer hosts.
type Case struct {
	Query  string   `yaml:"q"`
	Expect []string `yaml:"expect"`
}

// Set is the eval corpus.
type Set struct {
	Queries []Case `yaml:"queries"`
}

// CaseResult records the outcome of one query.
type CaseResult struct {
	Query     string `json:"query"`
	BestRank  int    `json:"best_rank"` // 1-based rank of first expected host, 0 = miss
	Hit       bool   `json:"hit"`       // expected host within num
	MatchedOn string `json:"matched_on,omitempty"`
}

// Report aggregates metrics.
type Report struct {
	Num          int                 `json:"num"`
	Queries      int                 `json:"queries"`
	HitRate      float64             `json:"hit_rate"` // fraction with hit in top-N
	HitLo        float64             `json:"hit_lo"`   // Wilson 95% CI bounds
	HitHi        float64             `json:"hit_hi"`
	MRR          float64             `json:"mrr"` // mean reciprocal rank
	ProviderOK   []string            `json:"providers_ok"`
	ProviderErrs map[string]int      `json:"provider_errs,omitempty"` // per-provider failure count — quota burn is data
	ByKind       map[string]KindStat `json:"by_kind"`                 // per QueryKind breakdown
	ByProvider   map[string]int      `json:"by_provider"`             // providers whose result got the hit
	Results      []CaseResult        `json:"results"`
}

// KindStat tracks hit-rate/MRR within one query shape.
type KindStat struct {
	Queries int     `json:"queries"`
	HitRate float64 `json:"hit_rate"`
	MRR     float64 `json:"mrr"`
}

// LoadSet parses a YAML eval set; path empty → the bundled default.
func LoadSet(path string) (Set, error) {
	data := defaultSet
	if path != "" {
		var err error
		if data, err = readFile(path); err != nil {
			return Set{}, err
		}
	}
	var s Set
	return s, yaml.Unmarshal(data, &s)
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// ─── extraction-quality eval ────────────────────────────────────────────────
// The second axis of search quality (per Exa's WebCode): did we retrieve the
// right URL is measured above; did we extract the right CONTENT is measured
// here against hand-picked marker strings + code-block counts.

// ExtractCase is one page with content the extraction must preserve.
type ExtractCase struct {
	URL           string   `yaml:"url"`
	Markers       []string `yaml:"markers"`
	MinCodeBlocks int      `yaml:"min_code_blocks"`
}

// ExtractSet is the extraction eval corpus.
type ExtractSet struct {
	Pages []ExtractCase `yaml:"pages"`
}

// ExtractResult records one page's extraction score.
type ExtractResult struct {
	URL        string   `json:"url"`
	Coverage   float64  `json:"coverage"` // fraction of markers found
	CodeBlocks int      `json:"code_blocks"`
	Extractor  string   `json:"extractor"`
	TextLength int      `json:"text_length"`
	Missing    []string `json:"missing,omitempty"`
	Err        string   `json:"err,omitempty"`
}

// ExtractReport aggregates extraction quality.
type ExtractReport struct {
	Pages        int             `json:"pages"`
	MeanCoverage float64         `json:"mean_coverage"`
	CodeOK       float64         `json:"code_recall"` // fraction meeting min_code_blocks
	Results      []ExtractResult `json:"results"`
}

// LoadExtractSet parses the extraction eval set; path empty → bundled.
func LoadExtractSet(path string) (ExtractSet, error) {
	data := defaultExtractSet
	if path != "" {
		var err error
		if data, err = readFile(path); err != nil {
			return ExtractSet{}, err
		}
	}
	var s ExtractSet
	return s, yaml.Unmarshal(data, &s)
}

// RunExtract fetches each page and scores whether the right content survived.
func RunExtract(ctx context.Context, s ExtractSet, logf func(string, ...any)) *ExtractReport {
	rep := &ExtractReport{Pages: len(s.Pages)}
	for i, p := range s.Pages {
		if logf != nil {
			logf("[%d/%d] %s", i+1, len(s.Pages), p.URL)
		}
		er := ExtractResult{URL: p.URL}
		doc, err := fetch.Fetch(ctx, fetch.FetchRequest{URL: p.URL})
		if err != nil {
			er.Err = err.Error()
			rep.Results = append(rep.Results, er)
			continue
		}
		er.Extractor = doc.Extractor
		er.TextLength = doc.TextLength
		er.Coverage, er.Missing = markerCoverage(doc.Markdown, p.Markers)
		er.CodeBlocks = strings.Count(doc.Markdown, "```") / 2
		rep.MeanCoverage += er.Coverage
		if er.CodeBlocks >= p.MinCodeBlocks {
			rep.CodeOK++
		}
		rep.Results = append(rep.Results, er)
	}
	if rep.Pages > 0 {
		rep.MeanCoverage /= float64(rep.Pages)
		rep.CodeOK /= float64(rep.Pages)
	}
	return rep
}

// Text renders the extraction report.
func (r *ExtractReport) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "webx eval --extract — %d pages\n", r.Pages)
	fmt.Fprintf(&b, "mean marker coverage: %.0f%%   code recall: %.0f%%\n\n",
		r.MeanCoverage*100, r.CodeOK*100)
	for _, c := range r.Results {
		if c.Err != "" {
			fmt.Fprintf(&b, "  ✗   %-55s %s\n", truncate(c.URL, 55), c.Err)
			continue
		}
		mark := "✓"
		if c.Coverage < 0.8 || len(c.Missing) > 0 {
			mark = "~"
		}
		fmt.Fprintf(&b, "  %s   %-55s %.0f%% cov  %d blocks  [%s]\n",
			mark, truncate(c.URL, 55), c.Coverage*100, c.CodeBlocks, c.Extractor)
		if len(c.Missing) > 0 {
			fmt.Fprintf(&b, "      missing: %s\n", strings.Join(c.Missing, ", "))
		}
	}
	return b.String()
}

// Run executes the eval: searches each query and scores host hits.
// Queries fan out across `workers` — 1000 sequential searches is a 30-minute
// wait, not an eval. Results land by index, so aggregation stays deterministic.
func Run(ctx context.Context, s Set, num int, providers []string, workers int, logf func(string, ...any)) *Report {
	if num <= 0 {
		num = 10
	}
	if workers < 1 {
		workers = 1
	}
	rep := &Report{Num: num, ByKind: map[string]KindStat{}, ByProvider: map[string]int{}}

	type outcome struct {
		cr         CaseResult
		hitSources []string
		providers  []string
		errs       []string // providers that errored on this query
	}
	outcomes := make([]outcome, len(s.Queries))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var done int
	var mu sync.Mutex
	for i, c := range s.Queries {
		wg.Add(1)
		go func(i int, c Case) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			resp := search.Search(ctx, search.Request{
				Query:     c.Query,
				Num:       num,
				Providers: providers,
			})
			cr := CaseResult{Query: c.Query}
			var hitSources []string
			urls := make([]string, len(resp.Results))
			for j, r := range resp.Results {
				urls[j] = r.URL
			}
			if rank, matched, ok := scoreHit(urls, c.Expect); ok {
				cr.BestRank, cr.Hit, cr.MatchedOn = rank, true, matched
				hitSources = resp.Results[rank-1].Sources
			}
			var provs []string
			for _, r := range resp.Results {
				provs = append(provs, r.Sources...)
			}
			var errs []string
			for p := range resp.Errors {
				errs = append(errs, p)
			}
			outcomes[i] = outcome{cr, hitSources, provs, errs}
			if logf != nil {
				mu.Lock()
				done++
				if done%25 == 0 || done == len(s.Queries) {
					logf("[%d/%d]", done, len(s.Queries))
				}
				mu.Unlock()
			}
		}(i, c)
	}
	wg.Wait()

	providersOK := map[string]bool{}
	kindAgg := map[string]*KindStat{}
	for i, o := range outcomes {
		rep.Results = append(rep.Results, o.cr)
		kindName := search.Classify(s.Queries[i].Query).String()
		ks, ok := kindAgg[kindName]
		if !ok {
			ks = &KindStat{}
			kindAgg[kindName] = ks
		}
		ks.Queries++
		if o.cr.Hit {
			rep.HitRate++
			rep.MRR += 1.0 / float64(o.cr.BestRank)
			ks.HitRate++
			ks.MRR += 1.0 / float64(o.cr.BestRank)
			for _, src := range o.hitSources {
				rep.ByProvider[src]++
			}
		}
		for _, src := range o.providers {
			providersOK[src] = true
		}
		for _, p := range o.errs {
			if rep.ProviderErrs == nil {
				rep.ProviderErrs = map[string]int{}
			}
			rep.ProviderErrs[p]++
		}
	}
	n := float64(len(s.Queries))
	if n > 0 {
		rep.HitRate /= n
		rep.MRR /= n
		rep.HitLo, rep.HitHi = wilson(rep.HitRate, n)
	}
	rep.Queries = len(s.Queries)
	for p := range providersOK {
		rep.ProviderOK = append(rep.ProviderOK, p)
	}
	sort.Strings(rep.ProviderOK)
	for name, ks := range kindAgg {
		if ks.Queries > 0 {
			ks.HitRate /= float64(ks.Queries)
			ks.MRR /= float64(ks.Queries)
		}
		rep.ByKind[name] = *ks
	}
	return rep
}

// wilson gives the 95% Wilson score interval for a proportion — the honest
// answer to "is 30 queries enough" (it isn't; the CI says so itself).
func wilson(p, n float64) (lo, hi float64) {
	if n == 0 {
		return 0, 0
	}
	const z = 1.96
	d := 1 + z*z/n
	c := p + z*z/(2*n)
	m := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n))
	return (c - m) / d, (c + m) / d
}

// scoreHit finds the first ranked URL whose host satisfies the expect list.
// Returns 1-based rank — identical grading for webx and competitor engines.
func scoreHit(urls []string, expect []string) (rank int, matched string, ok bool) {
	for i, u := range urls {
		h := host(u)
		for _, e := range expect {
			if h == e || strings.HasSuffix(h, "."+e) {
				return i + 1, e, true
			}
		}
	}
	return 0, "", false
}

func host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// Text renders a human-readable report.
func (r *Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "webx eval — %d queries, top-%d\n", r.Queries, r.Num)
	if r.HitLo > 0 || r.HitHi > 0 {
		fmt.Fprintf(&b, "hit rate: %.0f%%  [95%% CI %.0f–%.0f%%]   MRR: %.3f   providers: %s\n",
			r.HitRate*100, r.HitLo*100, r.HitHi*100, r.MRR, strings.Join(r.ProviderOK, ", "))
	} else {
		fmt.Fprintf(&b, "hit rate: %.0f%%   MRR: %.3f   providers contributing: %s\n",
			r.HitRate*100, r.MRR, strings.Join(r.ProviderOK, ", "))
	}
	if len(r.ByKind) > 0 {
		kinds := make([]string, 0, len(r.ByKind))
		for k := range r.ByKind {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		fmt.Fprintf(&b, "\nby query kind:\n")
		for _, k := range kinds {
			s := r.ByKind[k]
			fmt.Fprintf(&b, "  %-12s %2d queries  hit %.0f%%  mrr %.3f\n", k, s.Queries, s.HitRate*100, s.MRR)
		}
	}
	if len(r.ProviderErrs) > 0 {
		fmt.Fprintf(&b, "\nprovider failures:")
		names := make([]string, 0, len(r.ProviderErrs))
		for p := range r.ProviderErrs {
			names = append(names, p)
		}
		sort.Strings(names)
		for _, p := range names {
			fmt.Fprintf(&b, "  %s×%d", p, r.ProviderErrs[p])
		}
		b.WriteString("\n")
	}
	if len(r.ByProvider) > 0 {
		fmt.Fprintf(&b, "\nproviders that found the hit:")
		names := make([]string, 0, len(r.ByProvider))
		for p := range r.ByProvider {
			names = append(names, p)
		}
		sort.Strings(names)
		for _, p := range names {
			fmt.Fprintf(&b, "  %s×%d", p, r.ByProvider[p])
		}
	}
	b.WriteString("\n\n")
	for _, c := range r.Results {
		mark := "✗"
		if c.Hit {
			mark = fmt.Sprintf("%d", c.BestRank)
		}
		fmt.Fprintf(&b, "  %-3s %-55s %s\n", mark, truncate(c.Query, 55), c.MatchedOn)
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

package search

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/index"
)

// enrich fetches the top results' pages inline (the "--scrape" path), then
// extracts query-relevant highlight passages and optionally reranks by
// content relevance rather than SERP position.
// enrichCap bounds inline page reads — agents never consume 20 scraped
// pages per search, and every extra fetch is tail latency. Tavily's
// chunks_per_source guidance is similar: top results carry the answer.
const enrichCap = 8

// enrichPerPage bounds a single page read so one hung site can't stall
// the whole response.
const enrichPerPage = 25 * time.Second

func enrich(ctx context.Context, results []Result, req Request) ([]Result, int) {
	k := min(len(results), enrichCap)
	mds := make([]string, k)
	var wg sync.WaitGroup
	for i := 0; i < k; i++ {
		wg.Add(1)
		go func(r *Result, slot int) {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, enrichPerPage)
			defer cancel()
			doc, err := fetch.Fetch(pctx, fetch.FetchRequest{
				URL:        r.URL,
				Browser:    req.Browser,
				Session:    req.Session,
				Render:     req.Render,
				AutoRender: req.AutoRender,
			})
			if err != nil {
				return
			}
			md := doc.Markdown
			if req.ScrapeChars > 0 && len(md) > req.ScrapeChars {
				md = md[:req.ScrapeChars]
			}
			if !req.HighlightsOnly {
				r.Content = md
			}
			mds[slot] = doc.Markdown
			r.Highlights = extractHighlights(doc.Markdown, req.Query, 3)
		}(&results[i], i)
	}
	wg.Wait()

	// Near-dup pass on scraped bodies — syndicated copies differ by URL
	// but not content. Titles catch the never-fetched tail too.
	contents := map[int]string{}
	for i, md := range mds {
		if md != "" {
			contents[i] = md
		}
	}
	kept, rekeyed, dropped := dedupResults(results, contents)

	if req.Rerank {
		rerankResults(ctx, kept, req, rekeyed)
	}
	return kept, dropped
}

// rerankResults re-scores results by how well the scraped content matches
// the query. With an embedder configured it's semantic cosine (query ×
// content, one batch call); otherwise it falls back to lexical coverage.
func rerankResults(ctx context.Context, results []Result, req Request, contents map[int]string) {
	if eb := index.EmbedBatchFromEnv(); eb != nil {
		texts := make([]string, 0, len(results)+1)
		idxs := make([]int, 0, len(results))
		texts = append(texts, req.Query)
		for i, r := range results {
			md := contents[i]
			if md == "" {
				md = r.Title + " " + r.Snippet
			}
			if len(md) > 2000 {
				md = md[:2000]
			}
			texts = append(texts, md)
			idxs = append(idxs, i)
		}
		if vecs, err := eb(ctx, texts); err == nil && len(vecs) == len(texts) {
			qv := vecs[0]
			for j, i := range idxs {
				if c := index.Cosine(qv, vecs[j+1]); c > 0 {
					results[i].Score *= 1.0 + c
				}
			}
			sortResults(results)
			return
		}
	}
	for i, r := range results {
		if md := contents[i]; md != "" {
			// scraped page: bounded block-score nudge, content doesn't dominate
			r.Score *= 1.0 + contentScore(md, req.Query)
		} else {
			// snippet-level rerank (no scrape): a 1-2 line snippet can't
			// produce meaningful block scores — term coverage is the signal.
			// Full coverage earns up to +40%, enough to reorder near-ties.
			// Zero coverage on a ≥4-term query means the provider matched
			// loosely (a stale local index, a noise provider) — demote it
			// bounded, mirroring the boost, so junk can't ride a fused lead.
			cov := termCoverage(r.Title+" "+r.Snippet, req.Query)
			switch {
			case cov > 0:
				r.Score *= 1.0 + 0.4*cov
			case len(queryTerms(req.Query)) >= 4:
				r.Score *= 0.5
			}
		}
		results[i] = r
	}
	sortResults(results)
}

// termCoverage is the fraction of query terms present in text — the right
// relevance signal for short snippets where block scoring can't apply.
func termCoverage(text, query string) float64 {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return 0
	}
	low := strings.ToLower(text)
	covered := 0
	for _, t := range terms {
		if strings.Contains(low, t) {
			covered++
		}
	}
	return float64(covered) / float64(len(terms))
}

// extractHighlights returns the top N markdown blocks most relevant to the
// query — the poor-man's Exa contents.highlights. Scoring: query-term
// coverage + frequency, with a bonus for heading lines.
func extractHighlights(markdown, query string, n int) []string {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return nil
	}
	blocks := splitBlocks(markdown)
	type scored struct {
		text  string
		score float64
	}
	var cands []scored
	for _, b := range blocks {
		if soupBlock(b) {
			continue // scraped sidebar/chrome — links and separators, not prose
		}
		s := blockScore(b, terms)
		if s > 0 {
			if len(b) > 600 {
				b = b[:600] + "…"
			}
			cands = append(cands, scored{b, s})
		}
	}
	for i := 0; i < len(cands); i++ { // tiny top-n selection
		for j := i + 1; j < len(cands); j++ {
			if cands[j].score > cands[i].score {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}
	out := make([]string, 0, n)
	for i := 0; i < n && i < len(cands); i++ {
		out = append(out, cands[i].text)
	}
	return out
}

// contentScore is the overall query-vs-page relevance used by --rerank.
func contentScore(markdown, query string) float64 {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return 0
	}
	total := 0.0
	for _, b := range splitBlocks(markdown) {
		total += blockScore(b, terms)
	}
	return total / 100.0 // bounded contribution: content nudges, doesn't dominate
}

func splitBlocks(md string) []string {
	var blocks []string
	for _, para := range strings.Split(md, "\n\n") {
		p := strings.TrimSpace(para)
		if len(p) >= 40 { // skip trivial blocks
			blocks = append(blocks, p)
		}
	}
	return blocks
}

func queryTerms(q string) []string {
	f := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '_'
	})
	var out []string
	for _, t := range f {
		if len(t) >= 2 && !index.IsStopword(t) {
			out = append(out, t)
		}
	}
	return out
}

var mdLinkRunRe = regexp.MustCompile(`\[[^\]]*\]\([^)]*\)`)

// soupBlock reports whether a candidate excerpt is navigation/sidebar
// chrome rather than prose: mostly markdown-link syntax, a long dash-run
// separator (scraped SO/forum sidebars emit "-----"), or so little plain
// text once links are removed that only the links carried content.
func soupBlock(b string) bool {
	if strings.Contains(b, "-----") {
		return true
	}
	if !mdLinkRunRe.MatchString(b) {
		return false // short plain-text lines are legitimate excerpts
	}
	plain := strings.TrimSpace(mdLinkRunRe.ReplaceAllString(b, ""))
	return len(plain) < 30 // link soup leaves ~nothing once the syntax is gone
}

// blockScore: term frequency + coverage — a compact BM25-ish signal where
// headings and dense term coverage score higher.
func blockScore(block string, terms []string) float64 {
	low := strings.ToLower(block)
	isHeading := strings.HasPrefix(strings.TrimSpace(block), "#")
	covered := 0
	tf := 0.0
	for _, t := range terms {
		if strings.Contains(low, t) {
			covered++
			tf += float64(strings.Count(low, t))
		}
	}
	if covered == 0 {
		return 0
	}
	score := float64(covered)/float64(len(terms)) + 0.15*tf
	if isHeading {
		score *= 1.4
	}
	return score
}

package fetch

import (
	"math"
	"strings"
	"unicode"
)

// FitMarkdown returns only the BM25-relevant portion of a page's markdown —
// the "fit markdown" idea from crawl4ai: agents rarely need the whole page,
// they need the chunks that answer their question. Blocks are scored with
// real BM25 (not just term coverage), kept in document order above a
// dynamic threshold, and the lead block is always retained for context.
func FitMarkdown(md, query string) string {
	terms := fitTerms(query)
	if len(terms) == 0 || strings.TrimSpace(md) == "" {
		return md
	}
	blocks := splitFitBlocks(md)
	if len(blocks) <= 3 {
		return md // too small to bother filtering
	}
	scores := bm25Blocks(blocks, terms)

	// Threshold: keep blocks scoring ≥ max(mean, 0.3*max). Dynamic beats a
	// fixed cutoff because BM25 magnitude varies wildly with corpus size.
	var max, sum float64
	for _, s := range scores {
		if s > max {
			max = s
		}
		sum += s
	}
	if max == 0 {
		return md // nothing matched — the honest answer is the whole page
	}
	thresh := math.Max(sum/float64(len(blocks)), 0.3*max)

	var kept []string
	for i, b := range blocks {
		if i == 0 || scores[i] >= thresh {
			kept = append(kept, b)
		}
	}
	return strings.Join(kept, "\n\n")
}

// splitFitBlocks breaks markdown into paragraph-ish blocks, keeping headings
// attached to their content and code fences atomic (never split inside ```).
func splitFitBlocks(md string) []string {
	var blocks []string
	var cur strings.Builder
	inFence := false
	para := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			blocks = append(blocks, s)
		}
		cur.Reset()
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if strings.TrimSpace(line) == "" && !inFence {
			para()
			continue
		}
		cur.WriteString(line + "\n")
	}
	para()
	return blocks
}

// bm25Blocks scores each block as a document against the query — k1=1.2,
// b=0.75, the standard constants.
func bm25Blocks(blocks []string, terms []string) []float64 {
	const k1, b = 1.2, 0.75
	// document frequencies for idf
	df := map[string]int{}
	tokBlocks := make([][]string, len(blocks))
	for i, blk := range blocks {
		toks := fitTerms(blk)
		tokBlocks[i] = toks
		seen := map[string]bool{}
		for _, t := range toks {
			if !seen[t] {
				df[t]++
				seen[t] = true
			}
		}
	}
	n := float64(len(blocks))
	avgLen := 0.0
	for _, toks := range tokBlocks {
		avgLen += float64(len(toks))
	}
	if n > 0 {
		avgLen /= n
	}
	if avgLen == 0 {
		avgLen = 1
	}

	scores := make([]float64, len(blocks))
	for i, toks := range tokBlocks {
		tf := map[string]float64{}
		for _, t := range toks {
			tf[t]++
		}
		var s float64
		for _, term := range terms {
			f := tf[term]
			if f == 0 {
				continue
			}
			idf := math.Log(1 + (n-float64(df[term])+0.5)/(float64(df[term])+0.5))
			s += idf * (f * (k1 + 1)) / (f + k1*(1-b+b*float64(len(toks))/avgLen))
		}
		// Heading bonus: a scored heading is more likely the section the
		// agent wants than a passing mention in body text.
		if s > 0 && strings.HasPrefix(blocks[i], "#") {
			s *= 1.3
		}
		scores[i] = s
	}
	return scores
}

// fitTerms tokenizes for matching — lowercase alnum + identifier chars,
// so "useEffect" and "hx-swap" survive as terms.
func fitTerms(s string) []string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	})
	out := f[:0]
	for _, t := range f {
		if len(t) >= 2 {
			out = append(out, t)
		}
	}
	return out
}

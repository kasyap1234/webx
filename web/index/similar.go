package index

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/kasyap1234/webx/web/fetch"
)

// Similar finds pages like rawURL in the local corpus — Exa's findSimilar,
// approximated with FTS: fetch+index the page if unknown, distill its
// distinctive terms, re-query the corpus, drop self. Quality scales with
// corpus depth, which is exactly why seed/index exist.
// SignatureTerms exposes the distinctive-term distill similarQuery produces —
// shared with `similar --web` which sends the signature to live providers.
func SignatureTerms(p *Page) string { return similarQuery(p) }

func Similar(ctx context.Context, idx *Index, rawURL string, limit int) ([]Hit, error) {
	if limit <= 0 {
		limit = 10
	}
	page, err := idx.Get(ctx, rawURL)
	if err != nil {
		// Not indexed — fetch, index, then compare. Side effect is honest:
		// the corpus grows by the page the caller asked about.
		doc, ferr := fetch.Fetch(ctx, fetch.FetchRequest{URL: rawURL})
		if ferr != nil {
			return nil, ferr
		}
		page = &Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown}
		_ = idx.Put(ctx, *page)
	}

	query := similarQuery(page)
	if query == "" {
		return nil, nil
	}
	hits, err := idx.Query(ctx, query, limit+8)
	if err != nil {
		return nil, err
	}
	self := normalizeSelf(page.URL)
	out := hits[:0]
	for _, h := range hits {
		if normalizeSelf(h.URL) != self {
			out = append(out, h)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func normalizeSelf(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "www.")
	return strings.TrimSuffix(u, "/")
}

// similarQuery picks the page's most distinctive terms — title words plus
// rare-ish body tokens (length ≥ 6 filters the stop-ish mass) — into an
// OR'd FTS query that finds pages about the same topic.
func similarQuery(p *Page) string {
	freq := map[string]int{}
	var order []string
	add := func(s string) {
		for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		}) {
			if len(w) < 4 {
				continue
			}
			if freq[w] == 0 {
				order = append(order, w)
			}
			freq[w]++
		}
	}
	add(p.Title)
	add(p.Body)

	// Score: title terms (appearing early in `order` gets a bump — cheap
	// proxy since add(title) ran first) then body-frequency rarity.
	type termScore struct {
		t string
		s float64
	}
	var scored []termScore
	for i, t := range order {
		sc := float64(freq[t])
		if i < len(order)/4 { // early = title-ish
			sc *= 3
		}
		if len(t) >= 6 {
			sc *= 1.5
		}
		scored = append(scored, termScore{t, sc})
	}
	sort.Slice(scored, func(a, b int) bool { return scored[a].s > scored[b].s })
	var terms []string
	for _, ts := range scored {
		terms = append(terms, ts.t)
		if len(terms) >= 12 {
			break
		}
	}
	// Plain space-joined terms — Query() applies its own AND-then-OR-widen
	// logic; hand-rolled FTS syntax would be re-tokenized into noise.
	return strings.Join(terms, " ")
}

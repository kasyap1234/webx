package eval

// Side-by-side comparison: the same eval corpora scored identically through
// webx and each configured competitor engine. Metrics are deliberately the
// same on every row — different grading would make the table meaningless.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kasyap1234/webx/web/search"
)

// EngineStat is one column of the comparison table.
type EngineStat struct {
	Name    string       `json:"name"`
	Queries int          `json:"queries"`
	HitRate float64      `json:"hit_rate"`
	MRR     float64      `json:"mrr"`
	P50ms   int64        `json:"p50_ms"`
	P95ms   int64        `json:"p95_ms"`
	Fails   int          `json:"fails"` // request-level errors
	Ranks   []CaseResult `json:"results"`
}

// CompareReport is the search-quality league table.
type CompareReport struct {
	Num     int          `json:"num"`
	Queries int          `json:"queries"`
	Skipped []string     `json:"skipped,omitempty"`
	Engines []EngineStat `json:"engines"`
}

// RunCompare scores webx plus each competitor engine on the same query set.
func RunCompare(ctx context.Context, s Set, num int, providers []string, engines []Engine, skipped []string, logf func(string, ...any)) *CompareReport {
	if num <= 0 {
		num = 10
	}
	rep := &CompareReport{Num: num, Queries: len(s.Queries), Skipped: skipped}

	// webx row — identical grading to the engines below.
	wx := EngineStat{Name: "webx", Queries: len(s.Queries)}
	var lat []time.Duration
	for i, c := range s.Queries {
		if logf != nil {
			logf("[webx %d/%d] %s", i+1, len(s.Queries), c.Query)
		}
		t0 := time.Now()
		resp := search.Search(ctx, search.Request{Query: c.Query, Num: num, Providers: providers})
		lat = append(lat, time.Since(t0))
		cr := CaseResult{Query: c.Query}
		urls := make([]string, len(resp.Results))
		for j, r := range resp.Results {
			urls[j] = r.URL
		}
		if rank, matched, ok := scoreHit(urls, c.Expect); ok {
			cr.BestRank, cr.Hit, cr.MatchedOn = rank, true, matched
			wx.HitRate++
			wx.MRR += 1.0 / float64(rank)
		}
		wx.Ranks = append(wx.Ranks, cr)
	}
	finalizeStat(&wx, lat)
	rep.Engines = append(rep.Engines, wx)

	// competitor rows.
	for _, e := range engines {
		st := EngineStat{Name: e.Name(), Queries: len(s.Queries)}
		lat = lat[:0]
		for i, c := range s.Queries {
			if logf != nil {
				logf("[%s %d/%d] %s", e.Name(), i+1, len(s.Queries), c.Query)
			}
			t0 := time.Now()
			urls, err := e.Search(ctx, c.Query, num)
			lat = append(lat, time.Since(t0))
			cr := CaseResult{Query: c.Query}
			if err != nil {
				st.Fails++
			} else if rank, matched, ok := scoreHit(urls, c.Expect); ok {
				cr.BestRank, cr.Hit, cr.MatchedOn = rank, true, matched
				st.HitRate++
				st.MRR += 1.0 / float64(rank)
			}
			st.Ranks = append(st.Ranks, cr)
		}
		finalizeStat(&st, lat)
		rep.Engines = append(rep.Engines, st)
	}
	return rep
}

func finalizeStat(st *EngineStat, lat []time.Duration) {
	if st.Queries > 0 {
		st.HitRate /= float64(st.Queries)
		st.MRR /= float64(st.Queries)
	}
	if len(lat) == 0 {
		return
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	st.P50ms = lat[len(lat)/2].Milliseconds()
	st.P95ms = lat[(len(lat)*95)/100].Milliseconds()
	if st.P95ms == 0 && lat[len(lat)-1] > 0 {
		st.P95ms = lat[len(lat)-1].Milliseconds()
	}
}

// Text renders the league table.
func (r *CompareReport) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "webx eval --vs — %d queries, top-%d per engine\n\n", r.Queries, r.Num)
	fmt.Fprintf(&b, "  %-11s %7s %7s %8s %8s %6s\n", "engine", "hit", "mrr", "p50", "p95", "fails")
	fmt.Fprintf(&b, "  %-11s %7s %7s %8s %8s %6s\n",
		"───────────", "───", "───", "──", "──", "─────")
	for _, e := range r.Engines {
		fmt.Fprintf(&b, "  %-11s %6.0f%% %7.3f %6dms %6dms %6d\n",
			e.Name, e.HitRate*100, e.MRR, e.P50ms, e.P95ms, e.Fails)
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(&b, "\nskipped (no key): %s\n", strings.Join(r.Skipped, ", "))
	}
	// per-query diff: where webx won/lost vs the best other engine.
	best := -1
	for i, e := range r.Engines {
		if e.Name != "webx" && (best < 0 || e.MRR > r.Engines[best].MRR) {
			best = i
		}
	}
	if best >= 0 {
		fmt.Fprintf(&b, "\nqueries where %s beat webx:\n", r.Engines[best].Name)
		any := false
		for i, cr := range r.Engines[best].Ranks {
			wx := r.Engines[0].Ranks[i]
			if cr.Hit && (!wx.Hit || cr.BestRank < wx.BestRank) {
				fmt.Fprintf(&b, "  %-50s them:%d us:%s\n",
					truncate(cr.Query, 50), cr.BestRank, rankOr(wx))
				any = true
			}
		}
		if !any {
			fmt.Fprintf(&b, "  none\n")
		}
	}
	return b.String()
}

func rankOr(c CaseResult) string {
	if !c.Hit {
		return "miss"
	}
	return fmt.Sprintf("%d", c.BestRank)
}

// ─── extraction comparison ──────────────────────────────────────────────────

// EngineExtractStat is one column of the extraction table.
type EngineExtractStat struct {
	Name         string          `json:"name"`
	Pages        int             `json:"pages"`
	MeanCoverage float64         `json:"mean_coverage"`
	CodeRecall   float64         `json:"code_recall"`
	Fails        int             `json:"fails"`
	Results      []ExtractResult `json:"results"`
}

// ExtractCompareReport compares extraction quality across engines.
type ExtractCompareReport struct {
	Pages   int                 `json:"pages"`
	Skipped []string            `json:"skipped,omitempty"`
	Engines []EngineExtractStat `json:"engines"`
}

// RunExtractCompare scores marker coverage for webx plus each engine that
// implements Extract — the honest answer to "is our extraction as good".
func RunExtractCompare(ctx context.Context, s ExtractSet, engines []Engine, skipped []string, logf func(string, ...any)) *ExtractCompareReport {
	rep := &ExtractCompareReport{Pages: len(s.Pages), Skipped: skipped}

	webxRep := RunExtract(ctx, s, func(f string, a ...any) {
		if logf != nil {
			logf("[webx] "+f, a...)
		}
	})
	rep.Engines = append(rep.Engines, EngineExtractStat{
		Name: "webx", Pages: len(s.Pages),
		MeanCoverage: webxRep.MeanCoverage, CodeRecall: webxRep.CodeOK,
		Results: webxRep.Results,
	})

	for _, e := range engines {
		st := EngineExtractStat{Name: e.Name(), Pages: len(s.Pages)}
		for i, p := range s.Pages {
			if logf != nil {
				logf("[%s %d/%d] %s", e.Name(), i+1, len(s.Pages), p.URL)
			}
			er := ExtractResult{URL: p.URL}
			md, err := e.Extract(ctx, p.URL)
			if err != nil {
				er.Err = err.Error()
				st.Fails++
			} else {
				er.Extractor = e.Name()
				er.TextLength = len(md)
				er.Coverage, er.Missing = markerCoverage(md, p.Markers)
				er.CodeBlocks = strings.Count(md, "```") / 2
				st.MeanCoverage += er.Coverage
				if er.CodeBlocks >= p.MinCodeBlocks {
					st.CodeRecall++
				}
			}
			st.Results = append(st.Results, er)
		}
		if len(s.Pages) > 0 {
			st.MeanCoverage /= float64(len(s.Pages))
			st.CodeRecall /= float64(len(s.Pages))
		}
		rep.Engines = append(rep.Engines, st)
	}
	return rep
}

// markerCoverage is shared by webx's own extract eval and competitor scoring.
func markerCoverage(md string, markers []string) (float64, []string) {
	if len(markers) == 0 {
		return 1, nil
	}
	var missing []string
	found := 0
	for _, m := range markers {
		if strings.Contains(md, m) {
			found++
		} else {
			missing = append(missing, m)
		}
	}
	return float64(found) / float64(len(markers)), missing
}

// Text renders the extraction league table.
func (r *ExtractCompareReport) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "webx eval --extract --vs — %d pages\n\n", r.Pages)
	fmt.Fprintf(&b, "  %-11s %9s %11s %6s\n", "engine", "coverage", "code recall", "fails")
	for _, e := range r.Engines {
		fmt.Fprintf(&b, "  %-11s %8.0f%% %10.0f%% %6d\n",
			e.Name, e.MeanCoverage*100, e.CodeRecall*100, e.Fails)
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(&b, "\nskipped (no key): %s\n", strings.Join(r.Skipped, ", "))
	}
	return b.String()
}

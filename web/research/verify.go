// verify.go — grounding/fact-check: claim → verdict + cited sources.
// Jina's g.jina.ai equivalent — nobody else in the space ships this, and it's
// the natural pairing with research: search evidence, scrape it, judge.
package research

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/search"
)

// Verdict is the verify result — honest three-way answer, never a forced
// boolean when evidence is thin.
type Verdict struct {
	Claim      string   `json:"claim"`
	Verdict    string   `json:"verdict"` // supported | refuted | unclear | unavailable
	Confidence float64  `json:"confidence"`
	Reasoning  string   `json:"reasoning"`
	Sources    []Source `json:"sources"`
}

// Verify checks a factual claim against live web sources: search → fit-scrape
// the top hits → LLM judgment. No LLM → verdict "unavailable" with sources
// still returned (the evidence-gathering half works regardless).
func Verify(ctx context.Context, claim string, maxSources int, logf func(string, ...any)) (*Verdict, error) {
	if maxSources <= 0 {
		maxSources = 4
	}
	v := &Verdict{Claim: claim, Verdict: "unavailable"}

	resp := search.Search(ctx, search.Request{Query: claim, Num: maxSources * 2})
	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("verify: no sources found for %q", claim)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 4)
	for i := range resp.Results {
		if len(v.Sources) >= maxSources {
			break
		}
		r := resp.Results[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			doc, err := fetch.Fetch(ctx, fetch.FetchRequest{URL: r.URL, Fit: claim})
			if err != nil {
				if logf != nil {
					logf("source %s: %v", r.URL, err)
				}
				return
			}
			content := doc.FitMarkdown
			if content == "" {
				content = doc.Markdown
			}
			if len(content) > 2500 {
				content = content[:2500] + "…"
			}
			title := doc.Title
			if title == "" {
				title = r.Title
			}
			mu.Lock()
			v.Sources = append(v.Sources, Source{URL: doc.FinalURL, Title: title, Excerpt: content})
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(v.Sources) == 0 {
		return nil, fmt.Errorf("verify: all candidate sources failed to fetch")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "CLAIM: %s\n\nSOURCES:\n", claim)
	for i, s := range v.Sources {
		fmt.Fprintf(&b, "---\n[%d] %s\n%s\n%s\n", i+1, s.Title, s.URL, s.Excerpt)
	}
	out, err := fetch.LLMChat(ctx,
		`You are a fact-checking engine. Judge the CLAIM against the numbered SOURCES. Reply ONLY as JSON: {"verdict":"supported"|"refuted"|"unclear","confidence":0.0-1.0,"reasoning":"one sentence citing [n]"}. Use "unclear" when sources disagree or lack direct evidence.`,
		b.String(), true)
	if err != nil {
		// Sources still delivered — honest partial. Name the reason so the
		// caller sees "unavailable" means "no judge", not "no evidence".
		v.Reasoning = fmt.Sprintf("no LLM reachable — set WEBX_LLM_BASE/WEBX_LLM_MODEL for a verdict (%v)", err)
		return v, nil
	}
	out = strings.TrimSpace(out)
	out = strings.TrimPrefix(out, "```json")
	out = strings.TrimPrefix(out, "```")
	out = strings.TrimSuffix(out, "```")
	var parsed struct {
		Verdict    string  `json:"verdict"`
		Confidence float64 `json:"confidence"`
		Reasoning  string  `json:"reasoning"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &parsed) == nil && parsed.Verdict != "" {
		v.Verdict = parsed.Verdict
		v.Confidence = parsed.Confidence
		v.Reasoning = parsed.Reasoning
		return v, nil
	}
	// LLM answered but returned unparseable output — a wrong chat template
	// or a too-small WEBX_LLM_MAX_TOKENS. Say so, with a peek at what came
	// back, instead of an unexplained "unavailable".
	if preview := strings.TrimSpace(out); preview != "" {
		if len(preview) > 120 {
			preview = preview[:120] + "…"
		}
		v.Reasoning = fmt.Sprintf("LLM returned no usable verdict (%s)", preview)
	} else {
		v.Reasoning = "LLM returned an empty response — check model and WEBX_LLM_MAX_TOKENS"
	}
	return v, nil
}

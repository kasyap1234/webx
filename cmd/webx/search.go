package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/search"
	"github.com/spf13/cobra"
	"sort"
	"strings"
	"time"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search the web via webx's own metasearch merger",
	Args:  cobra.ExactArgs(1),
	RunE:  runSearch,
}

var (
	sNum         int
	sProviders   string
	sSite        string
	sAfter       string
	sBefore      string
	sDomains     string
	sExclDomains string
	sTopic       string
	sLang        string
	sExact       bool
	sHighlights  bool
	sFormat      string
	sScrape      bool
	sRerank      bool
	sScrapeChars int
	sSemantic    bool
	sFresh       bool
	sDepth       string
	sSources     []string
	sSubpages    int
	sSubpageTgt  []string
	sLocation    string
	sCategory    string
	sAnswer      bool
)

// iColl names the index corpus — shared by index/query/similar/llms/
// watch/diff so a per-tenant or per-topic corpus is one flag away.
var iColl string

func runSearch(cmd *cobra.Command, args []string) error {
	scrape := sScrape || sHighlights
	answer := sAnswer
	if answer && !scrape {
		// Answers need evidence — snippet-only grounding is too thin.
		// Scrape the top results at highlights depth, not full content.
		scrape, sHighlights = true, true
	}
	req := search.Request{
		Query:          args[0],
		Num:            sNum,
		Site:           sSite,
		Topic:          sTopic,
		Lang:           sLang,
		Location:       sLocation,
		Category:       sCategory,
		Exact:          sExact,
		Scrape:         scrape,
		Answer:         answer,
		Rerank:         sRerank,
		ScrapeChars:    sScrapeChars,
		HighlightsOnly: sHighlights,
		Browser:        browser,
		Session:        session,
		Render:         doRender,
		AutoRender:     autoRender,
		Semantic:       sSemantic,
		Fresh:          sFresh,
		Depth:          sDepth,
		Sources:        sSources,
		Subpages:       sSubpages,
		SubpageTarget:  sSubpageTgt,
		Collection:     iColl,
	}
	if sProviders != "" {
		req.Providers = strings.Split(sProviders, ",")
	}
	if sDomains != "" {
		req.Domains = strings.Split(sDomains, ",")
	}
	if sExclDomains != "" {
		req.ExcludeDomains = strings.Split(sExclDomains, ",")
	}
	if sAfter != "" {
		t, err := time.Parse("2006-01-02", sAfter)
		if err != nil {
			return fmt.Errorf("--after must be YYYY-MM-DD: %w", err)
		}
		req.After = t
	}
	if sBefore != "" {
		t, err := time.Parse("2006-01-02", sBefore)
		if err != nil {
			return fmt.Errorf("--before must be YYYY-MM-DD: %w", err)
		}
		req.Before = t
	}

	var resp *search.Response
	if c := apiClient(); c != nil {
		var err error
		resp, err = c.Search(cmd.Context(), req)
		if err != nil {
			return err
		}
	} else {
		resp = search.Search(cmd.Context(), req)
	}
	switch sFormat {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	case "md", "markdown":
		for name, e := range resp.Errors {
			fmt.Fprintf(cmd.ErrOrStderr(), "webx: provider %s failed: %s\n", name, e)
		}
		if resp.CacheHit {
			fmt.Fprintln(cmd.ErrOrStderr(), "webx: (cached)")
		}
		if len(resp.ProviderMs) > 0 {
			parts := make([]string, 0, len(resp.ProviderMs))
			for n, ms := range resp.ProviderMs {
				parts = append(parts, fmt.Sprintf("%s:%dms", n, ms))
			}
			sort.Strings(parts)
			fmt.Fprintf(cmd.ErrOrStderr(), "webx: providers: %s\n", strings.Join(parts, " "))
		}
		if resp.Answer != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "## Answer\n\n%s\n\n", resp.Answer)
		}
		if len(resp.Results) == 0 {
			fmt.Fprintln(cmd.ErrOrStderr(), "webx: no results")
			return nil
		}
		for i, r := range resp.Results {
			fmt.Fprintf(cmd.OutOrStdout(), "%d. %s\n   %s\n", i+1, r.Title, r.URL)
			if r.Snippet != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "   %s\n", r.Snippet)
			}
			for _, h := range r.Highlights {
				fmt.Fprintf(cmd.OutOrStdout(), "   > %s\n", h)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "   via %s\n\n", strings.Join(r.Sources, "+"))
		}
		return nil
	default:
		return fmt.Errorf("unknown --format %q (want md|json)", sFormat)
	}
}

var askCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Search then read top results — cited excerpts in one shot",
	Args:  cobra.ExactArgs(1),
	RunE:  runAsk,
}

var (
	aNum    int
	aTokens int
	aLLM    bool
)

func runAsk(cmd *cobra.Command, args []string) error {
	resp := search.Search(cmd.Context(), search.Request{
		Query:       args[0],
		Num:         aNum,
		Scrape:      true,
		ScrapeChars: 8000,
	})

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", args[0])
	budget := aTokens * 4 // ~4 chars/token
	if budget <= 0 {
		budget = 12000
	}
	type askSource struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Excerpt string `json:"excerpt,omitempty"`
	}
	var srcs []askSource
	n := 0
	for i, r := range resp.Results {
		if len(r.Highlights) == 0 && r.Content == "" && r.Snippet == "" {
			continue
		}
		var sec strings.Builder
		fmt.Fprintf(&sec, "## [%d] %s\n%s\n\n", i+1, r.Title, r.URL)
		excerpt := ""
		if len(r.Highlights) > 0 {
			for _, h := range r.Highlights {
				fmt.Fprintf(&sec, "> %s\n\n", h)
			}
			excerpt = strings.Join(r.Highlights, " … ")
		} else if r.Content != "" {
			c := r.Content
			if len(c) > 1500 {
				c = c[:1500] + "…"
			}
			sec.WriteString(c + "\n\n")
			excerpt = c
		} else {
			fmt.Fprintf(&sec, "> %s\n\n", r.Snippet)
			excerpt = r.Snippet
		}
		if b.Len()+sec.Len() > budget {
			break
		}
		b.WriteString(sec.String())
		srcs = append(srcs, askSource{Title: r.Title, URL: r.URL, Excerpt: excerpt})
		n++
	}
	for name, e := range resp.Errors {
		fmt.Fprintf(cmd.ErrOrStderr(), "webx: provider %s failed: %s\n", name, e)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "webx: %d sources cited\n", n)
	answer := ""
	if aLLM {
		a, err := synthesizeAnswer(cmd.Context(), args[0], b.String())
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "webx: LLM synthesis failed (%v) — excerpts only\n", err)
		} else {
			answer = a
		}
	}
	if format == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"question": args[0],
			"answer":   answer,
			"sources":  srcs,
		})
	}
	if _, err := fmt.Fprint(cmd.OutOrStdout(), b.String()); err != nil {
		return err
	}
	if answer != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "\n## Answer\n\n%s\n", answer)
	}
	return nil
}

// synthesizeAnswer asks the configured LLM to answer the question from the
// citation-numbered excerpts — Tavily include_answer / Olostep Answers
// parity. Returns an empty error path when no LLM backend is configured.
func synthesizeAnswer(ctx context.Context, question, sources string) (string, error) {
	sys := "You answer questions using only the numbered web sources provided. " +
		"Cite claims with [n] markers matching the source numbers. Be precise; " +
		"say when sources disagree or are silent on part of the question."
	user := fmt.Sprintf("Question: %s\n\nSources:\n%s", question, sources)
	return fetch.LLMChat(ctx, sys, user, false)
}

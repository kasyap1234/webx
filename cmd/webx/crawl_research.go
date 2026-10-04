package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/index"
	"github.com/kasyap1234/webx/web/research"
	"github.com/spf13/cobra"
)

var crawlCmd = &cobra.Command{
	Use:   "crawl <url> <goal>",
	Short: "Adaptive crawl — follow links toward the goal, stop when found",
	Args:  cobra.ExactArgs(2),
	RunE:  runCrawl,
}

var researchCmd = &cobra.Command{
	Use:   "research <question>",
	Short: "Deep research — expand → search → read sources → cited report",
	Args:  cobra.ExactArgs(1),
	RunE:  runResearch,
}

var (
	rSources int
	rSchema  string
)

func runResearch(cmd *cobra.Command, args []string) error {
	opts := research.Options{MaxSources: rSources}
	if rSchema != "" {
		if err := json.Unmarshal([]byte(rSchema), &opts.Schema); err != nil {
			return fmt.Errorf("--schema must be a JSON object: %w", err)
		}
	}
	rep, err := research.Run(cmd.Context(), args[0], opts,
		func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), "webx: "+f+"\n", a...) })
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), rep.Report)
	if rep.Fallback {
		fmt.Fprintln(cmd.ErrOrStderr(), "webx: no LLM — printed source excerpts instead of a synthesized report")
	}
	return nil
}

var verifyCmd = &cobra.Command{
	Use:   "verify <claim>",
	Short: "Fact-check a claim against live web sources (grounding — Jina g.jina.ai equivalent)",
	Args:  cobra.ExactArgs(1),
	RunE:  runVerify,
}

var vSources int

func runVerify(cmd *cobra.Command, args []string) error {
	v, err := research.Verify(cmd.Context(), args[0], vSources,
		func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), "webx: "+f+"\n", a...) })
	if err != nil {
		return err
	}
	if format == "json" {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(v)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "verdict:    %s", v.Verdict)
	if v.Confidence > 0 {
		fmt.Fprintf(out, " (%.0f%%)", v.Confidence*100)
	}
	fmt.Fprintln(out)
	if v.Reasoning != "" {
		fmt.Fprintf(out, "reasoning:  %s\n", v.Reasoning)
	}
	fmt.Fprintln(out, "sources:")
	for i, s := range v.Sources {
		fmt.Fprintf(out, "  [%d] %s\n      %s\n", i+1, s.Title, s.URL)
	}
	return nil
}

var (
	cLimit     int
	cDepth     int
	cTarget    int
	cThreshold float64
	cFormat    string
	cRobots    bool
	cIncPaths  []string
	cExcPaths  []string
	cSemantic  bool
)

func runCrawl(cmd *cobra.Command, args []string) error {
	if c := apiClient(); c != nil {
		return remoteCrawl(cmd, c, args)
	}
	var embedder func(context.Context, string) ([]float32, error)
	if cSemantic {
		embedder = index.EmbedderFromEnv()
		if embedder == nil {
			return fmt.Errorf("--semantic needs WEBX_EMBED_MODEL (+optional WEBX_EMBED_BASE/KEY)")
		}
	}
	res, err := fetch.Crawl(cmd.Context(), fetch.CrawlRequest{
		Start:         args[0],
		Query:         args[1],
		Limit:         cLimit,
		Depth:         cDepth,
		Target:        cTarget,
		Threshold:     cThreshold,
		SameHost:      true,
		RespectRobots: cRobots,
		IncludePaths:  cIncPaths,
		ExcludePaths:  cExcPaths,
		Embedder:      embedder,
	}, func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), "webx: "+f+"\n", a...) })
	if err != nil {
		return err
	}
	switch cFormat {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	default:
		for _, p := range res.Pages {
			mark := " "
			if p.Relevant {
				mark = "★"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %.2f  %s\n      %s\n", mark, p.Relevance, p.FinalURL, p.Title)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "webx: fetched %d pages, %d relevant%s\n",
			res.Fetched, res.Relevant, map[bool]string{true: " (stopped early — goal met)"}[res.StoppedEarly])
		return nil
	}
}

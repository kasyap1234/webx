package main

import (
	"context"
	"fmt"
	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/index"
	"github.com/kasyap1234/webx/web/search"
	"github.com/spf13/cobra"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

var indexCmd = &cobra.Command{
	Use:   "index <domain>",
	Short: "Crawl a site's sitemap into the local FTS5 index",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runIndex,
}

var queryCmd = &cobra.Command{
	Use:   "query <q>",
	Short: "Query only the local index",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sProviders = "index"
		return runSearch(cmd, args)
	},
}

var (
	iLimit    int
	iRPS      float64
	iConc     int
	iDB       string
	iFromCC   string
	iStale    time.Duration
	iRobots   bool
	iIncPaths []string
	iExcPaths []string
	iGC       bool
	iOlder    time.Duration
	iVacuum   bool
)

// dbPath resolves the index file — explicit --db wins, then --collection,
// then the default corpus.
func dbPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return index.PathFor(iColl)
}

func runIndex(cmd *cobra.Command, args []string) error {
	dbPath := dbPath(iDB)
	if iGC {
		// Retention sweep: drop pages older than --older-than plus their
		// vectors. Default horizon 90d — explicit because deletion is real.
		older := iOlder
		if older <= 0 {
			older = 90 * 24 * time.Hour
		}
		idx, err := index.Open(dbPath)
		if err != nil {
			return err
		}
		defer idx.Close()
		n, err := idx.DeleteOlderThan(cmd.Context(), time.Now().Add(-older))
		if err != nil {
			return err
		}
		if iVacuum {
			if err := idx.Vacuum(cmd.Context()); err != nil {
				return err
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "gc: removed %d pages older than %s from %s\n",
			n, older, dbPath)
		return nil
	}
	if len(args) != 1 {
		return fmt.Errorf("index needs a domain (or --gc)")
	}
	logf := func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), "webx: "+f+"\n", a...) }
	opts := index.IndexOptions{
		DBPath:        dbPath,
		Limit:         iLimit,
		Concurrency:   iConc,
		RatePerSec:    iRPS,
		StaleTTL:      iStale,
		RespectRobots: iRobots,
		IncludePaths:  iIncPaths,
		ExcludePaths:  iExcPaths,
	}
	var stats *index.Stats
	var err error
	if iFromCC != "" {
		opts.CCrawl = iFromCC
		if opts.CCrawl == "latest" {
			opts.CCrawl = ""
		}
		stats, err = index.IndexFromCC(cmd.Context(), args[0], opts, logf)
	} else {
		stats, err = index.IndexDomain(cmd.Context(), args[0], opts, logf)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "indexed %d/%d pages (%d failed) → %s\n",
		stats.Pages, stats.Discovered, stats.Failed, dbPath)
	return nil
}

var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Index the curated top docs domains (slow; polite crawl)",
	Args:  cobra.NoArgs,
	RunE:  runSeed,
}

// seedDomains are the highest-value docs domains for coding-agent queries —
// mostly the domainBoostTable, plus the big llms.txt publishers.
var seedDomains = []string{
	"go.dev", "pkg.go.dev", "docs.python.org", "developer.mozilla.org",
	"doc.rust-lang.org", "docs.rs", "react.dev", "nodejs.org",
	"docs.github.com", "kubernetes.io", "docs.docker.com", "docs.npmjs.com",
	"pypi.org", "crates.io", "htmx.org", "sqlite.org", "postgresql.org",
	"redis.io", "learn.microsoft.com", "docs.aws.amazon.com",
	"developer.mozilla.org", "web.dev", "developer.chrome.com",
	"git-scm.com", "docs.searxng.org", "commoncrawl.org",
}

var similarCmd = &cobra.Command{
	Use:   "similar <url>",
	Short: "Find pages like this one in the local index",
	Args:  cobra.ExactArgs(1),
	RunE:  runSimilar,
}

var llmsCmd = &cobra.Command{
	Use:   "llms <domain>",
	Short: "Emit a whole-site llms.txt from the local index",
	Args:  cobra.ExactArgs(1),
	RunE:  runLLMs,
}

var (
	simLimit int
	simWeb   bool
)

func runSimilar(cmd *cobra.Command, args []string) error {
	idx, err := index.Open(index.PathFor(iColl))
	if err != nil {
		return err
	}
	defer idx.Close()

	if simWeb {
		// Web-level findSimilar (Exa parity): distill the page's signature,
		// send it to live providers, drop self.
		page, err := idx.Get(cmd.Context(), args[0])
		if err != nil {
			doc, ferr := fetch.Fetch(cmd.Context(), fetch.FetchRequest{URL: args[0]})
			if ferr != nil {
				return ferr
			}
			page = &index.Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown}
			_ = idx.Put(cmd.Context(), *page)
		}
		terms := index.SignatureTerms(page)
		if terms == "" {
			return fmt.Errorf("couldn't distill a signature from %s", args[0])
		}
		resp := search.Search(cmd.Context(), search.Request{Query: terms, Num: simLimit + 4})
		self := strings.TrimPrefix(strings.TrimPrefix(page.URL, "https://"), "http://")
		self = strings.TrimPrefix(strings.TrimSuffix(self, "/"), "www.")
		n := 0
		for _, r := range resp.Results {
			u := strings.TrimPrefix(strings.TrimPrefix(r.URL, "https://"), "http://")
			u = strings.TrimPrefix(strings.TrimSuffix(u, "/"), "www.")
			if u == self {
				continue
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%8.1f  %s  %s\n", r.Score, r.Title, r.URL)
			if n++; n >= simLimit {
				break
			}
		}
		return nil
	}

	hits, err := index.Similar(cmd.Context(), idx, args[0], simLimit)
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "webx: no similar pages — the index may be too thin (webx seed/index)")
		return nil
	}
	for _, h := range hits {
		fmt.Fprintf(cmd.OutOrStdout(), "%8.1f  %s  %s\n", h.Score, h.Title, h.URL)
	}
	return nil
}

var llmsLimit int

// runLLMs emits a whole-site llms.txt from the local index — the same
// agent-discovery file Mintlify hosts as a paid feature, generated free.
// Pages group by first path segment; each entry is [title](url): blurb.
func runLLMs(cmd *cobra.Command, args []string) error {
	host := strings.TrimPrefix(strings.TrimPrefix(args[0], "https://"), "http://")
	host = strings.TrimSuffix(host, "/")
	idx, err := index.Open(index.PathFor(iColl))
	if err != nil {
		return err
	}
	defer idx.Close()
	pages, err := idx.ListHost(cmd.Context(), host, llmsLimit)
	if err != nil {
		return err
	}
	if len(pages) == 0 {
		return fmt.Errorf("no pages for %s in the index — run webx index %s first", host, host)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "# %s\n\n> Generated by webx from a local index (%d pages).\n\n", host, len(pages))
	bySection := map[string][]index.Page{}
	var order []string
	for _, p := range pages {
		section := "Pages"
		if u, err := url.Parse(p.URL); err == nil {
			segs := strings.Split(strings.Trim(u.Path, "/"), "/")
			if segs[0] != "" {
				section = segs[0]
			}
		}
		if _, seen := bySection[section]; !seen {
			order = append(order, section)
		}
		bySection[section] = append(bySection[section], p)
	}
	for _, sec := range order {
		fmt.Fprintf(out, "## %s\n\n", sec)
		for _, p := range bySection[sec] {
			title := p.Title
			if title == "" {
				title = p.URL
			}
			blurb := firstLine(p.Body, 120)
			if blurb != "" {
				fmt.Fprintf(out, "- [%s](%s): %s\n", title, p.URL, blurb)
			} else {
				fmt.Fprintf(out, "- [%s](%s)\n", title, p.URL)
			}
		}
		fmt.Fprintln(out)
	}
	return nil
}

// firstLine returns the first non-empty content line, de-markdowned and
// truncated — a one-line blurb for llms.txt entries.
func firstLine(body string, max int) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "#*>- ")
		line = strings.NewReplacer("*", "", "`", "", "&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(line)
		if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "!") {
			continue
		}
		if len(line) > max {
			line = line[:max-1] + "…"
		}
		return line
	}
	return ""
}

// ── wayback — Wayback Machine snapshots (dead-page recovery) ────────────────

var diffCmd = &cobra.Command{
	Use:   "diff <url>",
	Short: "Diff a page's current content against the indexed version",
	Args:  cobra.ExactArgs(1),
	RunE:  runDiff,
}

var (
	dDB       string
	dIndexNew bool
)

func runDiff(cmd *cobra.Command, args []string) error {
	dbPath := dbPath(dDB)
	idx, err := index.Open(dbPath)
	if err != nil {
		return err
	}
	defer idx.Close()
	old, err := idx.Get(cmd.Context(), args[0])
	if err != nil {
		return fmt.Errorf("no indexed version of %s (run webx index first)", args[0])
	}
	doc, err := fetch.Fetch(cmd.Context(), fetch.FetchRequest{
		URL: args[0], Browser: browser, Session: session,
		IfModifiedSince: old.FetchedAt,
	})
	if err != nil {
		return err
	}
	if doc.NotModified {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\nunchanged since %s (304 Not Modified)\n",
			args[0], old.FetchedAt.Format("2006-01-02 15:04"))
		return nil
	}
	added, removed := lineDiff(old.Body, doc.Markdown)
	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", args[0])
	fmt.Fprintf(cmd.OutOrStdout(), "indexed %s → now: +%d −%d lines\n\n",
		old.FetchedAt.Format("2006-01-02 15:04"), len(added), len(removed))
	for _, l := range removed {
		fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", l)
	}
	for _, l := range added {
		fmt.Fprintf(cmd.OutOrStdout(), "+ %s\n", l)
	}
	if dIndexNew {
		if err := idx.Put(cmd.Context(), index.Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown}); err != nil {
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "webx: index updated")
	}
	return nil
}

var (
	wEvery    time.Duration
	wOnce     bool
	wExec     string
	wQuiet    bool
	wSemantic bool
)

var watchCmd = &cobra.Command{
	Use:   "watch <url>",
	Short: "Monitor a page for content changes (Firecrawl scheduled-sync / Olostep monitor equivalent)",
	Long: `Poll a page on an interval; on each tick, fetch with a conditional
GET against the indexed snapshot and report added/removed lines. With --exec,
runs a shell command (env: WEBX_CHANGED_URL, WEBX_ADDED, WEBX_REMOVED) per
change — the self-hosted change-webhook.`,
	Args: cobra.ExactArgs(1),
	RunE: runWatch,
}

func runWatch(cmd *cobra.Command, args []string) error {
	dbPath := index.PathFor(iColl)
	idx, err := index.Open(dbPath)
	if err != nil {
		return err
	}
	defer idx.Close()
	target := args[0]
	out := cmd.OutOrStdout()

	// Seed the index with the baseline when absent so the first tick
	// measures against something real.
	if _, err := idx.Get(cmd.Context(), target); err != nil {
		doc, err := fetch.Fetch(cmd.Context(), fetch.FetchRequest{URL: target, Browser: browser, Session: session})
		if err != nil {
			return err
		}
		if err := idx.Put(cmd.Context(), index.Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown}); err != nil {
			return err
		}
		fmt.Fprintf(out, "watch: baseline indexed for %s\n", target)
	}

	tick := func() (changed bool, added, removed int, err error) {
		old, err := idx.Get(cmd.Context(), target)
		if err != nil {
			return false, 0, 0, err
		}
		doc, err := fetch.Fetch(cmd.Context(), fetch.FetchRequest{
			URL: target, Browser: browser, Session: session,
			IfModifiedSince: old.FetchedAt,
		})
		if err != nil {
			return false, 0, 0, err
		}
		if doc.NotModified {
			return false, 0, 0, nil
		}
		add, rm := lineDiff(old.Body, doc.Markdown)
		if len(add) == 0 && len(rm) == 0 {
			_ = idx.Put(cmd.Context(), index.Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown})
			return false, 0, 0, nil
		}
		_ = idx.Put(cmd.Context(), index.Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown})
		// --semantic: LLM judges whether the diff *matters* — timestamps,
		// template churn and tracking params get a "no" instead of an alert.
		if wSemantic {
			meaningful, reason := semanticChange(cmd.Context(), target, add, rm)
			if !meaningful {
				if !wQuiet {
					fmt.Fprintf(out, "  (semantic: ignored — %s)\n", reason)
				}
				return false, 0, 0, nil
			}
		}
		return true, len(add), len(rm), nil
	}

	for {
		changed, a, rm, err := tick()
		ts := time.Now().Format("15:04:05")
		switch {
		case err != nil:
			fmt.Fprintf(out, "%s  error: %v\n", ts, err)
		case changed:
			fmt.Fprintf(out, "%s  CHANGED  +%d −%d lines\n", ts, a, rm)
			if wExec != "" {
				c := exec.Command("sh", "-c", wExec)
				c.Env = append(os.Environ(),
					"WEBX_CHANGED_URL="+target,
					fmt.Sprintf("WEBX_ADDED=%d", a),
					fmt.Sprintf("WEBX_REMOVED=%d", rm))
				c.Stdout, c.Stderr = out, cmd.ErrOrStderr()
				_ = c.Run()
			}
		default:
			if !wQuiet {
				fmt.Fprintf(out, "%s  unchanged\n", ts)
			}
		}
		if wOnce {
			return nil
		}
		select {
		case <-cmd.Context().Done():
			return nil
		case <-time.After(wEvery):
		}
	}
}

// lineDiff reports lines present only in new (added) and only in old
// (removed) — multiset difference, order-independent.
func lineDiff(old, new string) (added, removed []string) {
	count := map[string]int{}
	for _, l := range strings.Split(old, "\n") {
		count[l]++
	}
	for _, l := range strings.Split(new, "\n") {
		if count[l] > 0 {
			count[l]--
		} else {
			added = append(added, l)
		}
	}
	for l, n := range count {
		for ; n > 0; n-- {
			removed = append(removed, l)
		}
	}
	return added, removed
}

// semanticChange asks the LLM whether a page diff is a meaningful content
// change — "the price changed" yes, "the CSRF token rotated" no. On error
// (no LLM configured, timeout) it errs toward meaningful: alerting on a
// false positive is better than silently dropping a real change.
func semanticChange(ctx context.Context, target string, added, removed []string) (bool, string) {
	clip := func(ls []string) string {
		s := strings.Join(ls, "\n")
		if len(s) > 3000 {
			s = s[:3000]
		}
		return s
	}
	out, err := fetch.LLMChat(ctx,
		`You judge whether a webpage change is semantically meaningful. Meaningful: new/removed content, changed facts, prices, dates, docs. Not meaningful: timestamps, session tokens, cache-busters, ad/tracker churn, cosmetic reordering, whitespace. Reply with exactly "YES <reason>" or "NO <reason>".`,
		fmt.Sprintf("URL: %s\n\n=== ADDED LINES ===\n%s\n\n=== REMOVED LINES ===\n%s", target, clip(added), clip(removed)),
		false)
	if err != nil {
		return true, "llm unavailable (" + err.Error() + ")"
	}
	out = strings.TrimSpace(out)
	if strings.HasPrefix(strings.ToUpper(out), "NO") {
		return false, strings.TrimSpace(strings.TrimPrefix(out, "NO"))
	}
	return true, strings.TrimSpace(strings.TrimPrefix(out, "YES"))
}

func runSeed(cmd *cobra.Command, args []string) error {
	logf := func(f string, a ...any) { fmt.Fprintf(cmd.ErrOrStderr(), "webx: "+f+"\n", a...) }
	var total, failed int
	for _, d := range seedDomains {
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		default:
		}
		logf("── %s", d)
		stats, err := index.IndexDomain(cmd.Context(), d, index.IndexOptions{
			Limit:         iLimit,
			Concurrency:   iConc,
			RatePerSec:    iRPS,
			StaleTTL:      iStale,
			RespectRobots: iRobots,
		}, logf)
		if err != nil {
			logf("%s: %v", d, err)
			failed++
			continue
		}
		total += stats.Pages
	}
	fmt.Fprintf(cmd.OutOrStdout(), "seeded %d pages across %d domains (%d failed)\n", total, len(seedDomains), failed)
	return nil
}

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/index"
	"github.com/spf13/cobra"
	"net/url"
	"os"
	"strings"
)

var mapCmd = &cobra.Command{
	Use:   "map <domain>",
	Short: "List a site's URLs from its sitemap",
	Args:  cobra.ExactArgs(1),
	RunE:  runMap,
}

var (
	mLimit  int
	mFormat string
)

func runMap(cmd *cobra.Command, args []string) error {
	rawurl := args[0]
	if !strings.Contains(rawurl, "://") {
		rawurl = "https://" + rawurl
	}
	base, err := url.Parse(rawurl)
	if err != nil {
		return fmt.Errorf("parse %q: %w", args[0], err)
	}
	urls, err := index.DiscoverURLs(cmd.Context(), base, mLimit)
	if err != nil {
		return err
	}
	switch mFormat {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"site": base.Host, "urls": urls})
	case "md", "markdown":
		for _, u := range urls {
			fmt.Fprintln(cmd.OutOrStdout(), u)
		}
		return nil
	default:
		return fmt.Errorf("unknown --format %q (want md|json)", mFormat)
	}
}

var (
	wbAt    string
	wbFrom  string
	wbTo    string
	wbLimit int
)

var waybackCmd = &cobra.Command{
	Use:   "wayback <url>",
	Short: "List Wayback Machine captures of a URL — or fetch one with --at (dead-page recovery)",
	Long: `Lists archived captures via the free CDX API (no key). With --at
<ts> (yyyy, yyyymmdd, or yyyymmddhhmmss) fetches the snapshot nearest that
date and extracts it like a live page — the path to content a 404 took.`,
	Args: cobra.ExactArgs(1),
	RunE: runWayback,
}

func runWayback(cmd *cobra.Command, args []string) error {
	target := args[0]
	if !strings.HasPrefix(target, "http") {
		target = "https://" + target
	}
	if wbAt == "" {
		snaps, err := index.WaybackSnapshots(cmd.Context(), target, wbFrom, wbTo, wbLimit)
		if err != nil {
			return err
		}
		if len(snaps) == 0 {
			fmt.Fprintf(cmd.ErrOrStderr(), "webx: no captures for %s\n", target)
			return nil
		}
		if format == "json" {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(snaps)
		}
		for _, s := range snaps {
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", s.Timestamp, s.MIME, s.URL)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "webx: %d captures — --at <ts> to fetch one\n", len(snaps))
		return nil
	}
	body, ct, ts, err := index.FetchWayback(cmd.Context(), target, wbAt)
	if err != nil {
		return err
	}
	doc, err := fetch.ExtractFromBody(body, ct, target)
	if err != nil {
		return err
	}
	doc.TierUsed = "wayback"
	if format == "json" {
		doc.Warnings = append([]string{"wayback capture " + ts + " — content may be stale or rewritten"}, doc.Warnings...)
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "webx: replaying capture %s of %s\n", ts, target)
	_, err = fmt.Fprintln(cmd.OutOrStdout(), doc.Markdown)
	return err
}

// ── cdx — Common Crawl URL enumeration (map without crawling) ───────────────

var (
	cdxCrawl string
	cdxLimit int
)

var cdxCmd = &cobra.Command{
	Use:   "cdx <domain|url-pattern>",
	Short: "List a domain's captured URLs from Common Crawl's index — discover without crawling",
	Long: `Queries the CDXJ index (e.g. example.com/* — * matches subdomains
in SURT order) and prints captured URLs. Zero requests to the target host;
pair with 'index --from-cc' to fetch the archived bodies too.`,
	Args: cobra.ExactArgs(1),
	RunE: runCDX,
}

func runCDX(cmd *cobra.Command, args []string) error {
	pattern := args[0]
	pattern = strings.TrimPrefix(pattern, "https://")
	pattern = strings.TrimPrefix(pattern, "http://")
	if !strings.ContainsAny(pattern, "*") && !strings.HasSuffix(pattern, "/") {
		pattern += "/*"
	}
	crawl := cdxCrawl
	if crawl == "" || crawl == "latest" {
		var err error
		if crawl, err = index.LatestCrawl(cmd.Context()); err != nil {
			return err
		}
	}
	recs, err := index.QueryCDX(cmd.Context(), crawl, pattern, cdxLimit)
	if err != nil {
		return err
	}
	if format == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(recs)
	}
	for _, r := range recs {
		fmt.Fprintln(cmd.OutOrStdout(), r.URL)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "webx: %d urls in %s for %s\n", len(recs), crawl, pattern)
	return nil
}

// ── archive — WARC/1.0 export (ISO 28500 replayable records) ────────────────

var archOut string

var archiveCmd = &cobra.Command{
	Use:   "archive <url> [url...]",
	Short: "Write raw fetches as a WARC/1.0 archive — ISO-standard, replayweb.page-compatible",
	Long: `Fetches each URL raw and writes one WARC response record per page
to --out (default webx.warc). The standard interchange format of web
archiving — open it in replayweb.page, feed it to pywb, re-extract later.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runArchive,
}

func runArchive(cmd *cobra.Command, args []string) error {
	out := archOut
	if out == "" {
		out = "webx.warc"
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, u := range args {
		status, hdr, body, ferr := fetch.FetchWARC(cmd.Context(), u, timeout, browser, insecure, session, proxy, cookieFile)
		if ferr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "webx: archive %s: %v\n", u, ferr)
			continue
		}
		if werr := fetch.WriteWARCResponse(w, u, status, hdr, body); werr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "webx: warc write %s: %v\n", u, werr)
			continue
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "webx: archived %s (%d, %d bytes)\n", u, status, len(body))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "webx: %s written\n", out)
	return nil
}

var extractCmd = &cobra.Command{
	Use:   "extract <url>",
	Short: "Structured extraction — page markdown → LLM → schema'd JSON",
	Args:  cobra.ExactArgs(1),
	RunE:  runExtract,
}

var (
	xSchema string
	xPrompt string
	xCSS    string
	xType   string
	xFormat string
)

func runExtract(cmd *cobra.Command, args []string) error {
	// --type: schema.org typed extraction from ld+json — zero LLM, beats
	// selectors when a site publishes structured data (most commerce/
	// publishing platforms do).
	if xType != "" {
		doc, err := fetch.Fetch(cmd.Context(), fetch.FetchRequest{
			URL: args[0], Browser: browser, Session: session, Render: doRender,
		})
		if err != nil {
			return err
		}
		var ents []json.RawMessage
		if xType == "auto" {
			ents = doc.JSONLD
		} else {
			ents = fetch.LDOfType(doc.JSONLD, xType)
		}
		if len(ents) == 0 {
			if len(doc.JSONLD) == 0 {
				return fmt.Errorf("no ld+json entities on this page at all")
			}
			return fmt.Errorf("no ld+json entities of type %q on this page", xType)
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if len(ents) == 1 {
			var single any
			_ = json.Unmarshal(ents[0], &single)
			return enc.Encode(map[string]any{"url": doc.FinalURL, "data": single})
		}
		var many []any
		for _, e := range ents {
			var v any
			_ = json.Unmarshal(e, &v)
			many = append(many, v)
		}
		return enc.Encode(map[string]any{"url": doc.FinalURL, "data": many})
	}
	// --css: deterministic selector extraction — no LLM needed.
	if xCSS != "" {
		var schema map[string]any
		if err := json.Unmarshal([]byte(xCSS), &schema); err != nil {
			return fmt.Errorf("--css must be a JSON object of field→selector: %w", err)
		}
		doc, err := fetch.Fetch(cmd.Context(), fetch.FetchRequest{
			URL: args[0], Browser: browser, Session: session, Render: doRender, WantHTML: true,
		})
		if err != nil {
			return err
		}
		data, err := fetch.ExtractCSS([]byte(doc.HTML), schema)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"url": doc.FinalURL, "data": data})
	}
	var schema map[string]any
	if xSchema != "" {
		if err := json.Unmarshal([]byte(xSchema), &schema); err != nil {
			return fmt.Errorf("--schema must be a JSON object: %w", err)
		}
	} else {
		schema = map[string]any{"type": "object"}
	}
	res, err := fetch.Extract(cmd.Context(), fetch.ExtractRequest{
		URL: args[0], Schema: schema, Prompt: xPrompt,
		Browser: browser, Session: session,
	})
	if err != nil {
		return err
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

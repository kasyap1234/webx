package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/kasyap1234/webx/client"
	"github.com/kasyap1234/webx/web/fetch"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "webx",
	Short: "The web toolkit for AI coding agents",
	Long: `webx scrapes, searches, maps and crawls the web —
clean markdown and JSON for coding agents and humans.`,
	Version: "0.1.0",
}

// apiClient is non-nil when --api points the CLI at a remote webx/webxd.
func apiClient() *client.Client {
	if apiURL == "" {
		return nil
	}
	return client.New(apiURL)
}
func init() {
	scrapeCmd.Flags().StringVarP(&format, "format", "f", "md", "output format: md|json")
	scrapeCmd.Flags().IntVar(&maxChars, "max-chars", 0, "truncate markdown to N chars (0 = no limit)")
	scrapeCmd.Flags().BoolVar(&raw, "raw", false, "skip readability extraction, convert the full page")
	scrapeCmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "request timeout")
	scrapeCmd.Flags().StringVar(&userAgent, "user-agent", "", "override User-Agent header")
	scrapeCmd.Flags().BoolVar(&browser, "browser", false, "use a real Chrome TLS fingerprint (uTLS) — passes first-line bot checks")
	scrapeCmd.Flags().StringVar(&session, "session", "", "persistent cookie jar name under ~/.webx/sessions/")
	scrapeCmd.Flags().BoolVar(&doRender, "render", false, "render JavaScript via Chrome (go-rod) — for SPA/client-rendered pages")
	scrapeCmd.Flags().BoolVar(&autoRender, "auto-render", false, "escalate to Chrome only when the page looks JS-required")
	scrapeCmd.Flags().StringVar(&waitFor, "wait-for", "", "CSS selector to wait for when rendering")
	scrapeCmd.Flags().IntVar(&waitMs, "wait", 0, "ms to wait after load — delayed JS injects (implies render)")
	scrapeCmd.Flags().IntVar(&scrolls, "scrolls", 0, "scroll passes for infinite/virtual pages when rendering")
	scrapeCmd.Flags().StringVar(&actions, "actions", "", "browser steps before extraction: \"click:.accept | wait:.quote | type:#q=term | press:enter | scroll | screenshot\"")
	scrapeCmd.Flags().StringVar(&screenshot, "screenshot", "", "capture a PNG while rendering → file path (or screenshot_b64 with -f json)")
	scrapeCmd.Flags().BoolVar(&stealth, "stealth", false, "patch headless-Chrome fingerprints (webdriver/plugins) when rendering")
	scrapeCmd.Flags().StringVar(&profile, "profile", "", "persistent Chrome profile name (~/.webx/profiles/<name>) — logins survive runs")
	scrapeCmd.Flags().BoolVar(&blockAds, "block-ads", false, "drop tracker/ad requests when rendering")
	scrapeCmd.Flags().BoolVar(&textMode, "text-mode", false, "drop images/fonts/media/css when rendering — faster + less noise")
	scrapeCmd.Flags().BoolVar(&mobile, "mobile", false, "emulate a phone viewport + UA when rendering")
	scrapeCmd.Flags().StringVar(&locale, "locale", "", "browser locale e.g. fr-FR (Accept-Language + navigator.language)")
	scrapeCmd.Flags().StringVar(&tz, "tz", "", "browser timezone e.g. Europe/Paris")
	scrapeCmd.Flags().BoolVar(&netCap, "network", false, "record page XHR/fetch traffic → network[] in json output")
	scrapeCmd.Flags().BoolVar(&conCap, "console", false, "record console.* messages → console[] in json output")
	scrapeCmd.Flags().StringVar(&pdfOut, "pdf", "", "save rendered page as PDF → file path (or pdf_b64 with -f json)")
	scrapeCmd.Flags().StringVar(&mhtmlOut, "mhtml", "", "save single-file archive → file path (or mhtml with -f json)")
	scrapeCmd.Flags().BoolVar(&insecure, "insecure", false, "skip TLS verification (dev targets with bad certs)")
	scrapeCmd.Flags().StringVar(&pageSess, "page-session", "", "named live tab — persists across renders for multi-step flows (Steel session_id parity)")
	scrapeCmd.Flags().BoolVar(&pierce, "pierce", false, "flatten shadow roots + same-origin iframes into the output")
	scrapeCmd.Flags().StringVar(&scrollSel, "scroll-sel", "", "scroll this element instead of the window (virtual lists)")
	scrapeCmd.Flags().Float64Var(&scrollBy, "scroll-by", 0, "px per scroll pass (default viewport-ish 1200)")
	scrapeCmd.Flags().StringVar(&proxy, "proxy", "", "route fetches through this http proxy (or WEBX_PROXY)")
	scrapeCmd.Flags().StringVar(&fit, "fit", "", "return only blocks relevant to this query (BM25 content filter — the token-saving path)")
	scrapeCmd.Flags().StringSliceVar(&incSel, "include", nil, "CSS selector(s) — extract only matching elements (Jina X-Target-Selector style)")
	scrapeCmd.Flags().StringSliceVar(&excSel, "exclude", nil, "CSS selector(s) — strip matching elements before extraction")
	scrapeCmd.Flags().BoolVar(&wantSum, "summary", false, "attach an LLM page summary (WEBX_LLM_*, best effort)")
	scrapeCmd.Flags().BoolVar(&wantLinks, "links", false, "list absolute links found on the page")
	scrapeCmd.Flags().BoolVar(&wantImgs, "images", false, "list absolute image URLs + alt text")
	scrapeCmd.Flags().BoolVar(&wantRefSum, "link-summary", false, "append aggregated ## Links/## Images reference list to the markdown (Jina-style)")
	scrapeCmd.Flags().BoolVar(&wantBrand, "branding", false, "site identity — logo, theme-color, icons, generator → branding{} in json")
	scrapeCmd.Flags().BoolVar(&wantChunks, "chunks", false, "RAG-ready segments with heading paths + est_tokens → chunks[] in json")
	scrapeCmd.Flags().BoolVar(&wantA11y, "a11y", false, "accessibility-tree snapshot with @eN action refs (implies --render) → a11y in json")
	scrapeCmd.Flags().StringVar(&engine, "engine", "", "render engine: chrome (default) | light — Lightpanda over CDP, ~16x lighter, reduced fidelity")
	scrapeCmd.Flags().BoolVar(&wantAgentR, "agent-ready", false, "probe host agent-friendliness — llms.txt, AI-bot robots rules, WebMCP tools, TDMRep/ai.txt compliance → agent_ready{} in json")
	scrapeCmd.Flags().IntVar(&maxTokens, "max-tokens", 0, "cap output at ~N tokens (4 chars/token)")
	scrapeCmd.Flags().StringVar(&cookieFile, "cookie-file", "", "Netscape cookies.txt — carry an SSO'd browser session into the fetch")
	scrapeCmd.Flags().BoolVar(&retryAfter, "retry-after", false, "honor a server's Retry-After once on 429/503 (bounded at 8s — beyond that, rate_limited)")
	scrapeCmd.Flags().BoolVar(&transcript, "transcript", false, "YouTube-only fast path — InnerTube captions → transcript (use --locale for caption language)")
	scrapeCmd.Flags().BoolVar(&redactPII, "redact-pii", false, "mask emails/phones/SSN-ish strings in the output")

	searchCmd.Flags().StringVarP(&sFormat, "format", "f", "md", "output format: md|json")
	searchCmd.Flags().IntVarP(&sNum, "num", "n", 10, "max results")
	searchCmd.Flags().StringVar(&sProviders, "providers", "", "comma-separated providers (index,ddg,hn,so,wiki,gh,reddit,searxng,brave,mojeek,kagi,grep,sg,npm,crates); default: free ones")
	searchCmd.Flags().StringVar(&sSite, "site", "", "restrict results to this domain")
	searchCmd.Flags().StringVar(&sAfter, "after", "", "drop results published before YYYY-MM-DD (when date is known)")
	searchCmd.Flags().StringVar(&sBefore, "before", "", "drop results published after YYYY-MM-DD (when date is known)")
	searchCmd.Flags().StringVar(&sDomains, "domains", "", "keep only results under these domains (comma-separated)")
	searchCmd.Flags().StringVar(&sExclDomains, "exclude-domains", "", "drop results under these domains (comma-separated)")
	searchCmd.Flags().StringVar(&sTopic, "topic", "", "topic hint: news boosts freshness + news-category providers")
	searchCmd.Flags().StringVar(&sLang, "lang", "", "language hint for providers that support it (searxng, wiki)")
	searchCmd.Flags().BoolVar(&sExact, "exact", false, "phrase-match the query verbatim")
	searchCmd.Flags().BoolVar(&sHighlights, "highlights", false, "fetch result pages but keep only query-relevant excerpts — the token-saving scrape")
	searchCmd.Flags().BoolVar(&sScrape, "scrape", false, "fetch each result's content + highlights inline")
	searchCmd.Flags().BoolVar(&sRerank, "rerank", false, "re-sort results by query relevance — embedding cosine on snippets; pair with --scrape for content-level rerank (WEBX_EMBED_*, lexical fallback)")
	searchCmd.Flags().BoolVar(&sFresh, "fresh", false, "bypass the 10m result cache — fresh provider calls (still writes cache)")
	searchCmd.Flags().IntVar(&sScrapeChars, "content-chars", 2000, "cap per-result content chars when --scrape")
	searchCmd.Flags().BoolVar(&browser, "browser", false, "Chrome TLS fingerprint for --scrape fetches")
	searchCmd.Flags().StringVar(&session, "session", "", "persistent cookie jar name")
	searchCmd.Flags().BoolVar(&doRender, "render", false, "render --scrape pages via Chrome (JS pages)")
	searchCmd.Flags().BoolVar(&autoRender, "auto-render", false, "escalate to Chrome only on detected JS-shells")

	mapCmd.Flags().IntVar(&mLimit, "limit", 1000, "max URLs")
	mapCmd.Flags().StringVarP(&mFormat, "format", "f", "md", "output format: md|json")
	mapCmd.Flags().StringVar(&mSearch, "search", "", "keep only URLs containing this string")
	mapCmd.Flags().BoolVar(&mSubdomains, "subdomains", false, "include subdomains of the target host")
	mapCmd.Flags().BoolVar(&mNoSitemap, "ignore-sitemap", false, "skip the sitemap — harvest same-host links from live pages")

	askCmd.Flags().IntVarP(&aNum, "num", "n", 5, "sources to cite")
	askCmd.Flags().IntVar(&aTokens, "max-tokens", 3000, "token budget for output")
	askCmd.Flags().BoolVar(&aLLM, "llm", false, "append an LLM-synthesized answer citing the sources (WEBX_LLM_*)")
	askCmd.Flags().StringVarP(&format, "format", "f", "md", "output format: md|json")

	evalCmd.Flags().IntVarP(&eNum, "num", "n", 10, "results per query")
	evalCmd.Flags().StringVar(&eProviders, "providers", "", "comma-separated providers (default: free set)")
	evalCmd.Flags().StringVar(&eSet, "set", "", "path to a YAML eval set (default: bundled)")
	evalCmd.Flags().BoolVar(&eExtract, "extract", false, "score extraction quality (marker coverage + code recall) instead of search")
	evalCmd.Flags().BoolVar(&eCite, "cite", false, "score citation-verifier precision/recall on the labeled pair set")
	evalCmd.Flags().StringVar(&eVs, "vs", "", "compare against engines: exa,tavily,firecrawl (needs their API keys)")
	evalCmd.Flags().BoolVar(&eJSON, "json", false, "machine-readable report output")
	evalCmd.Flags().BoolVar(&eGen, "gen", false, "generate a ~1000-query corpus from ORCAS clicks + Stack Exchange + GitHub")
	evalCmd.Flags().StringVar(&eGenOut, "gen-out", "", "also save the generated corpus YAML here")
	evalCmd.Flags().IntVar(&eWorkers, "workers", 4, "concurrent queries during the eval run")

	indexCmd.Flags().IntVar(&iLimit, "limit", 500, "max pages to index")
	indexCmd.Flags().IntVar(&iConc, "concurrency", 4, "parallel fetches")
	indexCmd.Flags().Float64Var(&iRPS, "rps", 3.0, "requests per second (politeness)")
	indexCmd.Flags().StringVar(&iDB, "db", "", "index db path (default ~/.webx/index.db)")
	indexCmd.Flags().StringVar(&iFromCC, "from-cc", "", "index from Common Crawl instead of live crawling ('latest' or CC-MAIN-YYYY-NN)")
	indexCmd.Flags().DurationVar(&iStale, "stale", 0, "skip pages fetched within this window (e.g. 720h)")
	indexCmd.Flags().BoolVar(&iRobots, "respect-robots", false, "honor robots.txt Disallow rules")
	indexCmd.Flags().StringSliceVar(&iIncPaths, "include-paths", nil, "regex — only index matching URL paths")
	indexCmd.Flags().StringSliceVar(&iExcPaths, "exclude-paths", nil, "regex — never index matching URL paths")
	indexCmd.Flags().BoolVar(&iGC, "gc", false, "garbage-collect the index instead of crawling")
	indexCmd.Flags().DurationVar(&iOlder, "older-than", 0, "with --gc: drop pages older than this (default 90d)")
	indexCmd.Flags().BoolVar(&iVacuum, "vacuum", false, "with --gc: VACUUM the db after deletion")
	jobsCmd.Flags().BoolVar(&jobsGC, "gc", false, "delete finished jobs older than --older-than (local jobs.db only)")
	jobsCmd.Flags().DurationVar(&jobsOlderThan, "older-than", 7*24*time.Hour, "with --gc: retention horizon (default 7d)")
	for _, c := range []*cobra.Command{indexCmd, searchCmd, queryCmd, similarCmd, llmsCmd, watchCmd, diffCmd} {
		c.Flags().StringVar(&iColl, "collection", "", "named index corpus (~/.webx/index-<name>.db)")
	}
	searchCmd.Flags().StringVar(&sDepth, "depth", "", "search depth tier: fast|basic|advanced")
	searchCmd.Flags().StringSliceVar(&sSources, "sources", nil, "result verticals to include: web,news,images")
	searchCmd.Flags().StringVar(&sLocation, "location", "", "ISO country code geo hint (Exa userLocation / FC location) — biases regional providers like DDG")
	searchCmd.Flags().StringVar(&sCategory, "category", "", "vertical preset: developer restricts providers to code/docs sources (so,gh,grep,sg,npm,crates,hn,reddit)")
	searchCmd.Flags().BoolVar(&sAnswer, "answer", false, "attach a cited extractive answer (scrapes top results at highlights depth)")
	searchCmd.Flags().IntVar(&sSubpages, "subpages", 0, "crawl N sitemap subpages per top result")
	searchCmd.Flags().StringSliceVar(&sSubpageTgt, "subpage-target", nil, "keyword filter for subpage candidates")

	seedCmd.Flags().IntVar(&iLimit, "limit", 300, "max pages per domain")
	seedCmd.Flags().IntVar(&iConc, "concurrency", 4, "parallel fetches")
	seedCmd.Flags().Float64Var(&iRPS, "rps", 3.0, "requests per second (politeness)")
	seedCmd.Flags().DurationVar(&iStale, "stale", 168*time.Hour, "skip domains/pages fetched within this window")
	seedCmd.Flags().BoolVar(&iRobots, "respect-robots", false, "honor robots.txt Disallow rules")

	crawlCmd.Flags().IntVar(&cLimit, "limit", 30, "max pages to fetch")
	crawlCmd.Flags().IntVar(&cDepth, "depth", 3, "max link depth")
	crawlCmd.Flags().IntVar(&cTarget, "target", 8, "stop after this many relevant pages")
	crawlCmd.Flags().Float64Var(&cThreshold, "threshold", 0.35, "relevance needed to count a page")
	crawlCmd.Flags().StringVarP(&cFormat, "format", "f", "md", "output format: md|json")
	crawlCmd.Flags().BoolVar(&cRobots, "respect-robots", false, "honor robots.txt Disallow rules")
	crawlCmd.Flags().StringSliceVar(&cIncPaths, "include-paths", nil, "regex — only crawl matching URL paths")
	crawlCmd.Flags().StringSliceVar(&cExcPaths, "exclude-paths", nil, "regex — never crawl matching URL paths")
	crawlCmd.Flags().BoolVar(&cSemantic, "semantic", false, "score page relevance by embedding cosine (needs WEBX_EMBED_MODEL) — focused crawling by meaning")

	researchCmd.Flags().IntVar(&rSources, "sources", 6, "max pages to read")
	researchCmd.Flags().StringVar(&rSchema, "schema", "", "JSON Schema — return the report as matching JSON (Tavily output_schema)")
	verifyCmd.Flags().IntVar(&vSources, "sources", 4, "max pages to read")
	verifyCmd.Flags().StringVarP(&format, "format", "f", "md", "output format: md|json")

	similarCmd.Flags().IntVar(&simLimit, "limit", 10, "max similar pages")
	similarCmd.Flags().BoolVar(&simWeb, "web", false, "search the live web for similar pages (Exa findSimilar) instead of the local index")
	similarCmd.Flags().StringVarP(&format, "format", "f", "md", "output format: md|json")
	llmsCmd.Flags().IntVar(&llmsLimit, "limit", 300, "max pages to include")
	llmsCmd.Flags().StringVarP(&format, "format", "f", "md", "output format: md|json")

	extractCmd.Flags().StringVar(&xSchema, "schema", "", "JSON schema for the data to extract")
	extractCmd.Flags().StringVar(&xPrompt, "prompt", "", "extraction instructions")
	extractCmd.Flags().StringVar(&xCSS, "css", "", `selector schema — no LLM: {"title":"h1","links":"a[]@href","items":{"selector":".row[]","fields":{"n":".name"}}}`)
	extractCmd.Flags().StringVar(&xType, "type", "", "schema.org entity type from ld+json — auto|product|article|job|event|faq (zero LLM)")
	extractCmd.Flags().BoolVar(&doRender, "render", false, "render JS before extracting (needed for SPA pages)")
	extractCmd.Flags().BoolVar(&browser, "browser", false, "Chrome TLS fingerprint")
	extractCmd.Flags().StringVar(&session, "session", "", "persistent cookie jar name")

	diffCmd.Flags().StringVar(&dDB, "db", "", "index db path (default ~/.webx/index.db)")
	diffCmd.Flags().BoolVar(&dIndexNew, "update-index", false, "write the fresh version back to the index")
	diffCmd.Flags().StringVarP(&format, "format", "f", "md", "output format: md|json")
	diffCmd.Flags().BoolVar(&browser, "browser", false, "Chrome TLS fingerprint")
	diffCmd.Flags().StringVar(&session, "session", "", "persistent cookie jar name")

	watchCmd.Flags().DurationVar(&wEvery, "every", 10*time.Minute, "poll interval")
	watchCmd.Flags().BoolVar(&wOnce, "once", false, "single check then exit (cron-friendly)")
	watchCmd.Flags().StringVar(&wExec, "exec", "", "shell command run on each change (env: WEBX_CHANGED_URL/ADDED/REMOVED)")
	watchCmd.Flags().BoolVar(&wQuiet, "quiet", false, "only print changes")
	watchCmd.Flags().BoolVar(&wSemantic, "semantic", false, "LLM judges whether a diff is a meaningful change (needs WEBX_LLM_*)")
	watchCmd.Flags().BoolVar(&browser, "browser", false, "Chrome TLS fingerprint")
	watchCmd.Flags().StringVar(&session, "session", "", "persistent cookie jar name")

	serveCmd.Flags().StringVar(&serveAddr, "addr", ":8080", "listen address")
	serveCmd.Flags().StringVar(&serveDB, "jobs-db", "", "job store path (default ~/.webx/jobs.db)")
	serveCmd.Flags().IntVar(&serveWorkers, "workers", 2, "async job workers")

	batchCmd.Flags().IntVar(&bConcurrency, "concurrency", 4, "parallel fetches for local batch")
	batchCmd.Flags().StringVarP(&bFormat, "format", "f", "md", "output format: md|json (json = ndjson)")

	rootCmd.PersistentFlags().StringVar(&apiURL, "api", "", "remote webx/webxd base URL — scrape/search/crawl run server-side")

	queryCmd.Flags().BoolVar(&sSemantic, "semantic", false, "fuse FTS5 with embedding cosine — needs WEBX_EMBED_MODEL (+WEBX_EMBED_BASE/KEY)")
	queryCmd.Flags().StringVarP(&sFormat, "format", "f", "md", "output format: md|json")
	botkeyCmd.Flags().StringVar(&botkeyOut, "out", fetch.BotKeyPath(), "key file path")
	botkeyCmd.Flags().StringVar(&botkeyDir, "directory", "", "https URL where you'll serve the JWKS (saved into the key file)")
	licenseCmd.Flags().StringVar(&licTier, "tier", "pro", "license tier: pro|enterprise")
	licenseCmd.Flags().StringVar(&licEmail, "email", "", "customer email on the license")
	licenseCmd.Flags().IntVar(&licDays, "days", 365, "days until expiry")
	home, _ := os.UserHomeDir()
	licenseCmd.Flags().StringVar(&licKey, "key", filepath.Join(home, ".webx", "maintainer.pem"), "maintainer signing key (keygen writes, gen reads)")
	licenseCmd.Flags().StringVar(&licOut, "out", "", "license output path (stdout when empty)")
	installCmd.Flags().StringVar(&installBin, "bin-dir", "", "install dir for lightpanda (default ~/.webx/bin)")

	waybackCmd.Flags().StringVar(&wbAt, "at", "", "fetch the capture nearest this timestamp (yyyy|yyyymmdd|yyyymmddhhmmss)")
	waybackCmd.Flags().StringVar(&wbFrom, "from", "", "only list captures after yyyy[mmdd[hhmmss]]")
	waybackCmd.Flags().StringVar(&wbTo, "to", "", "only list captures before yyyy[mmdd[hhmmss]]")
	waybackCmd.Flags().IntVar(&wbLimit, "limit", 200, "max captures to list")
	waybackCmd.Flags().StringVarP(&format, "format", "f", "md", "output format: md|json")

	cdxCmd.Flags().StringVar(&cdxCrawl, "crawl", "latest", "crawl collection (latest | CC-MAIN-YYYY-NN)")
	cdxCmd.Flags().IntVar(&cdxLimit, "limit", 500, "max URLs")
	cdxCmd.Flags().StringVarP(&format, "format", "f", "md", "output format: md|json")

	archiveCmd.Flags().StringVarP(&archOut, "out", "o", "webx.warc", "WARC output path")

	rootCmd.AddCommand(scrapeCmd, searchCmd, indexCmd, queryCmd, mapCmd, askCmd,
		mcpCmd, evalCmd, doctorCmd, seedCmd, crawlCmd, extractCmd, diffCmd, serveCmd,
		jobsCmd, cancelCmd, batchCmd, researchCmd, similarCmd, watchCmd, verifyCmd, skillCmd, llmsCmd,
		botkeyCmd, installCmd, waybackCmd, cdxCmd, archiveCmd, licenseCmd)
	// Errors print `Error: <msg>` only — a 38-flag usage dump after every
	// failed fetch is noise for the agents this tool is built for.
	for _, c := range rootCmd.Commands() {
		c.SilenceUsage = true
	}
	rootCmd.SilenceUsage = true
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		// --format json promised machine output — keep the contract on
		// failures: code + payment metadata stay machine-readable.
		if format == "json" {
			out := map[string]any{"error": err.Error()}
			if c := fetch.ErrorCode(err); c != "" {
				out["code"] = c
			}
			if p := fetch.PaymentOf(err); p != nil {
				out["payment"] = p
			}
			json.NewEncoder(os.Stdout).Encode(out)
		}
		os.Exit(1)
	}
}

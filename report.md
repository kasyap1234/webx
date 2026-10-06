
## Polish audit — agent-facing quality pass

Systematic edge-case sweep (bad inputs, empty results, JSON shapes,
stdout/stderr split, help consistency, MCP tool quality). Five more
fixes shipped.

### 17. `published: "0001-01-01T00:00:00Z"` in search JSON → FIXED

`omitempty` doesn't apply to `time.Time` — zero-dates serialized as
epoch-zero. Now `,omitzero` (`web/search/search.go`).

### 18. HTML comments leaked into markdown → FIXED

`<!--THE END-->`/CMS template markers survived extraction into
markdown — poisoning snippets, diffs, fit and llms output. New
`stripHTMLComments` post-pass removes them **outside fenced code
blocks only** (HTML tutorials legitimately show comments in code).
Tests cover multi-line comments + fence preservation
(`web/fetch/refs.go`, `refs_test.go`).

### 19. `verify`/`ask` had no machine-readable output → FIXED

`runVerify` checked `format == "json"` but the flag was never bound —
dead code path. `-f` now bound on both; `ask` gained a real JSON branch
(`{"question","answer","sources":[{title,url,excerpt}]}`). Agents no
longer parse display text (`cmd/webx/main.go`, `search.go`).
Verified live: `verify -f json` → `{"verdict":"supported","confidence":0.99,…}`.

### 20. `--agent-ready` silent on zero-signal hosts → FIXED

`doc.AgentReady` was only set when a probe found something — "not
probed" and "probed, nothing" looked identical. Now always emits
`agent_ready: {}` for "probed, host exposes nothing"
(`web/fetch/agentready.go`).

### 21. `doctor` ignored the LLM backend → FIXED

`verify`/`extract`/`research`/`ask --llm` all depend on `WEBX_LLM_*` but
`doctor` reported providers only — a dead LLM was invisible in the one
place users look. New `fetch.LLMHealth` (GET /models — reachability +
model-listed check, no token spend) prints a `llm` row.
Verified: `ok llm 609ms model ready` on OpenRouter.

### Also improved this pass

- `extract --type` errors now name the fallbacks: `try --css or LLM
  --schema instead` (`cmd/webx/mapx.go`).

### Assessed, deliberately not changed

- Nav/footer chrome in extraction on nav-heavy pages (go.dev/learn) —
  the `mdQuality` trafilatura pass already catches worst cases; deeper
  boilerplate removal is an extractor project, not a patch.
- `batch`/`map`/`search` plain-text formats — functional, consistent.
- MCP tool descriptions — already written for agents ("Use when…"),
  verified across all 17 tools.
- `query`/`similar`/`diff`/`llms` JSON modes — text output is already
  line-structured; -f would be nice-to-have, not a defect.

---

## Improvement pass — 2026-10-04 (features built, not just fixes)

User asked for the highest-leverage product improvements to be built
(ranked earlier: reader endpoint, search rerank, output coverage,
extraction quality, tests). All implemented and verified live.

### I1. `GET /r/<url>` Jina-style reader endpoint → NEW

Zero-setup front door: `curl http://host/r/https://example.com` returns
`Title:` / `URL Source:` / `Published Time:` / `Markdown Content:` —
Jina's response shape, so r.jina.ai users switch with a host change.

- `Accept: application/json` → full Document JSON (tier_used, links…)
- `X-Render`, `X-Engine: chrome|light`, `X-Token-Budget`,
  `X-Target-Selector`, `X-Remove-Selector` headers
- `AutoRender` on by default — escalation only on detection
- Go ServeMux canonicalizes `https://` → `https:/` (301); handler
  reconstructs the scheme from the match group
- Same TargetGuard SSRF policy — 403 on private/loopback/metadata IPs

Verified live: markdown + query passthrough + JSON (`tier_used: http`) +
`403` on `169.254.169.254` and `localhost`.
(`internal/serve/reader.go`, route `GET /r/{url...}` in `serve.go`)

### I2. `--rerank` without `--scrape` → FIXED + lexical fallback strengthened

`rerankResults` already did embedding-cosine rerank (`WEBX_EMBED_MODEL`)
but only ran inside the `--scrape` path. Now `search --rerank` reranks on
title+snippet — no page fetches; `--scrape --rerank` still ranks on page
bodies. The lexical fallback was a ~1% no-op on snippets (blockScore
needs ≥40-char blocks): snippets now score by **term coverage** (up to
+40%), scraped bodies keep the bounded content nudge.
(`web/search/search.go`, `scrape.go`; `cmd/webx/search.go`,
`main.go` help)

Verified: `--rerank` promoted go.dev/doc/tutorial/generics + intro-
generics blog above generic hits; fresh query top score 1.000 after
normalize.

### I3. `-f json` for query/similar/diff/llms → NEW

The last unstructured commands now emit JSON for agent pipelines:

- `query -f json` — full search Response (shares runSearch)
- `similar -f json` — `{"url","mode","similar":[{score,title,url}]}`
- `diff -f json` — `{"url","indexed_at","changed","added","removed",
  "added_lines","removed_lines"}`; `{"not_modified":true}` on 304
- `llms -f json` — `{"domain","page_count","pages":[{title,url,blurb,
  section}]}`

All verified against the local test index (`cmd/webx/index_cmd.go`).

### I4. Boilerplate pass 2 — link-soup + footnote stripping → NEW

`stripLinkClusters` (fence-aware, like stripHTMLComments):

1. `[\[N\]](url)`-style footnote/citation markers removed inline —
   converters escape inner brackets (`\[`), matched both forms incl.
   optional "title" attrs.
2. Non-list lines that are ≥4 links at ≥85% link-syntax density dropped
   (nav menus, infobox soup). Bullet lists and headings are never
   candidates — docs index/TOC sections legitimately look like link
   lists and are preserved.
3. Blank-line runs left by removed blocks collapse to one.

Verified: Wikipedia "Go" page 1377→624 lines, prose untouched;
go.dev/doc renders all sections incl. `### [Installing Go]` headings.
(`web/fetch/refs.go`, post-pass in `fetch.go`)

### I5. Regression tests → NEW

- `structuredUnsupported` — OpenAI envelope + OpenRouter `metadata.raw`
  "structured-outputs" + negatives (`web/fetch/llm_test.go`)
- `llmMaxTokens`/`llmTimeout` — defaults, env override, invalid/negative
  fallback
- `LLMHealth` — model ready / not listed / unreachable (httptest)
- `LLMChat` 429→retry→success path
- `published omitzero` — zero time.Time not serialized; real time kept
- reader — markdown shape, scheme repair, query passthrough, SSRF 403s
  (fetch never reached), JSON, token budget, empty-path usage
- rerank — lexical fallback reorders near-ties on coverage; scraped
  content bypasses the snippet path
- stripLinkClusters — footnotes, nav soup, list/prose/fence preservation

`go build ./...`, `go vet ./...`, `go test ./...` — all green.

---

## Competitor-parity pass — 2026-10-04 (existing features vs Firecrawl v2 / Exa / Tavily / Jina)

Re-audited the feature surface against the live competitor API docs
(Firecrawl v2 scrape/search, Exa search reference, Tavily, Jina). The
fetch/render surface is already deeper than Firecrawl's (a11y,
agent_ready, transcript, redact_pii, branding, pierce, page sessions).
Gaps found — all in *existing* features — fixed below.

### J1. `/v2/map` SSRF hole + dead params → FIXED (security)

The Firecrawl-compat route skipped `TargetGuard.CheckURL` that every
other fetch route enforces — `POST /v2/map {"url":"http://169.254.169.254"}`
reached outbound fetch. Now 403. Additionally `includeSubdomains` and
`ignoreSitemap` were decoded then silently dropped:

- `includeSubdomains:false` → results filtered to host±www (`SameOrWWW`)
- `ignoreSitemap:true` → new `fetch.MapLinks` — harvests same-host
  links from live pages, 2 levels, 40-fetch politeness budget,
  asset extensions filtered (svg/png/js/woff/pkg/msi…; pdf/xml kept —
  maps legitimately list papers and feeds)

CLI parity: `webx map --search` / `--subdomains` / `--ignore-sitemap`.
(`internal/serve/firecrawl.go`, `web/fetch/links.go`, `cmd/webx/mapx.go`)

### J2. `/v2/search` dead params → FIXED

`country`, `location`, `sources`, and `categories` were decoded and
ignored — Firecrawl SDK users got silently wrong results. Now wired:

- `location`/`country` → `Request.Location` (geo hint)
- `categories:["developer"]` → dev-provider restriction
- `sources` (string or `{type}` objects) → typed verticals; results
  bucket into `data.web/news/images` with `imageUrl` for images
- `includeDomains`/`excludeDomains` (FC camelCase) → domain filters
- `include_answer:true` → cited answer on the response

(`internal/serve/firecrawl.go`)

### J3. `--location <cc>` geo hint → NEW (Exa userLocation / FC location)

`Request.Location` → DDG `kl` region (was hardcoded `us-en`): `de`→
`de-de`, `us`→`us-en`, `uk`→`uk-en` etc. Verified: `--location de` on
"oktoberfest münchen" returns real results through the DDG tier.
(`web/search/duckduckgo.go`, CLI flag)

### J4. `search --answer` → NEW (Tavily include_answer parity)

Cited extractive answer attached to the response — no LLM: picks the
best query-covered excerpt per top source (highlights → content →
snippet) with `[n]` source markers under a token budget. `--answer`
alone auto-enables highlights-depth scraping (snippet grounding is too
thin). JSON field `answer`; md mode prints `## Answer` first.
(`web/search/search.go` composeAnswer + bestExcerpt)

### J5. `--category developer` → NEW (FC categories parity)

Restricts the provider set to code/docs sources — `so, gh, grep, sg,
npm, crates, hn, reddit, index`. Applies AFTER depth resolution so
`--category developer --depth advanced` means "advanced dev search".
Verified: providers ran {hn, index, so} vs the full default set.
(`web/search/search.go` resolve)

### J6. include+exclude domains → honest error → FIXED

Firecrawl 400s when both are set; webx silently intersected (≈ empty
results). Now `errors.request` names the conflict in the response —
non-breaking but visible in JSON + stderr.
(`web/search/search.go`)

### Also fixed

- Stale `Rerank` doc comment ("needs Scrape" — false since snippet
  rerank shipped).
- `map` CLI `--search` filter + host-boundary filter — previously
  absent while the API advertised them.

### Tests added

- `SameOrWWW`, `isPagePath` (asset ext table incl. doc exceptions)
- `resolve` dev-category restriction
- mutual-exclusion `errors.request`
- `composeAnswer` best-excerpt + citation marker selection

`go build ./...`, `go vet ./...`, `go test ./...` — all green.
Verified live: `/v2/map` 403 on metadata IP; `/v2/search` with
`location`+`includeDomains`+`include_answer` returns answer + filtered
results; `map --ignore-sitemap` emits pages only (no .svg/.pkg assets).

### Remaining gaps (deliberate)

- Exa `outputSchema` — our `research --schema` covers synthesis;
  wiring it onto search is a follow-up.
- Exa `additionalQueries` expansion — needs LLM or synonym table;
  deferred (medium value vs complexity).
- Firecrawl `categories:research/pdf` — no academic provider exists
  (arxiv/scholar) — a provider feature, not a flag.
- Exa entities/costDollars — proprietary enrichment + billing shape;
  metering already exists server-side.

---

## Hard-site stress pass — 2026-10-04 (non-Go targets, real-world difficulty)

Tested the new features against genuinely difficult sites rather than
go.dev/example.com: SPAs, delayed-injection pages, bot walls, heavy
non-tech pages. Found and fixed 6 real issues.

### K1. `WaitMs` / `wait_ms` / `--wait` / `X-Wait-Ms` → NEW

`quotes.toscrape.com/js-delayed` injects content **10s after load** —
every tier captured the empty shell. A numeric post-load delay existed
only on the remote-render path. Now: `fetch.FetchRequest.WaitMs` →
prepends `sleep:N` action + implies render (same "implies render" class
as `wantA11y`). Wired through `/scrape` (`wait_ms`), `--wait`, reader
`X-Wait-Ms`, and FC `waitFor` now maps to the field instead of hand-
rolled action string. **Verified: 12s wait → full quote extraction.**

### K2. `NeedsRender` missed custom-element shells → FIXED

Reddit's logged-out page is 8.4KB of `<shreddit-*>` markup — no
`id="root"`/`__next` marker, 5.6% script bytes, extracted 6 chars.
All three signals missed → auto-render never fired. New signal 4:
`extractedLen < 60 && rawHTML > 3000` (Lit/web-component shells produce
~zero text on a non-trivial page). **Verified: reddit.com/r/programming
→ chrome tier, 15.4K chars, real post titles.**

### K3. Amazon interstitial invisible to challenge detection → FIXED

Amazon's "Click the button below to continue shopping" wall (HTTP 200,
326 chars) matched no challenge pattern → silently returned as content.
Added to `cfChallengeRe`. Chrome can't beat it without stealth/session,
so it now honestly reports `needs_render: true` instead of serving the
interstitial as if it were the product page.

### K4. Search weak-OR precision → FIXED (the biggest find)

`search "best time to visit iceland northern lights"` ranked two htmx
blog posts #1-2 at score 1.0/0.83 — the local index's OR-widening kept
hits matching just 2/6 content terms, and fusion normalized them to the
top. Three-layer fix:

- `filterWeakOR` minTerms scales: ≥5-term queries need 3 matches
- **long-term requirement**: hits must match ≥1 term of ≥7 chars —
  English content words are rarely that long; "french"+"end" on a
  revolution query dies, "northern lights" hits live
- `queryTerms` drops stopwords (exported `index.IsStopword`) so
  coverage scoring stops crediting "to"/"time"

**Verified: fresh query → 4 real Iceland results at top; `query` on the
index correctly returns nothing for out-of-corpus queries.**

### K5. Answer/highlights excerpted sidebar junk → FIXED

`--answer` picked SO sidebar blocks ("Terrorist attack in Nice…",
`-----` separators) as cited excerpts — bestExcerpt/blockScore had no
quality gate. New `soupBlock` (dash-runs, links-only blocks) gates
extractHighlights AND bestExcerpt. Verified: cited excerpt now comes
from the Wikipedia prose.

### K6. Request validation ran *after* cache read → FIXED

`domains`+`exclude_domains` together returned a clean cached response —
replay skipped the mutual-exclusion check (cached blobs store only
Results). Validation now runs before the cache lookup; a replay still
flags `errors.request`. Caught by a test that only failed on its second
run — exactly the kind of bug a suite green-once misses.

### K7. `search --fresh` → NEW

No cache bypass existed — agents re-checking a query after re-indexing
got 10-min-stale results with no escape hatch. `Fresh` skips the read
but still writes (so it refreshes the cache). CLI `--fresh`, API
`fresh:true`.

### Honest failures observed (correct behavior, not bugs)

- **linkedin.com**: status 999 surfaced as `http_status` error
- **nytimes.com**: DataDome blocked Chrome — 11 chars, honest
- **indeed.com**: chrome rendered but thin (466 chars) + hidden-text warning
- **uniswap**: Lightpanda partially rendered (swap UI text extracted);
  noscript text leaks into markdown — minor
- Amazon wall honestly flagged `needs_render` rather than fake content

### Extraction quality measured

Wikipedia "French Revolution": 2872-line page → 719 non-blank lines,
**3% link-soup remnants, zero `\[N\]` footnote artifacts**, clean prose
+ headings. GitHub repo pages: README extracted cleanly. BBC front
page: nav links are legitimately the content (link-index page).

### Tests added

- `TestQueryWeakORRequiresLongTerm` (index)
- `TestSoupBlock` (5-case table)
- Mutual-exclusion test now passes on repeat (cache-order regression)

`go build`, `go vet`, `go test ./...` — all green.

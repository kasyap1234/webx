# webx

**The web toolkit for AI coding agents** — scrape, search, map, crawl, index, extract, diff, and serve. Single static binary, MIT licensed, zero required services.

The self-hosted alternative to Firecrawl/Exa/Tavily/TinyFish: the important parts (metasearch fusion, FTS5 indexing, adaptive crawling, TLS fingerprinting) are built in, not wrapped. One binary replaces a scraping API bill, a search API bill, and a browser-infra bill.

![webx demo — scrape, search, doctor](docs/assets/demo.gif)

## Install

```bash
go install github.com/kasyap1234/webx/cmd/webx@latest   # from source
brew install kasyap1234/tap/webx                      # macOS/Linux formula
docker pull ghcr.io/kasyap1234/webx:latest            # webx + webxd image
```

or grab a binary from the [releases page](../../releases) — linux/darwin/windows, amd64+arm64.

## Quickstart

```bash
webx search "concurrent map writes golang panic"      # fused metasearch
webx scrape https://go.dev/doc/effective_go           # clean markdown
webx ask "how does htmx hx-swap work"                 # search + read + cite
webx doctor                                           # provider health check
```

## Commands

| Command | What it does |
|---|---|
| `scrape <url>` | readability→trafilatura→full-page cascade → clean markdown + metadata. `--browser` (Chrome TLS fingerprint), `--render`/`--auto-render` (real Chrome via go-rod for JS pages), `--actions` (browser steps — `eval:<js>` returns values in `action_returns[]`), `--screenshot`, `--proxy`, `--session`, `--raw`, `--wait-for`, `--scrolls`, `--fit <query>` (BM25-filtered markdown — the token-saver), `--include/--exclude <sel>` (CSS scoping), `--summary` (LLM digest), `--links`/`--images`/`--link-summary`, `--max-tokens`. Render depth: `--stealth` (fingerprint patches), `--profile <name>` (persistent logins in `~/.webx/profiles`), `--block-ads`/`--text-mode` (request interception), `--mobile`, `--locale`, `--tz`, `--network` (XHR log), `--console`, `--pdf`/`--mhtml`, `--insecure`, `--branding` (logo/colors/icons — deterministic), `--page-session <name>` (reusable live tab for multi-step flows — empty URL acts on the open page), `--pierce` (flatten shadow DOM + same-origin iframes), `--scroll-sel`/`--scroll-by` (virtualized containers), `--a11y` (accessibility snapshot + `@eN` refs → `click:@e3`/`fill:@e2=x`/`get:@e1`/`hover:@e3` ref-actions with occlusion check — "covered by div#modal" instead of a silent miss), `--engine light` (Lightpanda via CDP — ~16x lighter than Chrome, honest `tier_used`+warnings), `--chunks` (RAG-ready heading-path segments + `est_tokens`), `--agent-ready` (llms.txt/AI-bot robots/WebMCP/**api-catalog** probe → `agent_ready{}`). Always-on: `Accept: text/markdown` negotiation — edge-served markdown skips extraction (`extractor: edge-markdown`, ~80% fewer tokens on Cloudflare/Vercel zones). `act:<english>` actions resolve elements via `WEBX_LLM_*` and cache selectors for replay. `--transcript` pulls YouTube captions straight from InnerTube (no page fetch, `--locale` picks the language); `--redact-pii` masks emails/phones/SSN-ish strings. Web Bot Auth: `webx botkey` + `WEBX_BOT_*` signs every request (RFC 9421) — cryptographic identity, the anti-stealth |
| `search <q>` | Metasearch over 15 providers — ddg, hn, so, wiki, index, gh, reddit, sg, npm, crates, searxng, brave, mojeek, kagi, grep — fused by weighted RRF + bm25 blending + domain priors. Filters: `--site/--domains/--exclude-domains`, `--after/--before`, `--lang`, `--topic`, `--exact`. `--depth fast\|basic\|advanced` picks the result tier (Tavily `search_depth`); `--sources web,news,images` selects verticals (searxng categories); `--subpages N --subpage-target a,b` also reads matching subpages per top result (Exa `subpages`). `--scrape` reads the top results inline (bounded + per-page timeouts), `--highlights` keeps only query-relevant excerpts, `--rerank` re-sorts by content — semantic cosine when `WEBX_EMBED_MODEL` is set, lexical otherwise; `depth:advanced` reranks automatically. Near-duplicate results drop via title + simhash (`deduped` count in output); `score` is normalized 0–1 |
| `index <domain>` | Crawl → local SQLite FTS5 index (sitemap or BFS). `--from-cc latest` bootstraps from Common Crawl with zero requests to the host. `--stale` skips fresh pages, `--respect-robots` honors robots.txt, `--include-paths/--exclude-paths <regex>` scope to URL paths, `--concurrency N` fetches in parallel. `--collection <name>` writes a named corpus (`~/.webx/index-<name>.db`); `--gc --older-than 90d --vacuum` garbage-collects stale pages + orphan embeddings. With `WEBX_EMBED_MODEL`, pages embed **per-chunk** (heading-path segments) — `WEBX_INDEX_EMBED=page` restores whole-page vectors |
| `seed` | Index a curated list of ~25 docs domains |
| `query <q>` | Index-only search (offline, private corpus). `--semantic` fuses FTS5 bm25 with chunk-level embedding cosine via RRF — Exa-style neural search on your own docs, with best-chunk snippets (needs `WEBX_EMBED_MODEL`; Ollama works out of the box). `--collection <name>` queries a named corpus |
| `map <domain>` | Sitemap URL discovery |
| `crawl <url> <goal>` | Adaptive crawl — scores pages toward the goal, follows best links first, stops early when found. `--semantic` blends embedding cosine into relevance (focused/topic crawling; needs `WEBX_EMBED_MODEL`). `--concurrency N` fetches frontier waves in parallel with per-host rate limiting (default `WEBX_CRAWL_CONCURRENCY`=4). Content-hash dedup drops identical pages (`deduped` in output). `--respect-robots` honors robots.txt, `--include-paths/--exclude-paths <regex>` filter the frontier |
| `ask <q>` | Search → scrape → citation-numbered excerpts under a token budget. `--llm` synthesizes a cited answer when `WEBX_LLM_*` is configured |
| `extract <url>` | Page → schema'd JSON. `--css '{"title":"h1","links":"a[]@href"}'` is deterministic selector extraction (zero LLM); `--type product\|article\|job\|event\|faq` reads schema.org/JSON-LD entities (zero LLM); default path is LLM extraction (`WEBX_LLM_*`); `--render` for JS pages |
| `diff <url>` | Line-diff live page vs indexed version — sends `If-Modified-Since` so unchanged pages answer with a cheap 304. `--update-index` |
| `watch <url>` | Monitor a page for changes — conditional-GET polls against the index, reports `+added −removed` lines. `--semantic` asks the LLM whether a diff is *meaningful* (ignores timestamp/template churn). `--every`, `--once` (cron-friendly), `--exec <cmd>` runs on change with `WEBX_CHANGED_URL`/`WEBX_ADDED`/`WEBX_REMOVED` env |
| `verify <claim>` | Fact-check a claim against live web sources → `supported\|refuted\|unclear` + confidence + cited sources (grounding — Jina `g.jina.ai` equivalent; honestly reports `unavailable` without an LLM) |
| `research <question>` | Deep-research pipeline — search → read sources → cited markdown report. `--schema '<json-schema>'` returns the report as matching JSON (Tavily `output_schema`). LLM-synthesized when `WEBX_LLM_*` is configured; falls back to ranked excerpts. `--sources N` |
| `similar <url>` | Find pages similar to the given page via term-signature match — local index by default, `--web` searches live providers (source URL excluded). `--limit` |
| `llms <domain>` | Emit a whole-site `llms.txt` from the local index — grouped by path section with one-line blurbs (Mintlify hosts this as a paid feature; here it's a JOIN). `--limit` |
| `eval` | Search quality: 30-query dev set → hit rate + MRR, per-kind breakdown. `--extract` scores content extraction quality |
| `doctor` | Probe every provider: liveness, latency, result counts, config errors |
| `batch <file>` | Scrape a file of URLs concurrently (ndjson with `-f json`); `--api` turns it into a durable server-side job |
| `jobs` / `cancel` | List/inspect/cancel async jobs — local jobs.db or `--api` remote. `jobs --gc --older-than 7d` sweeps finished jobs; `webx serve`/`webxd` do it hourly via `WEBX_JOB_TTL` |
| `serve` | HTTP API — `POST /scrape /search /map /extract /research /verify /similar /wayback`, **async `POST /crawl /batch/scrape` → `GET /crawl/{id}` → `POST /crawl/{id}/cancel`**, SSE progress at `GET /crawl/{id}/events`, hosted snapshots at `GET /pages/md?url=`, `webhook_url` on jobs, `GET /doctor /jobs /health`. **Tenancy**: `POST /keys` issues per-user keys (sha256 at rest, per-key `rpm`/`concurrency`/`monthly_units`), `GET /usage` meters credits per key/day/endpoint, `POST /schedules` runs cron recurring crawls with webhooks, `GET /audit` trails requests. `WEBX_MAX_CONCURRENCY` caps global in-flight. License-gated (`webx license`, `WEBX_LICENSE`): >3 keys, admin roles, audit, `zdr` no-persistence mode. `/scrape` accepts `formats[]` (markdown/html/links/images/screenshot/pdf/mhtml/summary/branding/chunks/a11y/agent_ready), `max_age`, `change_tracking`, `include/exclude` selectors, `fit`, and all render options (`stealth`, `profile`, `block_ads`, `text_mode`, `mobile`, `locale`, `timezone`, `capture_network`, `capture_console`, `skip_tls_verification`, `page_session`, `pierce_dom`, `scroll_selector`, `scroll_by`, `branding` format). Per-IP rate limit via `WEBX_RATE_RPM`. `Idempotency-Key` header on `/crawl` + `/batch/scrape` replays to the original job instead of duplicating it. New: `POST /agent` (goal-only extraction — find the pages, extract what's asked), `GET /crawl/{id}/errors` + `POST /crawl/{id}/retry` (requeue failed pages as a batch job), `citation_check` on `/answer` flags `[n]` markers whose claims aren't in the cited source, `GET /crawl/{id}?limit&offset` pagination, `async:true` on `/extract` (incl. `example.com/*` wildcards) + `/research`, `webhook_secret` HMAC-SHA256 signing (`X-Webx-Signature`) with retries, `GET /ready /metrics /version` (health/metrics/build), `POST /mcp` streamable HTTP transport, search `depth/sources/subpages/collection`, scrape `screenshot_options{full_page,quality,viewport}` + `formats:["transcript","redact_pii"]` + `lang`. Hardening: request-ID + panic-guard middleware, 1MB JSON body cap, gzip responses, opt-in CORS (`WEBX_CORS_ORIGINS`), `log/slog` structured logging. **SSRF guard on by default** — private/loopback/metadata targets refused (`WEBX_BLOCK_PRIVATE`, `WEBX_ALLOW_DOMAINS`, `WEBX_DENY_DOMAINS`). **Firecrawl-compatible `/v1/*` + `/v2/*` routes** (`scrape /crawl /search /map /batch/scrape /extract /agent`) — Firecrawl SDKs work against a self-hosted webx. `GET /openapi.json` serves the embedded OpenAPI 3 spec |
| `mcp` | MCP server — stdio (`webx mcp`) or streamable HTTP at `POST /mcp` on `serve`/`webxd` (`claude mcp add --transport http`). 17 tools: search/scrape/ask/query/map/doctor/crawl/extract/diff/research/verify/watch/similar/wayback/llms/answer/batch |
| `skill` | Emit a `SKILL.md` teaching agent harnesses when to use webx — `webx skill > .devin/skills/webx/SKILL.md` |
| `install <engine>` | Install a render engine — `chrome` (Chrome for Testing via rod's downloader) or `lightpanda` (Zig engine → `~/.webx/bin`, the `--engine light` backend) |
| `botkey` | Generate a Web Bot Auth ed25519 key + the JWKS to self-host — sign requests cryptographically instead of fingerprint-guessing (draft-meunier-webbotauth-httpsig-protocol) |
| `license` | `check` verifies a license file; `install <file\|->` validates + parks it at `~/.webx/license.json` (picked up automatically by `serve`/`webxd`); `status` shows the active tier. Maintainer side: `keygen` makes the signing keypair, `gen` signs customer files (`--tier pro\|enterprise --days N`) — `webxl` automates this from Polar/Stripe webhooks |
| `wayback <url>` | Wayback Machine: list captures (`--from`/`--to`), or fetch the nearest snapshot `--at <ts>` and extract it like a live page — dead-link recovery (`tier_used: wayback`, stale warning) |
| `cdx <pattern>` | Common Crawl CDXJ index — enumerate a domain's known URLs without touching the host (`--crawl <collection>`, `--limit`) |
| `archive <url>...` | Write raw fetches as WARC/1.0 (ISO 28500) response records to `-o file.warc` — replays in replayweb.page/pywb |

## Environment

| Var | Effect |
|---|---|
| `WEBX_SEARXNG_URL` | SearXNG instance(s), comma-separated for failover — no API key needed, but the instance must enable `formats: [html, json]` |
| `WEBX_BRAVE_API_KEY` | Enables Brave provider |
| `WEBX_MOJEEK_API_KEY` | Enables Mojeek provider (independent web index) |
| `WEBX_KAGI_API_TOKEN` | Enables Kagi provider |
| `WEBX_GITHUB_TOKEN` | GitHub API auth |
| `WEBX_INDEX_DB` | Index path (default `~/.webx/index.db`) |
| `WEBX_LLM_BASE` / `WEBX_LLM_MODEL` / `WEBX_LLM_KEY` | Extract backend (default Ollama at `localhost:11434/v1`) |
| `WEBX_RENDER_URL` | Remote render service URL — delegates `--render`/`--auto-render` so the CLI needs no Chrome |
| `WEBX_CHROME_BIN` | Chrome/Chromium binary path override for local rendering |
| `WEBX_PROXY` | HTTP proxy for fetches + Chrome renders |
| `WEBX_API_KEY` | Bootstrap admin key for `serve`/`webxd` — all routes except `/health` require `Authorization: Bearer <key>`; `--api` clients send it automatically. Issue tenant keys via `POST /keys` |
| `WEBX_MAX_CONCURRENCY` | Global in-flight request cap on `serve`/`webxd` — over-limit → 429 + `Retry-After` |
| `WEBX_LICENSE` | Signed license file (path or JSON) — unlocks >3 API keys, admin roles, `GET /audit`, `zdr` mode. Default path `~/.webx/license.json` when unset (what `webx license install` writes). `WEBX_LICENSE_PUBKEY` overrides the verifier for dev |
| `WEBX_RATE_RPM` | Per-IP request cap on `serve`/`webxd` (default 240/min) |
| `WEBX_BLOCK_PRIVATE` | SSRF guard — refuse private/loopback/metadata targets (default ON; `0` opts out for intranet deploys) |
| `WEBX_ALLOW_DOMAINS` / `WEBX_DENY_DOMAINS` | Comma-separated domain policy on every outbound target (subdomains included) |
| `WEBX_CDP_URL` | Attach renders to an existing Chrome via CDP websocket (browserless/steel-style remote browser) |
| `WEBX_RENDER_ENGINE` | `light` — default render engine becomes Lightpanda (`--engine light` per-request equivalent) |
| `WEBX_LIGHTPANDA_URL` | Lightpanda CDP endpoint (`http://host:9222` or `ws://…`) — skips auto-detect/spawn |
| `WEBX_EMBED_MODEL` / `WEBX_EMBED_BASE` / `WEBX_EMBED_KEY` | Embedding endpoint for `query --semantic` + auto-embed on index — OpenAI-compatible `/v1/embeddings` (default base `localhost:11434/v1`, Ollama) |
| `WEBX_INDEX_EMBED` | `chunk` (default) embeds per heading-path segment for long-doc recall; `page` restores one vector per page |
| `WEBX_CRAWL_CONCURRENCY` | Parallel page fetches for crawl/index/batch jobs (default 4; per-host rate limit still applies) |
| `WEBX_WEBHOOK_SECRET` | Fallback HMAC key for job webhooks when the request doesn't carry `webhook_secret` — signs `X-Webx-Signature: sha256=…` with 3-attempt retry |
| `WEBX_CORS_ORIGINS` | Comma-separated allowed origins — enables CORS middleware on `serve`/`webxd` (off by default) |
| `WEBX_JOB_TTL` | Retention horizon for finished jobs — the scheduler sweeps `done`/`failed`/`cancelled` jobs + their pages hourly (default `168h`; `0` disables) |
| `WEBX_BOT_KEY` / `WEBX_BOT_DIRECTORY` | Web Bot Auth signing — key file from `webx botkey` + the https URL where your JWKS directory is served |

## Server deployment (with SearXNG)

```bash
docker compose up -d    # webx serve + searxng, prewired via WEBX_SEARXNG_URL
curl -X POST localhost:8080/search -d '{"query":"golang http client"}'
```

`webx serve` queues crawl jobs into `~/.webx/jobs.db` (`--jobs-db`) and runs
in-process workers (`--workers`). For the durable edition there's `webxd` —
same routes, multi-worker-safe job claims via Postgres `SKIP LOCKED`:

```bash
webxd --store pg --dsn postgres://user:pass@host/webx --workers 4
webxd --store sqlite --dsn /var/lib/webx/jobs.db   # single-node durable
```

The lite CLI doubles as a remote client: `webx --api http://host:8080
scrape|search|crawl …` runs server-side — crawl submits the job and polls
until done.

Rendering in Docker: the base image ships Chrome-free (tiers 1–2 only).
Two compose profiles add tier-3 rendering — `render` builds webx from
`Dockerfile.render` (Chrome for Testing baked in, all-in-one), `browserless`
runs `browserless/chromium` as a sidecar (`shm_size: 2g`) that webx attaches
to over `WEBX_CDP_URL`:

```bash
docker compose --profile render up -d       # Chrome inside the webx image
docker compose --profile browserless up -d  # Chrome as a separate service
```

## SDKs

| language | package | notes |
|---|---|---|
| **Python** | `sdk/python` → `pip install webx-sdk` (`from webx_sdk import WebX`) | zero-dep (stdlib urllib); `WebX(base_url, api_key)` — `scrape / search / crawl / extract / map / research / verify / wayback / batch_scrape` + tenancy (`create_key / usage / schedule`) |
| **JS/TS** | `sdk/javascript` → `npm install webx-sdk` | zero-dep ESM, Node 18+, full `.d.ts`; same method surface, camelCase opts |
| **Go** | `import "github.com/kasyap1234/webx/client"` | the same typed client the CLI uses for `--api` mode |

All three share the API's error contract: errors carry `.code` (`payment_required`, `quota_exceeded`, `concurrency_limit`, …) and x402/AP2 `.payment` metadata. Firecrawl SDKs also work unchanged — point them at `webx serve` via the `/v2/*` routes.

**Other languages**: `scripts/gen-sdks.sh rust ruby java` generates openapi-generator clients from `internal/serve/openapi.json` into `sdk/gen/<lang>` (dockerized generator — no Java needed; falls back to npx+java). Any of openapi-generator's ~50 targets works; CI validates the spec stays generator-clean on every change.

## Quality (measured, not claimed)

- `webx eval` — search quality on real corpora: hand-picked dev set (30 queries) → **80% hit @10, MRR 0.466**; generated corpus (`--gen`, ORCAS Bing clicks + SE + GitHub). **Measured 2026-10-03, 619 dev-intent queries, free API providers only**: 20.5% hit [95% CI 17.5–23.9], MRR 0.178 — HN covered most hits; SO/Reddit/grep.app hit their anonymous quotas mid-run (the report now surfaces per-provider error counts, which is what exposed it). General-web queries need a general-web provider — `WEBX_SEARXNG_URL` or `BRAVE_API_KEY`; that's why the compose searxng exists.
- `webx eval --extract` — golden-page extraction quality → **100% marker coverage, 100% code recall**
- `webx eval --cite` — citation-verifier accuracy on labeled claim/evidence pairs → **100% precision** (numeric + entity hard-requirements; recall-limited by design — paraphrases fail closed, never false-verified)
- `webx eval --gen` — builds a **~1300-query corpus from real data**: reservoir-sampled ORCAS Bing click logs (expected host = what a real user clicked), Stack Exchange top-voted titles, GitHub repo descriptions. `--gen-out file.yaml` saves it for `--set` reuse; `--workers N` parallelizes the run; hit-rate reports a Wilson 95% CI so sample size is honest
- `webx eval --vs exa,tavily,firecrawl` — same corpus scored identically through each engine (`EXA_API_KEY`/`TAVILY_API_KEY`/`FIRECRAWL_API_KEY`; missing keys skip honestly). `--extract --vs` compares marker coverage; `--json` for CI. `FIRECRAWL_BASE_URL` also points at a self-hosted Firecrawl — or a webx server, which doubles as a `/v1`-compat regression check
- `go run ./bench` — comparative scrape benchmark vs external providers (jina-reader, firecrawl, tavily — key-gated providers skip gracefully). Two corpora, measured 2026-10-03:
  - **Static corpus** (15 URLs): webx 15/15 @ p50 996ms · jina 14/15 @ p50 516ms (shared edge cache) · webx markdown **2.8× tighter** (249KB vs 700KB)
  - **JS corpus** (15 hard targets — SPAs, app walls, bot-protected): webx **15/15** @ p50 1572ms (auto_render → chrome on bot-walls) · jina 14/15 · raw-http 12/15
  - Methodology, per-URL rows, honest caveats: [`bench/RESULTS.md`](bench/RESULTS.md). As-measured-on-the-day — re-run before quoting.

## Design notes

- **Search**: providers implement a `Provider` interface + optional `Rewrite` hook; results fuse via weighted RRF (k=60) blended with provider-native bm25, shaped by a query classifier (errorish/identifier/howto/concept/fresh), title-match bonus, freshness decay, and a hand-tuned domain-prior table.
- **Index**: SQLite FTS5 (pure-Go `modernc.org/sqlite` — static binary preserved), porter tokenizer, title/headings/body weighted columns, bm25 scoring, stopword-aware widening.
- **Fetch — three escalating tiers**, honestly reported as `tier_used`:
  1. `http` — plain fetch (default)
  2. `tls` — `--browser`: Chrome ClientHello via uTLS, for fingerprint-check sites
  3. `chrome` — `--render`: real headless Chrome via go-rod, for JS-rendered/SPA pages. `--auto-render` escalates *only* when the HTTP pass detects a JS-shell (SPA markers, script-dominated body, or a bot-challenge page) — docs sites never pay the Chrome cost. `needs_render: true` in JSON output signals the condition even without escalating. `--wait-for` (CSS selector) and `--scrolls` (virtual-scroll passes) control the render.
  Remote render: `WEBX_RENDER_URL` delegates rendering to a service so the binary can stay Chrome-free; `WEBX_CDP_URL` attaches to an already-running Chrome (browserless/steel-style).
  **`--actions`** scripts steps before extraction: `click:.accept | wait:.quote | type:#q=term | press:enter | sleep:400 | scroll | eval:<js> | screenshot`. `eval` return values land in `action_returns[]` (Firecrawl `javascriptReturns`). `--screenshot <path>` captures a PNG. **Render hardening**: `--stealth` patches webdriver/plugins/WebGL fingerprints, `--profile` keeps a persistent Chrome profile under `~/.webx/profiles/` (logins survive runs), `--block-ads`/`--text-mode` intercept tracker and media requests, `--mobile`/`--locale`/`--tz` emulate devices and regions, `--network` records the XHR log (`network[]`), `--console` captures `console.*` (`console[]`), `--pdf`/`--mhtml` archive the rendered page. `--proxy`/`WEBX_PROXY` routes HTTP fetches (and Chrome) through a proxy.
- **Content trust**: every scrape runs an injection scan — `warnings[]` in JSON flags hidden text (`display:none`, off-screen, font-size:0), instruction-hijack phrasing ("ignore all previous instructions"), invisible/zero-width characters, and suspicious HTML comments. Page content is untrusted input for agents; webx is honest about what's in it.
- **Jobs**: `POST /crawl`, `POST /batch/scrape`, and `async:true` on `/extract` (wildcards map-then-extract), `/research`, `/agent` queue into SQLite (lite) or Postgres `SKIP LOCKED` (webxd) — durable per-page progress, results land in `job.result`. `webhook_url` fires on completion, HMAC-SHA256-signed when `webhook_secret`/`WEBX_WEBHOOK_SECRET` is set (`X-Webx-Signature: sha256=…`, 3-attempt backoff). `GET /crawl/{id}` paginates (`limit`/`offset`), `GET /crawl/{id}/errors` lists failed pages, `POST /crawl/{id}/cancel` stops at page boundaries keeping what landed.
- **Crawl**: relevance-scored best-first BFS — the budget goes where the goal is. `--respect-robots` enforces robots.txt on index/seed/crawl.
- **Fit markdown**: `--fit <query>` scores markdown blocks with BM25 (k1=1.2, b=0.75, heading bonus) and returns only the relevant ones in document order — same idea as crawl4ai's fit_markdown, without the LLM cost.
- **Deterministic extraction**: `extract --css '{"field":"sel","list":"sel[]@attr","nested":{"selector":"sel[]","fields":{...}}}'` maps selectors to schema'd JSON — zero LLM, works on rendered pages. `--include`/`--exclude` scope the DOM *before* readability runs (Jina target-selector style). PDFs extract text via a pure-Go parser.
- **Research**: `research` fans the question out across providers, reads the top sources under a token budget, and either synthesizes a cited report via `WEBX_LLM_*` or falls back to ranked excerpts — source fetches report failures honestly instead of fabricating.
- **Change tracking & caching**: `/scrape` honors `max_age` (ms — fresh index snapshot beats a refetch) and `change_tracking` (diffs vs the indexed copy: `new|same|changed` + `previous_scrape_at`). `diff` sends conditional GETs. `watch` polls on an interval and fires `--exec` on change. `GET /pages/md?url=` serves the stored snapshot back (Olostep `markdown_hosted_url`). `GET /crawl/{id}/events` streams job progress as SSE.
- **Structured data**: `--images`/`--link-summary` append Jina-style reference blocks; `jsonld[]` exposes schema.org entities raw; `extract --type` normalizes products/articles/jobs/events/faqs; base64 `data:` image URIs are stripped from markdown.
- **Safety**: `serve`/`webxd` resolve outbound targets and refuse private/loopback/link-local addresses (SSRF), honor `WEBX_ALLOW_DOMAINS`/`WEBX_DENY_DOMAINS`, and cap per-IP rate.
- **Firecrawl `/v2` compat**: `/v2/scrape`, `/v2/crawl`, `/v2/crawl/{id}`, `/v2/search`, `/v2/map`, `/v2/extract`, `/v2/agent` accept Firecrawl request shapes (`formats` — strings *and* objects like `{"type":"screenshot","fullPage":true,"quality":80,"viewport":{...}}` or `{"type":"json","schema":{...}}`, `includeTags`/`excludeTags`, `waitFor`, `actions`, `maxAge`, `includePaths`/`excludePaths`, `webhook`, `limit`) and answer with `{success, data}` envelopes — point a Firecrawl SDK at a self-hosted webx by changing the base URL. `POST /v2/agent` runs always-async (`{prompt, urls?, schema?, webhook?}` → `{id, statusUrl}`; poll `GET /v2/agent/{id}` → `{status, data, expiresAt}`).
- **Render pooling**: Chrome launches once per process and is reused across renders (page-per-request isolation); `Leakless` still reaps it on exit. Repeat renders drop the ~1-2s launch cost.
- **Auth**: `WEBX_API_KEY` turns the open dev server into an exposed API — bearer auth on everything except `/health`; `webx --api` picks it up from the env automatically.

## Pricing

webx is MIT open source — the code is free forever. You're buying **the
hosted version, support, and enterprise features**, not permission.

| | **Community** | **Pro** | **Enterprise** |
|---|---|---|---|
| | free | **$49/mo** per deployment | **contact** |
| Search / scrape / crawl / extract / agent / answer | ✔ | ✔ | ✔ |
| MCP server · SDKs · eval harness | ✔ | ✔ | ✔ |
| API keys | 3 | **unlimited** | unlimited |
| Key roles (RBAC) | — | ✔ | ✔ |
| Request audit log | — | ✔ | ✔ |
| Zero-data-retention mode (`zdr`) | — | — | ✔ |
| Priority job queue | — | — | ✔ |
| Support | community | email, 2 business days | dedicated, SLA |
| Self-hosted air-gap install | ✔ | ✔ | ✔ + onboarding |

Gates are enforced in-code — a 402 response body includes `"upgrade"` pointing
here. `GET /license` on any deployment reports its live tier.

**Buying a license**: self-serve checkout (Polar/Stripe) → the fulfillment
service (`webxl`) signs your license and emails it within seconds — renewals
re-issue automatically each billing cycle. Or open a
[license request](../../issues/new?template=license-request.yml) and get a
signed file + invoice within 1 business day. Either way:

```bash
webx license install webx-license.json   # verify + park at ~/.webx/license.json
webx license status                      # confirm the tier
```

`serve`/`webxd` pick it up with zero env config (`WEBX_LICENSE=<path|json>`
still works, and wins over the file). Operators reselling webx-hosted service
point their own paywall with `WEBX_UPGRADE_URL`.

**Hosted cloud**: `docker compose --profile cloud up -d` on a VPS gives a
durable, TLS-terminated deployment (`DOMAIN`, `WEBX_API_KEY`, `PG_PASSWORD`)
— that's also exactly what a paid hosted webx would be.

Honest note: the free tier is genuinely a product, not a crippleware demo —
metering, schedules, webhooks all work without a license. The gates are the
things that only matter once you're running it for a team.

## Comparisons

Honest, dated, reproducible — re-run `go run ./bench` / `webx eval --vs` before quoting.

- [webx vs Firecrawl](docs/vs-firecrawl.md) — features, self-host footprint, pricing model
- [webx vs Crawl4AI](docs/vs-crawl4ai.md) — library vs toolkit, Python vs single binary
- [webx vs search APIs (Exa/Tavily/Brave)](docs/vs-search-apis.md) — metasearch vs owned indexes

## License

MIT.

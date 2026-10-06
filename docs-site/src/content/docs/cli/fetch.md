---
title: Fetch commands
description: scrape, batch, map, cdx, wayback, archive — getting pages and URL lists.
---

## scrape

`webx scrape <url>` — readability→trafilatura→full-page cascade → clean markdown + metadata.

```bash
webx scrape https://go.dev/doc/effective_go
webx scrape https://spa.example.com --auto-render
webx scrape https://go.dev --fit "http server" --max-tokens 2000
```

### The token savers (agents, read these)

| Flag | Effect |
|---|---|
| `--fit <query>` | BM25-filtered markdown — keeps only query-relevant blocks |
| `--max-tokens N` | Hard output budget |
| `--chunks` | Heading-path segments with `est_tokens` — RAG-ready |
| `--include` / `--exclude <sel>` | CSS-scope the extraction |
| `--summary` | LLM digest (`WEBX_LLM_*`) |
| `--links` / `--images` / `--link-summary` | Link/image inventories |

### Content shaping

| Flag | Effect |
|---|---|
| `--raw` | Skip extraction, emit the raw fetch |
| `--transcript` | YouTube captions straight from InnerTube — no page fetch (`--locale` picks the language) |
| `--redact-pii` | Mask emails/phones/SSN-ish strings |
| `--agent-ready` | llms.txt/AI-robots/WebMCP/api-catalog probe → `agent_ready{}` |
| `--branding` | Deterministic logo/colors/icons extraction |
| `--pdf` / `--mhtml` / `--screenshot` | Alternate output formats |

### Browser & rendering

`--browser` (uTLS Chrome fingerprint) · `--render` / `--auto-render` · `--engine light` (Lightpanda) · `--stealth` · `--profile <name>` · `--proxy` · `--wait-for <sel>` · `--wait <ms>` (fixed delay — delayed JS injects; implies render) · `--scrolls` · `--mobile` · `--locale` · `--tz` · `--block-ads` · `--text-mode` · `--network` · `--console` · `--insecure` · `--pierce` · `--scroll-sel`/`--scroll-by` · `--page-session <name>` · `--a11y` (snapshot + `@eN` refs for `click:@e3`/`fill:@e2=x`/`get:@e1`/`hover:@e3` with occlusion check)

`--actions` runs pipe-separated steps before extraction: `click:.accept | scroll | screenshot`, `eval:<js>` (results in `action_returns[]`), `act:<english>` (LLM-resolved via `WEBX_LLM_*`, selectors cached for replay).

See [Fetch tiers & rendering](/guides/rendering/) for the escalation model.

## batch

`webx batch <file>` — scrape a file of URLs concurrently (one per line; ndjson with `-f json`).

```bash
webx batch urls.txt -f json          # local concurrency
webx batch urls.txt --api <server>   # durable server-side job with progress
```

## map

`webx map <domain>` — list a site's URLs from its sitemap. Pure discovery, no fetching.

```bash
webx map go.dev --search generics      # keep only matching URLs
webx map go.dev --subdomains           # include docs./blog. subdomains
webx map go.dev --ignore-sitemap       # no sitemap: harvest same-host links from live pages (bounded, pages-only)
```

## cdx

`webx cdx <pattern>` — Common Crawl CDXJ index. Enumerate a domain's known URLs **without touching the host**.

```bash
webx cdx "example.com/docs/*" --crawl CC-MAIN-2025-30 --limit 100
```

## wayback

`webx wayback <url>` — list Wayback Machine captures (`--from`/`--to`), or fetch the nearest snapshot with `--at <ts>` and extract it like a live page. Dead-link recovery: output reports `tier_used: wayback` plus a staleness warning.

## archive

`webx archive <url>...` — write raw fetches as **WARC/1.0** (ISO 28500) response records:

```bash
webx archive https://a.com https://b.com -o evidence.warc
```

Replays in replayweb.page/pywb — the compliance-friendly audit trail.

---
title: Fetch tiers & rendering
description: The three-tier fetch ladder — HTTP, uTLS fingerprint, real Chrome — and when each engages.
---

webx doesn't have a "render or don't" switch — it has a **ladder**. Each request climbs as far as it needs and honestly reports where it landed (`tier_used` in output).

## The three tiers

| Tier | Mechanism | Cost | Engages when |
|---|---|---|---|
| `http` | Plain HTTP/2 fetch → readability→trafilatura→full-page cascade | ~ms, no deps | Default. Most of the web |
| `browser` | uTLS with a Chrome TLS fingerprint — passes TLS-level bot checks | ~ms, no browser | `--browser` flag |
| `render` | Real Chrome via go-rod (or Lightpanda/remote CDP) | ~seconds, needs an engine | `--render`, `--auto-render` |

`--auto-render` is the sweet spot for mixed workloads: fetch HTTP first, escalate to render only when the markdown comes back thin (JS shells, bot walls). On the bench corpus it took JS-heavy pages from 12/15 to 15/15 successful extracts.

## Edge-served markdown (always on)

Every request sends `Accept: text/markdown`. Sites on Cloudflare/Vercel zones that negotiate markdown return it directly — extraction is skipped entirely (`extractor: edge-markdown`, ~80% fewer tokens). Nothing to configure.

## Choosing an engine

| Engine | Weight | Use for |
|---|---|---|
| Chrome for Testing | Full Chrome | Fidelity — real layouts, real JS, real bot resistance |
| Lightpanda | ~16× lighter | High-volume rendering where full Chrome is wasteful — `webx install lightpanda`, then `--engine light` or `WEBX_RENDER_ENGINE=light` |
| Remote CDP | Zero local browser | `WEBX_CDP_URL` attaches to browserless/steel-style fleets; `WEBX_RENDER_URL` delegates to a render service |

## Hardened pages

When a target fights back, escalate in order:

```bash
webx scrape <url> --browser               # TLS fingerprint
webx scrape <url> --render --stealth      # fingerprint patches in real Chrome
webx scrape <url> --render --profile me   # persistent login profile (~/.webx/profiles)
webx scrape <url> --render --proxy http://…
webx scrape <url> --render --mobile --locale de-DE --tz Europe/Berlin
webx scrape <url> --wayback-mode…          # dead page? see wayback/archive below
```

:::caution[Honest limit]
webx has stealth + uTLS but **no proxy fleet**. Targets behind residential-proxy-only walls (think `x.com`-class) can return thin payloads — for those, point `WEBX_CDP_URL` at a managed browser fleet or run through `--proxy`. See [vs Firecrawl](/comparisons/vs-firecrawl/) for the honest boundary.
:::

## Acting inside the page

`--actions` runs steps before extraction — the grammar is pipe-separated:

```bash
webx scrape <url> --render --actions "click:.accept | scroll | screenshot"
webx scrape <url> --render --a11y --actions "fill:@e2=q | click:@e3"   # @eN refs from the a11y snapshot
webx scrape <url> --render --actions "eval:document.title"           # returns values in action_returns[]
webx scrape <url> --render --actions "act:open the pricing dialog"   # LLM resolves the element (WEBX_LLM_*)
```

`--a11y` snapshots the accessibility tree first and gives you `@eN` element refs — `click:@e3`, `fill:@e2=x`, `get:@e1`, `hover:@e3` — with **occlusion checking** ("covered by div#modal" instead of a silent miss). `--page-session <name>` keeps a live tab open across invocations for multi-step flows (empty URL acts on the open page).

Other useful render flags: `--screenshot` (+`full_page`, `quality`, `viewport` via the API), `--pdf`/`--mhtml`, `--block-ads`, `--text-mode` (request interception), `--pierce` (flatten shadow DOM + same-origin iframes), `--scroll-sel`/`--scroll-by` (virtualized containers), `--network` (XHR log), `--console`.

## Recovery paths

- `webx wayback <url>` — list Wayback captures or fetch `--at <ts>` and extract a snapshot like a live page (`tier_used: wayback` + stale warning)
- `webx cdx <domain>` — enumerate a domain's known URLs from Common Crawl's index without touching the host
- `webx index <domain> --from-cc latest` — bootstrap the local index from Common Crawl with zero requests to the host
- `webx archive <url>...` — write raw fetches as WARC/1.0 (ISO 28500), replays in replayweb.page/pywb

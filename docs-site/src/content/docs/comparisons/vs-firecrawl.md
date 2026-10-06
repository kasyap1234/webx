---
title: vs Firecrawl
description: Hosted SaaS vs single static binary you own — honest trade-offs, pricing, and migration path.
---

*Last reviewed 2026-10-04. Competitor features/pricing change fast — verify
against [firecrawl.dev/pricing](https://www.firecrawl.dev/pricing) before quoting.
Re-run `go run ./bench` for fresh numbers.*

## TL;DR

Firecrawl is a **hosted service** with a large team and polished DX. webx is a
**single static binary you own** — same job description (page→markdown, crawl,
search, extract, agent), no required services, MIT, and a `/v2` compatibility
layer so Firecrawl SDKs work unchanged against a webx server.

| | webx | Firecrawl |
|---|---|---|
| Model | Open source (MIT) + optional license | Hosted SaaS; self-host is AGPL-3.0 |
| Deploy | 1 binary, SQLite (Postgres optional for `webxd`) | Their cloud; self-host needs Node + Playwright + Redis + workers |
| Price | Free; Pro $49/mo per deployment; Enterprise contact | $16 Hobby / $83 Standard / $333 Growth / $599 Scale per month, credit-based; extract bills tokens separately |
| Free tier | Unlimited (your hardware) | 500 searches **or** 1k pages/mo |
| Search | 15-provider metasearch + private index | Managed search credits |
| Extraction | CSS/JSON-LD deterministic, LLM via your endpoint (Ollama works) | Managed LLM extraction |
| Browser | Chrome via go-rod or Lightpanda, pooled; browserless/steel attach via `WEBX_CDP_URL` | Managed browser fleet |
| Compliance | ZDR mode, audit log, SSRF guard, air-gap install, WARC archive | Hosted; enterprise terms on higher tiers |

## Where Firecrawl wins — be honest

- **Managed scale.** Their browser fleet, proxy infra, and edge cache absorb
  the hardest targets (aggressive bot walls, residential-proxy-only sites).
  webx has stealth + uTLS but no proxy fleet — `x.com`/`amazon`-class targets
  can return thin payloads.
- **Zero ops.** No machine to run, no Chrome to keep alive.
- **Ecosystem.** Bigger community, more integrations, battle-tested at volume.

## Where webx wins

- **Cost shape.** Flat per-deployment vs per-credit. At 100k pages/mo,
  Firecrawl Standard is ~$83/mo plus extract-token spend; webx Pro is $49/mo
  per deployment with unlimited pages on your hardware, or $0 community.
- **Data never leaves.** Self-host by default; ZDR mode + audit log for
  regulated shops. AGPL-free embedding (MIT) means no copyleft questions.
- **Private corpus.** `webx index` + `webx query --semantic` is Exa-style
  neural search over *your* corpus — Firecrawl doesn't index your data for
  offline querying.
- **Offline/edge.** Runs on a laptop, a Lambda, a plane. SQLite FTS5 works
  with no network.
- **Token discipline.** `--fit` BM25 block filtering and `est_tokens` chunks
  are built for agent context budgets; measured ~2.8× tighter markdown than
  jina-reader on the bench corpus (see `bench/RESULTS.md`).
- **Migration path.** `/v2/*` routes accept Firecrawl request shapes —
  point a Firecrawl SDK at a webx base URL and it runs.

## When to pick which

- **Pick Firecrawl** if you want a fully managed service, need hard-site
  success rates today, and per-credit pricing fits your volume.
- **Pick webx** if you self-host anyway, have compliance/air-gap
  requirements, want flat pricing, need a private searchable index, or your
  agents run in sandboxes where one static binary beats a client SDK + API
  key + network egress.

## Migration (Firecrawl → webx)

```bash
docker compose up -d          # webx serve + searxng
# Firecrawl SDK:
#   before: Firecrawl(api_key="fc-...")
#   after:  Firecrawl(api_url="http://localhost:8080", api_key="webx_...")
```

`POST /v2/scrape /v2/crawl /v2/search /v2/map /v2/extract /v2/agent` accept
the same request shapes (`formats` incl. object forms, `includeTags`,
`waitFor`, `actions`, `maxAge`, `webhook`, `limit`).

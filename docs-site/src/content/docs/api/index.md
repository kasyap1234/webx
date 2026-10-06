---
title: Server overview
description: webx serve / webxd — routes, auth, tenancy, jobs, webhooks, and hardening defaults.
---

`webx serve` (dev) and `webxd` (production) expose the full toolkit over HTTP. An embedded **OpenAPI 3 spec** is served at `GET /openapi.json`.

## Core routes

| Route | What it does |
|---|---|
| `POST /scrape` | All scrape features — `formats[]` (markdown/html/links/images/screenshot/pdf/mhtml/summary/branding/chunks/a11y/agent_ready/transcript/redact_pii), `max_age`, `change_tracking`, `fit`, include/exclude selectors, every render option |
| `POST /search` | Metasearch — `depth`, `sources`, `subpages`, `collection` |
| `POST /map` | Sitemap discovery |
| `POST /extract` | CSS/JSON-LD/LLM extraction; `async:true` for jobs (incl. `example.com/*` wildcards) |
| `POST /research` | Deep research; `async:true` for jobs |
| `POST /verify` | Claim grounding |
| `POST /similar` | Near-neighbor match |
| `POST /wayback` | Wayback captures |
| `POST /agent` | Goal-only extraction — find the pages, extract what's asked |
| `POST /answer` | Cited answers; `citation_check` flags `[n]` markers whose claims aren't in the cited source |
| `GET /r/<url>` | Jina-style reader — clean markdown for any URL over plain GET. `Accept: application/json` for Document JSON; `X-Render`, `X-Engine`, `X-Token-Budget`, `X-Target-Selector`, `X-Remove-Selector` headers |
| `GET /pages/md?url=` | Hosted page snapshots |
| `POST /mcp` | Streamable HTTP MCP transport — all 17 tools |

## Async jobs

`POST /crawl` and `POST /batch/scrape` return job IDs:

- `GET /crawl/{id}` — status + results (`?limit&offset` pagination)
- `GET /crawl/{id}/events` — **SSE progress stream**
- `GET /crawl/{id}/errors` + `POST /crawl/{id}/retry` — requeue failed pages as a batch job
- `POST /crawl/{id}/cancel`
- `GET /jobs` — all jobs

`webhook_url` on a job POST delivers completion callbacks; `webhook_secret` gets HMAC-SHA256 signing (`X-Webx-Signature`) with retries. `Idempotency-Key` headers replay to the original job instead of duplicating it.

## Tenancy & metering

```bash
curl -X POST localhost:8080/keys -H "authorization: Bearer $WEBX_API_KEY" \
  -d '{"name":"agent-1","rpm":120,"monthly_units":5000}'
```

- `POST /keys` — issue per-tenant keys (sha256 at rest, per-key `rpm`/`concurrency`/`monthly_units`)
- `GET /usage` — meter credits per key/day/endpoint
- `POST /schedules` — cron recurring crawls with webhooks
- `GET /audit` — request trail (license-gated)
- Over-quota → `429`/`402` with `Retry-After` + `upgrade` metadata

## Ops & hardening

`GET /health` · `GET /ready` · `GET /metrics` · `GET /version` · `GET /doctor`

Defaults that stay on: **SSRF guard** (private/loopback/metadata refused — `WEBX_BLOCK_PRIVATE`), request-ID + panic-guard middleware, 1MB JSON body cap, gzip responses, `log/slog` structured logging. Opt-in: CORS (`WEBX_CORS_ORIGINS`), per-IP rate limits (`WEBX_RATE_RPM`), global concurrency cap (`WEBX_MAX_CONCURRENCY`).

---
title: Server & admin commands
description: serve, jobs, cancel, install, license — running webx as a service.
---

## serve

`webx serve` — the HTTP API on `:8080`. Same binary, same features, exposed as routes with tenancy, durable jobs, and Firecrawl compatibility. Full route surface: [HTTP API](/api/).

```bash
WEBX_API_KEY=webx_admin_secret webx serve
```

For production use `webxd` (Postgres-capable, worker pools): see [Self-hosting](/guides/self-hosting/).

### Reader endpoint — `GET /r/<url>`

Jina-style zero-setup reader: everything after `/r/` is the target URL, response is clean markdown.

```bash
curl http://localhost:8080/r/https://example.com
```

```text
Title: Example Domain

URL Source: https://example.com

Markdown Content:
This domain is for use in documentation examples …
```

- `Accept: application/json` returns the full Document JSON (`tier_used`, links, agent_ready) instead.
- `X-Render: 1` forces the browser tier; `X-Engine: chrome|light` picks the engine. Auto-render escalation is on by default.
- `X-Token-Budget: N` truncates the markdown to ~N tokens.
- `X-Target-Selector` / `X-Remove-Selector` (comma-separated CSS) scope the extraction.
- `X-Wait-Ms: N` sleeps N ms after render before extracting — pages that inject content on a timer (e.g. `quotes.toscrape.com/js-delayed` waits 10s).
- Same SSRF guard as the rest of the API — private/loopback targets are refused with 403.

## jobs

`webx jobs` — list async jobs (or `webx jobs <id>` to inspect). Local `jobs.db` by default; `--api` queries a remote server.

```bash
webx jobs --gc --older-than 7d      # sweep finished jobs
```

`serve`/`webxd` run the same sweep hourly via `WEBX_JOB_TTL` (default 168h).

## cancel

`webx cancel <job-id>` — cancel a queued/running job (or `POST /crawl/{id}/cancel` over HTTP).

## install

`webx install <engine>` — install a render engine:

| Engine | What it is |
|---|---|
| `chrome` | Chrome for Testing via rod's downloader |
| `lightpanda` | Lightpanda (Zig engine → `~/.webx/bin`) — the `--engine light` backend, ~16× lighter |

## license

`webx license` — license tooling for operators:

```bash
webx license keygen                                    # maintainer Ed25519 keypair
webx license gen --tier pro --days 365 --out lic.json  # sign a customer license
webx license check lic.json                            # verify a license file
```

`WEBX_LICENSE=<file-or-json>` on `serve`/`webxd` unlocks >3 API keys, RBAC roles, `GET /audit`, and ZDR mode. Details: [Licenses & tiers](/guides/license/).

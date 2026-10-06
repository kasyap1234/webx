---
title: Configuration
description: All webx environment variables — providers, LLM/embeddings, server, security, and storage.
---

webx is configured entirely through environment variables — everything works with none set.

## Search providers

| Var | Effect |
|---|---|
| `WEBX_SEARXNG_URL` | SearXNG instance(s), comma-separated for failover — no API key needed, but the instance must enable `formats: [html, json]` |
| `WEBX_BRAVE_API_KEY` | Enables the Brave provider |
| `WEBX_MOJEEK_API_KEY` | Enables Mojeek (independent web index) |
| `WEBX_KAGI_API_TOKEN` | Enables Kagi |
| `WEBX_GITHUB_TOKEN` | GitHub API auth for the `gh` provider |

:::tip[General-web search quality]
The free providers (ddg, hn, so, wiki, gh, reddit, npm, crates, grep) cover dev content well. For general-web recall, configure `WEBX_SEARXNG_URL` or `WEBX_BRAVE_API_KEY` — `webx doctor` shows which providers are live.
:::

## LLM & embeddings

| Var | Effect |
|---|---|
| `WEBX_LLM_BASE` / `WEBX_LLM_MODEL` / `WEBX_LLM_KEY` | OpenAI-compatible chat endpoint for `extract`, `ask --llm`, `research`, `verify`, `act:<english>` actions (default base `localhost:11434/v1` — Ollama) |
| `WEBX_LLM_MAX_TOKENS` | Generation cap per call (default `4096` — reasoning models spend thinking budget inside it; also bounds runaway output from misconfigured servers) |
| `WEBX_LLM_TIMEOUT` | Per-request timeout in seconds (default `120` — raise for slow local hardware) |
| `WEBX_EMBED_MODEL` / `WEBX_EMBED_BASE` / `WEBX_EMBED_KEY` | OpenAI-compatible `/v1/embeddings` for `query --semantic`, `crawl --semantic`, auto-embed on `index` (default base `localhost:11434/v1`) |
| `WEBX_INDEX_EMBED` | `chunk` (default) embeds per heading-path segment for long-doc recall; `page` restores one vector per page |

## Rendering

| Var | Effect |
|---|---|
| `WEBX_RENDER_URL` | Remote render service — delegates `--render`/`--auto-render`, the CLI launches no browser |
| `WEBX_CDP_URL` | Attach renders to an existing Chrome via CDP websocket (browserless/steel-style remote browser) |
| `WEBX_CHROME_BIN` | Chrome/Chromium binary path override for local rendering |
| `WEBX_RENDER_ENGINE` | `light` makes Lightpanda the default engine (equivalent to `--engine light` per request) |
| `WEBX_LIGHTPANDA_URL` | Lightpanda CDP endpoint (`http://host:9222` or `ws://…`) — skips auto-detect/spawn |
| `WEBX_PROXY` | HTTP proxy for fetches + Chrome renders |

## Server (`webx serve` / `webxd`)

| Var | Effect |
|---|---|
| `WEBX_API_KEY` | Bootstrap admin key — all routes except `/health` require `Authorization: Bearer <key>`; `--api` clients send it automatically. Tenant keys via `POST /keys` |
| `WEBX_MAX_CONCURRENCY` | Global in-flight request cap — over-limit → `429` + `Retry-After` |
| `WEBX_RATE_RPM` | Per-IP request cap (default `240`/min) |
| `WEBX_LICENSE` | Signed license file (path or JSON) — unlocks >3 API keys, admin roles, `GET /audit`, `zdr` mode |
| `WEBX_LICENSE_PUBKEY` | Override the embedded verifier key (dev/testing) |
| `WEBX_WEBHOOK_SECRET` | Fallback HMAC key for job webhooks — signs `X-Webx-Signature: sha256=…` (3-attempt retry) |
| `WEBX_CORS_ORIGINS` | Comma-separated allowed origins — enables CORS middleware (off by default) |
| `WEBX_JOB_TTL` | Retention for finished jobs — `done`/`failed`/`cancelled` jobs + pages swept hourly (default `168h`; `0` disables) |
| `WEBX_UPGRADE_URL` | Where 402-gated responses point users to buy a license |

## Security

| Var | Effect |
|---|---|
| `WEBX_BLOCK_PRIVATE` | SSRF guard — refuse private/loopback/metadata targets (**default ON**; `0` opts out for intranet deploys) |
| `WEBX_ALLOW_DOMAINS` / `WEBX_DENY_DOMAINS` | Comma-separated domain policy on every outbound target (subdomains included) |
| `WEBX_BOT_KEY` / `WEBX_BOT_DIRECTORY` | Web Bot Auth signing — key file from `webx botkey` + the https URL where your JWKS is served |

## Storage & concurrency

| Var | Effect |
|---|---|
| `WEBX_INDEX_DB` | Index path (default `~/.webx/index.db`) |
| `WEBX_CRAWL_CONCURRENCY` | Parallel page fetches for crawl/index/batch (default `4`; per-host rate limit still applies) |

`webx watch --exec` sets `WEBX_CHANGED_URL`, `WEBX_ADDED`, `WEBX_REMOVED` for the hook process — see [Index & watch](/cli/indexing/).

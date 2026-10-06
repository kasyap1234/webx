---
title: Self-hosting webxd
description: Docker compose profiles — from serve + searxng to the durable Postgres edition to a full hosted product.
---

Everything is driven by `docker-compose.yml` profiles — pick the topology that matches your durability and rendering needs.

## Profiles

| Profile | Command | You get |
|---|---|---|
| *(default)* | `docker compose up -d` | `webx serve` + self-hosted SearXNG on `:8080` |
| `durable` | `docker compose --profile durable up -d` | `webxd` + Postgres — jobs survive restarts, SKIP LOCKED worker claiming (scale replicas freely) |
| `render` | `docker compose --profile render up -d` | webx with Chrome for Testing baked in (one box, `shm_size: 2g` set for you) |
| `browserless` | `docker compose --profile browserless up -d` | Chrome as a sidecar over CDP — lean webx replicas + one warm browser. **Recommended production topology** |
| `cloud` | `docker compose --profile cloud up -d` | The sellable deployment — see below |

## SearXNG — the one env var that matters

Every profile wires `WEBX_SEARXNG_URL=http://searxng:8080` automatically. SearXNG fans out to upstream engines and webx fuses it with the dev-vertical providers and the local index — this is what takes general-web recall from free-tier to Google-class.

The bundled config at `./searxng/settings.yml` is mounted for you. If you bring your own instance, it **must** enable `formats: [html, json]` — JSON output is required.

## The cloud profile — your own hosted product

```bash
DOMAIN=api.example.com ACME_EMAIL=you@co.com \
PG_PASSWORD=<strong> WEBX_API_KEY=<bootstrap-admin-key> \
  docker compose --profile cloud up -d
```

That single command gives you: `webxd` (Postgres-backed, 4 workers) + SearXNG + browserless Chrome + Caddy auto-HTTPS on your domain. Point DNS at the box and it's a live product:

- **Tenants** — `POST /keys` (Bearer `WEBX_API_KEY`) issues customer keys; `PATCH /keys/{id}` sets `rpm`/`concurrency`/`monthly_units` — that *is* the billing surface
- **Licenses** — `webx license gen` mints signed license files for `WEBX_LICENSE`
- **Quotas → revenue** — over-limit keys get `429`/`402` responses with `Retry-After` and `upgrade` metadata

## Production checklist

- [ ] `WEBX_API_KEY` set — otherwise the API is open
- [ ] `WEBX_BLOCK_PRIVATE` left ON (SSRF guard) unless it's an intranet deploy
- [ ] `WEBX_RATE_RPM` tuned for your tenant mix (default 240/min per IP)
- [ ] `WEBX_JOB_TTL` for retention (default 168h)
- [ ] `WEBX_WEBHOOK_SECRET` if tenants receive webhooks
- [ ] `--store pg` (durable profile) before you scale past one replica — SQLite jobs don't share across processes

## Health endpoints

`GET /health` · `GET /ready` · `GET /metrics` · `GET /version` — wire `/ready` to your orchestrator's readiness probe and `/metrics` to Prometheus.

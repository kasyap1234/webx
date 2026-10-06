---
title: Firecrawl compatibility
description: /v1 and /v2 routes accept Firecrawl request shapes — their SDKs work against a self-hosted webx.
---

webx implements Firecrawl-shaped routes so existing integrations keep working when you repoint the base URL.

## Compatible routes

`POST /v1/scrape` · `POST /v1/crawl` · `POST /v1/search` · `POST /v1/map` · `POST /v1/extract` · `POST /v2/scrape` · `POST /v2/crawl` · `POST /v2/search` · `POST /v2/map` · `POST /v2/batch/scrape` · `POST /v2/extract` · `POST /v2/agent`

Request shapes accepted: `formats` (including object forms), `includeTags`, `waitFor`, `actions`, `maxAge`, `webhook`, `limit`, and the rest of the documented fields — unknown fields are ignored, not rejected.

## Migration

```python
# before — Firecrawl cloud
from firecrawl import FirecrawlApp
app = FirecrawlApp(api_key="fc-…")

# after — self-hosted webx (same SDK!)
app = FirecrawlApp(api_url="http://localhost:8080", api_key="webx_…")
```

```js
// JavaScript — same shape
import Firecrawl from "@mendable/firecrawl-js";
const app = new Firecrawl({ apiUrl: "http://localhost:8080", apiKey: "webx_…" });
```

That's the entire diff — one line.

## What translates

| Firecrawl feature | webx behavior |
|---|---|
| `formats: ["markdown","html","links","screenshot"]` | Same names; webx adds `chunks`, `a11y`, `agent_ready`, `transcript`, `redact_pii`, `pdf`, `mhtml` |
| `actions` | webx `--actions` grammar — click/fill/scroll/eval/`act:<english>` |
| `waitFor` | `--wait-for <selector>` equivalent |
| `maxAge` | `max_age` — serves fresh index copies inside the window |
| `webhook` on crawl | `webhook_url` + optional `webhook_secret` HMAC signing |
| `changeTracking` | `change_tracking` — diff vs indexed version |
| Async `/crawl` job polling | `GET /crawl/{id}` + SSE at `GET /crawl/{id}/events` |

## What doesn't (be honest)

- **Firecrawl's managed scale** — their browser fleet and proxy infra. webx renders with your own engine (or `WEBX_CDP_URL` → a browser fleet you control).
- **Credit semantics** — webx meters per-key `monthly_units` on *your* deployment, not their credit pool.
- Brand-new Firecrawl fields may lag — the compat layer covers the documented surface, not unreleased features.

Full comparison: [vs Firecrawl](/comparisons/vs-firecrawl/).

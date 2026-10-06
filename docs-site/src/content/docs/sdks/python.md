---
title: Python SDK
description: pip install webx-sdk — zero-dependency client for any webx server.
---

```bash
pip install webx-sdk
```

Zero dependencies — stdlib `urllib` only. Works in sandboxes, lambdas, and anywhere you can't install heavy clients. Import as `webx_sdk`:

```python
from webx_sdk import WebX

wx = WebX("http://localhost:8080")           # api_key="webx_..." if the server requires one
```

## Scrape

```python
doc = wx.scrape("https://example.com")
print(doc["data"]["markdown"])

# render + act on JS pages
doc = wx.scrape("https://spa.example.com", render=True,
                actions="click:.accept | scroll | screenshot",
                formats=["markdown", "a11y"])
```

## Search

```python
res = wx.search("go generics tutorial", limit=5)
```

## Crawl (async job — polls until done)

```python
job = wx.crawl("https://docs.example.com", goal="authentication docs",
               limit=20, semantic=True)
for page in job.data:
    print(page["url"])
```

## Extraction — deterministic CSS or LLM schema

```python
data = wx.extract("https://shop.example.com/p/1",
                  css={"title": "h1", "price": ".price"})
```

## Tenancy (admin key)

```python
key = wx.create_key("agent-1", rpm=120, monthly_units=5000)
print(key["key"])          # webx_... — shown once
print(wx.usage(days=7))

wx.schedule("nightly docs crawl", "0 3 * * *", "crawl",
            {"url": "https://docs.example.com", "limit": 50},
            webhook_url="https://me.example/hook")
```

## Errors

Raise `WebXError` with `.status`, `.code` (`payment_required`, `quota_exceeded`, `concurrency_limit`, …), and `.payment` metadata for 402-gated targets — upgrade paths are data, not dead ends.

Source: [`sdk/python/`](https://github.com/kasyap1234/webx/tree/main/sdk/python) — PyPI: `webx-sdk`.

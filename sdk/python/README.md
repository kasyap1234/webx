# webx-sdk — Python SDK

Zero-dependency Python client for any [webx](https://github.com/kasyap1234/webx) server
(`webx serve` locally, `webxd` in production, or a hosted deployment).

```bash
pip install webx-sdk
```

```python
from webx_sdk import WebX

wx = WebX("http://localhost:8080")           # api_key="webx_..." if the server requires one

# scrape
doc = wx.scrape("https://example.com")
print(doc["data"]["markdown"])

# render + act
doc = wx.scrape("https://spa.example.com", render=True,
                actions="click:.accept | scroll | screenshot",
                formats=["markdown", "a11y"])

# search
res = wx.search("go generics tutorial", limit=5)

# crawl (async job — polls until done)
job = wx.crawl("https://docs.example.com", goal="authentication docs",
               limit=20, semantic=True)
for page in job.data:
    print(page["url"])

# extraction — deterministic css or LLM schema
data = wx.extract("https://shop.example.com/p/1",
                  css={"title": "h1", "price": ".price"})

# tenancy (admin key)
key = wx.create_key("agent-1", rpm=120, monthly_units=5000)
print(key["key"])          # webx_... — shown once
print(wx.usage(days=7))
wx.schedule("nightly docs crawl", "0 3 * * *", "crawl",
            {"url": "https://docs.example.com", "limit": 50},
            webhook_url="https://me.example/hook")
```

Errors raise `WebXError` with `.status`, `.code` (`payment_required`,
`quota_exceeded`, `concurrency_limit`, …), and `.payment` metadata for
402-gated targets.

No dependencies — stdlib `urllib` only. Works in sandboxes and lambdas.

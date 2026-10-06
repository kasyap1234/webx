---
title: JavaScript SDK
description: npm install webx-sdk — zero-dependency ESM client with full TypeScript types.
---

```bash
npm install webx-sdk
```

Zero dependencies, ESM, Node 18+, full TypeScript types.

```js
import { WebX } from "webx-sdk";

const wx = new WebX({ baseUrl: "http://localhost:8080" }); // apiKey: "webx_..." if required
```

## Scrape

```js
const { data } = await wx.scrape("https://example.com");
console.log(data.markdown);

// render + actions on JS-heavy pages
const spa = await wx.scrape("https://spa.example.com", {
  render: true,
  actions: "click:.accept | scroll | screenshot",
  formats: ["markdown", "a11y"],
});
```

## Search

```js
const res = await wx.search("go generics tutorial", { limit: 5 });
```

## Crawl — async job, polls to completion

```js
const job = await wx.crawl("https://docs.example.com", {
  goal: "authentication docs", limit: 20, semantic: true,
});
for (const page of job.data) console.log(page.url);
// wait:false to fire-and-forget
```

## Deterministic extraction — zero LLM

```js
const out = await wx.extract("https://shop.example.com/p/1", {
  css: { title: "h1", price: ".price" },
});
```

## Tenancy (admin key)

```js
const { key } = await wx.createKey("agent-1", { rpm: 120, monthlyUnits: 5000 });
console.log(key);            // webx_... — shown once
console.log(await wx.usage({ days: 7 }));

await wx.schedule({
  name: "nightly docs crawl", cron: "0 3 * * *", action: "crawl",
  params: { url: "https://docs.example.com", limit: 50 },
  webhookUrl: "https://me.example/hook",
});
```

Source: [`sdk/javascript/`](https://github.com/kasyap1234/webx/tree/main/sdk/javascript) — npm: `webx-sdk`.

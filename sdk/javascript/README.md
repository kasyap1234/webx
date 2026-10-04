# webx-sdk — JavaScript/TypeScript SDK

Zero-dependency client for any [webx](https://github.com/kasyap1234/webx) server.
ESM, Node 18+, full TypeScript types.

```bash
npm install webx-sdk
```

```js
import { WebX } from "webx-sdk";

const wx = new WebX({ baseUrl: "http://localhost:8080" }); // apiKey: "webx_..." if required

// scrape
const { data } = await wx.scrape("https://example.com");
console.log(data.markdown);

// render + actions on JS-heavy pages
const spa = await wx.scrape("https://spa.example.com", {
  render: true,
  actions: "click:.accept | scroll | screenshot",
  formats: ["markdown", "a11y"],
});

// search
const res = await wx.search("go generics tutorial", { limit: 5 });

// crawl — async job, polls to completion (wait:false to fire-and-forget)
const job = await wx.crawl("https://docs.example.com", {
  goal: "authentication docs", limit: 20, semantic: true,
});
for (const page of job.data) console.log(page.url);

// deterministic extraction — zero LLM
const out = await wx.extract("https://shop.example.com/p/1", {
  css: { title: "h1", price: ".price" },
});

// tenancy (admin key)
const { key } = await wx.createKey("agent-1", { rpm: 120, monthlyUnits: 5000 });
console.log(key);            // webx_... — shown once
console.log(await wx.usage({ days: 7 }));
await wx.schedule({
  name: "nightly docs crawl", cron: "0 3 * * *", action: "crawl",
  params: { url: "https://docs.example.com", limit: 50 },
  webhookUrl: "https://me.example/hook",
});
```

Errors throw `WebXError` with `.status`, `.code` (`payment_required`,
`quota_exceeded`, `concurrency_limit`, …), and `.payment` metadata for
402-gated targets. camelCase options are converted to the API's snake_case.

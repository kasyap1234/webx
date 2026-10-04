# Benchmark results — 2026-10-03

Measured with `go run ./bench` on `bench/corpus.txt` (15 URLs: docs,
articles, repos, JS-heavy sites). Same machine, sequential requests,
300ms politeness gap. macOS arm64, residential connection, IST.

**Cold cache** (fresh server, empty store):

| provider | p50 | p95 | mean | success | content-check | md bytes |
|---|---|---|---|---|---|---|
| raw-http (floor) | 359ms | 1198ms | 463ms | 100% | 100% | 3,958,411 |
| jina-reader (anon) | 516ms | 1185ms | 554ms | 93% | 93% | 699,976 |
| **webx** | **996ms** | **3028ms** | **1042ms** | **100%** | **100%** | **249,167** |

## What the numbers say (and don't)

- **Jina's edge cache is warmer** — r.jina.ai is a shared cache for popular
  URLs; its ~500ms p50 is a served-cache number, not compute. webx's ~1s p50
  is cold fetch + local extraction. On un-cached / niche URLs the gap
  narrows or inverts; re-run on your own corpus before quoting.
- **webx hit 15/15** — jina got `403 AbuseAlleviation` on httpbin.org
  (domain policy). Raw+webx handled it.
- **webx emits 2.8× tighter output than jina** (249KB vs 700KB) — stricter
  main-content extraction drops nav/footer chrome. Raw HTML ceiling was
  3.9MB → webx compresses ~16×. Caveat: tighter is usually better for LLM
  context budgets, but bytes ≠ completeness — the content-check only
  verifies an expected substring per URL.
- **No keys needed to reproduce** — `go run ./bench` runs webx + jina-anon
  + raw floor out of the box. Set `FIRECRAWL_API_KEY` / `TAVILY_API_KEY` to
  add their rows.

## JS-heavy corpus — 2026-10-03

`bench/corpus-js.txt` — 15 harder targets: SPAs (youtube, spotify, whatsapp,
discord, claude.ai, chatgpt), commerce (airbnb, tiktok, amazon), news
(cnn, medium, producthunt), app walls (x.com), bot-test (nowsecure.nl).
webx ran with `auto_render` — escalate to Chrome only when the page demands it.

| provider | p50 | p95 | mean | success | content-check | md bytes |
|---|---|---|---|---|---|---|
| **webx (auto-render)** | **1572ms** | 32230ms | 5947ms | **100%** | **100%** | 93,932 |
| jina-reader (anon) | 611ms | 8172ms | 1738ms | 93% | 100% | 174,196 |
| raw-http (floor) | 383ms | 998ms | 403ms | 80% | 80% | 9,171,211 |

### What this run actually showed

- **webx 15/15, jina 14/15, raw 12/15.** raw-http 403'd on npmjs/medium/
  claude.ai (UA/bot walls) — webx's first run did too, which exposed a real
  gap: `auto_render` only escalated on 200-with-JS-shell, never on challenge
  statuses. **Fixed this session**: 403/429/503 now escalate to Chrome —
  npmjs recovered 10.4KB of package docs, medium and claude.ai landed real
  content. Jina got those three via its own infra; it 403'd on x.com where
  webx's chrome still returned a page.
- **Jina is faster** (p50 611ms vs 1572ms) — shared edge cache plus
  datacenter render fleet. webx pays cold-Chrome cost per first-hit page
  (youtube 32s, medium 30s = challenge-solving time). That's the honest
  trade for self-hosted + no API key.
- **webx emitted ~half jina's bytes** (94KB vs 174KB) — tighter extraction,
  same content-check pass rate.
- Honest caveat on "success": x.com/nowsecure/amazon returned thin payloads
  (49–333B wall/interstitial text) — they count as ok because they didn't
  error; the `bytes` column shows how thin. Per-URL rows in the JSON are
  the ground truth.

## Reproduce

```bash
webx serve --jobs-db /tmp/bench.db &
WEBX_BENCH_BASE=http://localhost:8080 go run ./bench -out results/r1.json
WEBX_BENCH_BASE=http://localhost:8080 go run ./bench -corpus bench/corpus-js.txt   # hard sites
```

Numbers age fast — providers change pricing, caching, and edge behavior.
Regenerate before publishing; record the date.

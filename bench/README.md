# webx-bench — comparative benchmark harness

Runs a fixed URL corpus through webx and external providers, measures
latency / success / extracted-content volume, and prints a markdown table
suitable for README inclusion.

```bash
webx serve --jobs-db /tmp/bench.db &     # start the server under test
go run ./bench                          # webx + raw-http + jina (no keys needed)
go run ./bench -n 3 -warm -out results.json
```

## Providers

| provider | env required | what it calls |
|---|---|---|
| `webx` | — (`WEBX_BENCH_BASE`, `WEBX_API_KEY` optional) | `POST {base}/scrape` |
| `raw-http` | — | plain `GET` — the floor; shows what extraction costs |
| `jina-reader` | `JINA_API_KEY` optional | `r.jina.ai/{url}` anonymous tier |
| `firecrawl` | `FIRECRAWL_API_KEY` | `api.firecrawl.dev/v2/scrape` |
| `tavily-extract` | `TAVILY_API_KEY` | `api.tavily.com/extract` |

Unset keys → provider is **skipped**, never faked. Add providers by
implementing the `Provider` interface (~20 lines).

## Metrics

- **p50 / p95 / mean latency** — median over `-n` runs per URL
- **success %** — response parsed to non-empty content
- **content-check %** — expected substring present (catches "succeeded but
  returned a captcha page" which inflates naive success rates)
- **md bytes** — total extracted-content volume (raw-html provider shows
  ceiling; differences show extraction vs passthrough)

## Methodology & honesty rules

- Same corpus, same order, same machine, sequential requests with a 300ms
  politeness gap. We benchmark *ourselves* under the same client code.
- `raw-http` is always included so readers see the network floor.
- `webx` runs **cold** (fresh server) and optionally `-warm` (cache-on
  second pass — labeled separately, never merged into cold numbers).
- Failures are printed in-line (`FAIL …`), not dropped from stats.
- Cloud providers are called on their default/free tier — not comparable
  to their paid render tiers; the table says "cloud" tier explicitly.
- Results go to `-out` JSON for reproducibility; commit snapshots under
  `bench/results/` if you publish numbers.

## Cost model (per request, list prices — verify before publishing)

| provider | unit cost of a markdown scrape |
|---|---|
| webx | self-hosted: compute only; hosted tiers: 1 unit |
| jina-reader | free tier 20 RPM anon; paid tiers per-token |
| firecrawl | 1 credit/page (≈$0.0016–0.0083 depending on plan) |
| tavily | extract billed per successful URL (≈1 credit/5 urls basic) |

Numbers in results are **as measured on the day** — re-run before quoting.

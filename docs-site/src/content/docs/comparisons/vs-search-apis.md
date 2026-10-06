---
title: vs search APIs
description: Exa / Tavily / Brave / Perplexity Sonar — owned indexes vs a metasearch layer + page toolkit you run.
---

*Last reviewed 2026-10-04. Prices move — check each provider's pricing page.
`webx eval --vs exa,tavily,firecrawl` re-scores the same corpus identically
through each engine when you have keys.*

## TL;DR

Exa and Tavily sell **answers from indexes they own**. webx is a **metasearch
layer + page toolkit you run** — it fuses providers (ddg, brave, searxng,
kagi, mojeek, hn, so, reddit, gh, npm, crates, grep.app, wikipedia, your own
index) and then *reads* the results. Different product, overlapping job.
Where webx is genuinely different: `webx index` + `webx query --semantic`
gives you a private neural+lexical index — the "Exa for your own corpus" — at
flat cost on your hardware.

| | webx | Exa | Tavily | Brave API | Sonar |
|---|---|---|---|---|---|
| Index | Yours (FTS5 + embeddings) + metasearch | Proprietary neural | Managed aggregate | Brave's web index | Managed + LLM answer |
| Price | Free / flat license | ~$7/1k searches w/contents; $5/1k answer; $12–15/1k deep research | ~$8/1k PAYG basic | Subscription tiers | Per-token |
| Self-host | Yes — it's the point | No | No | No | No |
| Page reading | First-class (`scrape`, `--fit`, render) | Contents/highlights | Extract + crawl | Snippets only | Answer text |
| Research | `webx research` → cited report | `research` endpoints | Research API | — | Core product |
| Grounding check | `verify` + `citation_check` | — | Grounding/injection defense | — | — |

## Where the APIs win

- **Search quality on general web queries.** Owned indexes beat metasearch
  fusion — our own eval measured 20.5% hit@10 on the free-provider corpus vs
  what a dedicated index delivers. If general-web recall is the product, pay
  for an index. (webx's number rises with `WEBX_SEARXNG_URL`/`BRAVE_API_KEY`.)
- **Latency.** No cold fetch — answers are pre-indexed.
- **Zero ops, SLAs,** and answer-quality engineering you don't maintain.

## Where webx wins

- **Private corpus search.** `webx index yourdocs.com --collection eng` then
  `webx query --semantic "auth flow"` — lexical + embedding fusion over
  *your* sites. None of these APIs index your private corpus for offline
  querying.
- **Read after search.** `search --scrape --highlights` fuses search and
  extraction in one call — results arrive as clean excerpts, not URLs you
  still have to fetch through a second API.
- **Metering you control.** Per-key quotas on your deployment vs per-call
  bills; agent-heavy workloads stop being a meter anxiety.
- **Provider independence.** Add/drop providers per query (`--site`,
  searxng categories, domain priors). No single-index lock-in; when a
  provider degrades, `webx doctor` tells you which.

## When to pick which

- **Pick Exa/Tavily** for general-web search quality inside a product, deep
  research at scale, or when per-call pricing is simply cheaper than ops.
- **Pick webx** for agent toolbelts (MCP), private/offline corpora,
  search+read in one hop, compliance-constrained environments, or
  cost-predictable high-volume workloads.
- **Compose them:** webx can sit in front of Brave/SearXNG and will happily
  share a pipeline with Exa — `search` for discovery, `scrape --fit` for
  reading, `index` for the durable corpus.

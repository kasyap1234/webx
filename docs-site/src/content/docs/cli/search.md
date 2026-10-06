---
title: Search & answer commands
description: search, ask, research, verify, eval, doctor — metasearch and cited answers.
---

## search

`webx search <q>` — metasearch over 15 providers — `ddg, hn, so, wiki, index, gh, reddit, sg, npm, crates, searxng, brave, mojeek, kagi, grep` — fused by weighted RRF + BM25 blending + domain priors. `score` is normalized 0–1; near-duplicates drop via title + simhash (`deduped` count in output).

### Filters

`--site` / `--domains` / `--exclude-domains` · `--after` / `--before` · `--lang` · `--topic` · `--exact` · `--location <cc>` (geo hint — Exa `userLocation`/Firecrawl `location` parity) · `--category developer` (restricts providers to code/docs sources: so, gh, grep, sg, npm, crates, hn, reddit)

### Depth & sources

```bash
webx search <q> --depth advanced              # Tavily-style tier: fast|basic|advanced
webx search <q> --sources web,news,images     # searxng category verticals
webx search <q> --subpages 3 --subpage-target a,b   # read matching subpages per result (Exa-style)
```

### Read-after-search

```bash
webx search <q> --scrape                      # fetch top results inline (bounded, per-page timeouts)
webx search <q> --scrape --highlights         # keep only query-relevant excerpts
webx search <q> --rerank                      # snippet-level rerank, no page fetches
webx search <q> --scrape --rerank             # content-level rerank on the scraped pages
webx search <q> --answer                      # attach a cited extractive answer (Tavily include_answer)
webx search <q> --fresh                       # bypass the 10m result cache
```

Rerank uses embedding cosine when `WEBX_EMBED_MODEL` is configured and a
term-coverage lexical score otherwise — opt-in, so search stays fast without it.
`depth:advanced` reranks automatically.

## ask

`webx ask <q>` — search → scrape → citation-numbered excerpts under a token budget. One shot, source-backed:

```bash
webx ask "how does htmx hx-swap work"
webx ask "go generics constraints" --llm      # LLM-synthesized cited answer (WEBX_LLM_*)
```

## research

`webx research <question>` — deep-research pipeline: expand → search → read sources → cited markdown report.

```bash
webx research "state of wasm in 2026" --sources 8
webx research <q> --schema '{"title":"string","findings":["string"]}'   # JSON matching schema (Tavily output_schema equivalent)
```

LLM-synthesized when `WEBX_LLM_*` is configured; falls back to ranked excerpts otherwise.

## verify

`webx verify <claim>` — fact-check against live web sources → `supported` | `refuted` | `unclear` + confidence + cited sources. The grounding check (Jina `g.jina.ai` equivalent); honestly reports `unavailable` without an LLM.

## eval

`webx eval` — score search quality on the bundled 30-query dev set → hit rate + MRR, per-kind breakdown. `--extract` scores content extraction quality. `eval --vs exa,tavily,firecrawl` re-scores the same corpus through each engine when you have their keys — the honest-benchmark harness.

## doctor

`webx doctor` — probe every provider: liveness, latency, result counts, config errors. Run this first when results look thin.

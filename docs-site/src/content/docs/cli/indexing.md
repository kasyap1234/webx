---
title: Index & watch commands
description: index, query, seed, similar, diff, watch, llms — your private searchable corpus.
---

## index

`webx index <domain>` — crawl → local SQLite FTS5 index (sitemap-driven or BFS).

```bash
webx index go.dev
webx index docs.example.com --collection eng --concurrency 8
webx index example.com --from-cc latest         # bootstrap from Common Crawl — zero host requests
```

| Flag | Effect |
|---|---|
| `--from-cc latest` | Common Crawl bootstrap — zero requests to the host |
| `--stale` | Skip pages already fresh (re-crawl mode) |
| `--respect-robots` | Honor robots.txt |
| `--include-paths` / `--exclude-paths <regex>` | Scope to URL paths |
| `--concurrency N` | Parallel fetches (`WEBX_CRAWL_CONCURRENCY`, default 4) |
| `--collection <name>` | Named corpus → `~/.webx/index-<name>.db` |
| `--gc --older-than 90d --vacuum` | Garbage-collect stale pages + orphan embeddings |

With `WEBX_EMBED_MODEL`, pages embed **per-chunk** (heading-path segments) — `WEBX_INDEX_EMBED=page` restores whole-page vectors.

## query

`webx query <q>` — index-only search. Offline, private, ~ms:

```bash
webx query "error handling"
webx query --semantic "graceful shutdown"         # bm25 + chunk-embedding cosine via RRF
webx query --collection eng "auth flow"
```

`--semantic` needs `WEBX_EMBED_MODEL` (Ollama works) and returns best-chunk snippets — see [Private index & semantic search](/guides/private-index/).

## seed

`webx seed` — index the curated list of ~25 top docs domains. Slow, polite crawl — run once for a useful default corpus.

## similar

`webx similar <url>` — find pages like this one via term-signature match. Local index by default; `--web` searches live providers (source URL excluded). `--limit` controls count.

## diff

`webx diff <url>` — line-diff live page vs indexed version. Sends `If-Modified-Since`, so unchanged pages cost a cheap 304. `--update-index` reindexes only on change.

## watch

`webx watch <url>` — monitor a page for content changes via conditional-GET polls against the index; reports `+added −removed` lines.

```bash
webx watch <url> --every 30m
webx watch <url> --once --exec "./notify.sh"     # cron-friendly
webx watch <url> --semantic                       # LLM judges if a diff is meaningful (skips timestamp churn)
```

`--exec` hooks receive `WEBX_CHANGED_URL`, `WEBX_ADDED`, `WEBX_REMOVED`.

## llms

`webx llms <domain>` — emit a whole-site `llms.txt` from the local index, grouped by path section with one-line blurbs. `--limit` caps pages. Hosted vendors sell this as a feature; here it's a JOIN.

:::tip[Machine-readable output]
`query`, `similar`, `diff`, and `llms` all accept `-f json` — structured results for agent pipelines (e.g. `webx diff <url> -f json` → `{"changed": true, "added": n, "removed": n, …}`).
:::

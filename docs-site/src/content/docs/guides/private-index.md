---
title: Private index & semantic search
description: Build a searchable private corpus — SQLite FTS5 + embeddings, collections, and offline query.
---

The index is webx's unique wedge: **Exa-style neural search over your own corpus**, on your hardware, working offline. One SQLite file holds pages, FTS5 terms, and chunk embeddings.

## Build an index

```bash
webx index go.dev                        # sitemap-driven crawl → ~/.webx/index.db
webx index docs.example.com --concurrency 8
webx index example.com --collection eng  # named corpus → ~/.webx/index-eng.db
```

Index options:

| Flag | Effect |
|---|---|
| `--from-cc latest` | Bootstrap from Common Crawl — **zero requests to the host** |
| `--stale` | Skip pages already fresh in the index (re-crawl mode) |
| `--respect-robots` | Honor robots.txt |
| `--include-paths` / `--exclude-paths <regex>` | Scope to URL paths |
| `--concurrency N` | Parallel fetches (default `WEBX_CRAWL_CONCURRENCY`=4, per-host rate-limited) |
| `--collection <name>` | Named corpus in a separate `.db` |
| `--gc --older-than 90d --vacuum` | Garbage-collect stale pages + orphan embeddings |

## Query it

```bash
webx query "error handling patterns"                 # FTS5 bm25 — offline, instant
webx query --semantic "graceful shutdown"            # + chunk-embedding cosine via RRF
webx query --collection eng "auth flow"              # search a named corpus
webx similar <url>                                   # term-signature match in the index
```

`--semantic` needs `WEBX_EMBED_MODEL` (+ `WEBX_EMBED_BASE`/`KEY` if not local Ollama). Pages embed **per-chunk** (heading-path segments) by default — better recall on long docs; `WEBX_INDEX_EMBED=page` restores whole-page vectors. Results come back with best-chunk snippets.

## Keep it fresh

```bash
webx diff <url>                  # line-diff live vs indexed — conditional GET (304 = cheap)
webx diff <url> --update-index   # reindex only on change
webx watch <url> --every 30m     # monitor — reports +added −removed lines
webx watch <url> --once --exec "./reindex.sh"   # cron-friendly one-shot + hook
```

`watch --exec` receives `WEBX_CHANGED_URL`, `WEBX_ADDED`, `WEBX_REMOVED`. `watch --semantic` asks the LLM whether a diff is *meaningful* — ignores timestamp/template churn.

## Turn the index into llms.txt

```bash
webx llms go.dev        # whole-site llms.txt — grouped by path, one-line blurbs
webx seed               # index ~25 curated docs domains (slow, polite crawl)
```

`llms` is the same output Mintlify sells as a feature — here it's a JOIN over your local index.

## Why this matters

None of the hosted search APIs (Exa/Tavily/Brave) will index *your* private corpus for offline querying. `index` + `query --semantic` is the self-hosted answer: your docs, your code wikis, your intranet — searchable in ~50ms with no network and no per-call bill.

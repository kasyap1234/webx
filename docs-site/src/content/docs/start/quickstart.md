---
title: Quickstart
description: Five commands that cover the core loop — search, read, index, answer.
---

The core loop: **search → read → store → query**. Every command below works immediately after `go install`.

## 1. Search the web

```bash
webx search "concurrent map writes golang panic"
```

Fused metasearch over 15 providers — weighted RRF + BM25 blending + domain priors. `score` is normalized 0–1, near-duplicates are dropped (`deduped` in output). Check provider health anytime with `webx doctor`.

## 2. Read a page

```bash
webx scrape https://go.dev/doc/effective_go
```

Clean markdown + metadata through the readability→trafilatura→full-page cascade. For JS-heavy pages:

```bash
webx scrape https://spa.example.com --auto-render   # escalates http → browser automatically
webx scrape https://go.dev --fit "http server"      # BM25-filtered markdown — the token-saver
```

## 3. Search *and* read in one hop

```bash
webx ask "how does htmx hx-swap work"
```

Search → scrape top results → citation-numbered excerpts under a token budget. Add `--llm` for a synthesized cited answer (needs `WEBX_LLM_*`).

## 4. Build a private index

```bash
webx index go.dev
webx query "error handling patterns"                # lexical FTS5, offline
webx query --semantic "graceful shutdown"           # + embeddings (WEBX_EMBED_MODEL)
```

`index` crawls the sitemap (or BFS) into `~/.webx/index.db`. `--semantic` fuses bm25 with chunk-level embedding cosine — Exa-style neural search over *your* corpus.

## 5. Serve it

```bash
webx serve                                          # HTTP API on :8080
curl -X POST localhost:8080/scrape \
  -H 'content-type: application/json' \
  -d '{"url":"https://go.dev/doc/effective_go"}'
```

Firecrawl `/v1`/`/v2` routes are built in — point a Firecrawl SDK at the same base URL and it works. Full surface: [HTTP API](/api/).

## Next steps

- [Fetch tiers & rendering](/guides/rendering/) — when http stops being enough
- [Agents, MCP & skills](/guides/agents/) — wire webx into Claude Code, Cursor, Devin
- [Command map](/cli/) — all 30 commands with flags

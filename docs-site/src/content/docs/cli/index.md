---
title: Command map
description: All 30 webx commands, grouped by job — fetch, search & answer, index & watch, agents, server & admin.
---

Every command accepts `--api <base-url>` to run on a remote `webx serve`/`webxd` instead of locally.

## Fetch — get pages

| Command | What it does |
|---|---|
| [`scrape <url>`](/cli/fetch/#scrape) | URL → clean markdown + metadata. Render, actions, fit, chunks, PII redaction, a11y refs |
| [`batch <file>`](/cli/fetch/#batch) | Scrape many URLs concurrently; `--api` makes it a durable server job |
| [`map <domain>`](/cli/fetch/#map) | Sitemap URL discovery |
| [`cdx <pattern>`](/cli/fetch/#cdx) | Common Crawl index — enumerate URLs without touching the host |
| [`wayback <url>`](/cli/fetch/#wayback) | Wayback Machine captures — list, or extract `--at <ts>` |
| [`archive <url>…`](/cli/fetch/#archive) | Raw fetches → WARC/1.0 (ISO 28500, replayweb.page-compatible) |

## Search & answer — find things

| Command | What it does |
|---|---|
| [`search <q>`](/cli/search/#search) | 15-provider metasearch — weighted RRF + BM25 + domain priors |
| [`ask <q>`](/cli/search/#ask) | Search → read top results → cited excerpts in one shot |
| [`research <q>`](/cli/search/#research) | Deep research → cited markdown report (`--schema` for JSON) |
| [`verify <claim>`](/cli/search/#verify) | Fact-check → `supported\|refuted\|unclear` + confidence |
| [`eval`](/cli/search/#eval) | Search/extraction quality on the bundled dev set (hit rate + MRR) |
| [`doctor`](/cli/search/#doctor) | Probe every provider — liveness, latency, config errors |

## Index & watch — your corpus

| Command | What it does |
|---|---|
| [`index <domain>`](/cli/indexing/#index) | Crawl → local SQLite FTS5 (+ embeddings with `WEBX_EMBED_MODEL`) |
| [`query <q>`](/cli/indexing/#query) | Index-only search — offline; `--semantic` adds neural fusion |
| [`seed`](/cli/indexing/#seed) | Index ~25 curated docs domains |
| [`similar <url>`](/cli/indexing/#similar) | Term-signature near-neighbors — index or `--web` |
| [`diff <url>`](/cli/indexing/#diff) | Live vs indexed line-diff — conditional GET (304-aware) |
| [`watch <url>`](/cli/indexing/#watch) | Change monitor — `--every`, `--once`, `--exec`, `--semantic` |
| [`llms <domain>`](/cli/indexing/#llms) | Whole-site `llms.txt` from the index |

## Agents — for the machines

| Command | What it does |
|---|---|
| [`extract <url>`](/cli/agents/#extract) | Page → schema'd JSON — deterministic CSS/JSON-LD or LLM |
| [`mcp`](/cli/agents/#mcp) | MCP server — stdio, or `POST /mcp` on a running server (17 tools) |
| [`skill`](/cli/agents/#skill) | Emit a SKILL.md teaching agent harnesses how to use webx |
| [`botkey`](/cli/agents/#botkey) | Web Bot Auth signing key + self-hostable JWKS |

## Server & admin — run it as a product

| Command | What it does |
|---|---|
| [`serve`](/cli/server/#serve) | HTTP API — all routes, tenancy, jobs, `/v1`+`/v2` Firecrawl compat |
| [`jobs`](/cli/server/#jobs) / [`cancel`](/cli/server/#cancel) | List/inspect/cancel async jobs — local or `--api` remote |
| [`install <engine>`](/cli/server/#install) | Install Chrome for Testing or Lightpanda |
| [`license`](/cli/server/#license) | `keygen` / `gen` / `check` — signed license tooling |
| `completion` | Shell autocompletion (bash/zsh/fish/powershell) |
| `help` | Help on any command |

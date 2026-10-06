---
title: Agent commands
description: extract, mcp, skill, botkey — the machine-facing surface.
---

## extract

`webx extract <url>` — page → schema'd JSON. Three modes, cheapest first:

| Mode | Flag | LLM needed? |
|---|---|---|
| Deterministic CSS | `--css '{"title":"h1","links":"a[]@href"}'` | No — selector extraction |
| schema.org entities | `--type product\|article\|job\|event\|faq` | No — reads JSON-LD |
| LLM schema | default path | Yes (`WEBX_LLM_*`) |

```bash
webx extract <url> --css '{"title":"h1","price":".price","imgs":"img[]@src"}'
webx extract <url> --type product
webx extract <url> --render                      # JS pages
```

Prefer `--css` when you know the shape — it's deterministic, free, and reproducible.

## mcp

`webx mcp` — run webx as an MCP server over **stdio**. On a running `serve`/`webxd`, the same 17 tools are exposed over streamable HTTP at `POST /mcp`.

Tools: `search` · `scrape` · `ask` · `query` · `map` · `doctor` · `crawl` · `extract` · `diff` · `research` · `verify` · `watch` · `similar` · `wayback` · `llms` · `answer` · `batch`

Client setup: [Agents, MCP & skills](/guides/agents/).

## skill

`webx skill` — print a `SKILL.md` that teaches agent harnesses (Claude Code, Devin, Cursor, Windsurf) *when* to use webx and how to call it:

```bash
webx skill > .devin/skills/webx/SKILL.md
```

## botkey

`webx botkey` — generate a **Web Bot Auth** ed25519 key + the JWKS to self-host. Set `WEBX_BOT_KEY` + `WEBX_BOT_DIRECTORY` and every outbound request is cryptographically signed (RFC 9421, draft-meunier-webbotauth) — a verifiable identity for your crawler instead of fingerprint-guessing. The anti-stealth: sites that adopt the spec can *recognize* and allow your bot.

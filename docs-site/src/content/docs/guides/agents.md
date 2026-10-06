---
title: Agents, MCP & skills
description: Wire webx into Claude Code, Cursor, Devin, Windsurf, and any MCP client — 17 tools plus a generated SKILL.md.
---

webx is built for agent harnesses first. Two integration paths — use either or both.

## Path 1: MCP server (17 tools)

```bash
webx mcp            # stdio transport — point your client at the binary
```

On a running server (`webx serve` / `webxd`), MCP is also exposed over streamable HTTP at `POST /mcp`.

### Client setup

:::tabs
- Claude Code

  ```bash
  # stdio
  claude mcp add webx -- webx mcp

  # or streamable HTTP against a running server
  claude mcp add --transport http webx http://localhost:8080/mcp
  ```

- Cursor / Windsurf / other MCP clients

  Add to the client's MCP config (usually `~/.cursor/mcp.json` or project `.mcp.json`):

  ```json
  {
    "mcpServers": {
      "webx": { "command": "webx", "args": ["mcp"] }
    }
  }
  ```

- Devin

  ```bash
  webx mcp   # register as an MCP server in Devin's MCP settings
  ```

:::

### Tools exposed

`search` · `scrape` · `ask` · `query` · `map` · `doctor` · `crawl` · `extract` · `diff` · `research` · `verify` · `watch` · `similar` · `wayback` · `llms` · `answer` · `batch`

## Path 2: generated skill

```bash
webx skill > .devin/skills/webx/SKILL.md     # or .claude/skills/, .agents/skills/
```

`webx skill` emits a SKILL.md that teaches the harness *when* to reach for webx (search/scrape/index/research) and how to call it — no MCP transport needed, works anywhere the CLI does.

## Agent-oriented output features

| Feature | Why agents care |
|---|---|
| `--fit <query>` | BM25-filtered markdown — drops nav/footer/boilerplate before it burns context |
| `--chunks` | Heading-path segments with `est_tokens` — RAG-ready, budgetable |
| `--a11y` + `@eN` refs | Deterministic element targeting for `click:@e3`/`fill:@e2=x` with occlusion checks |
| `--max-tokens` | Hard output budget |
| `--agent-ready` | Probes llms.txt/AI-bot robots/WebMCP/api-catalog → `agent_ready{}` report |
| Prompt-injection warnings | Content-trust flags on scraped output |
| Web Bot Auth | `webx botkey` signs requests cryptographically (RFC 9421) — identity instead of stealth |

## Point agents at a server instead

Any command accepts `--api <base-url>` to run server-side — agents on thin clients (sandboxes, lambdas) get full capability through one HTTP endpoint:

```bash
webx --api http://webx.internal:8080 scrape <url>
```

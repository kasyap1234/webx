---
title: Installation
description: Install webx — single static binary via go install, source build, or Docker.
---

webx ships as a **single static binary**. No runtime dependencies are required for HTTP fetching, metasearch, indexing, or the HTTP API — Chrome is only needed if you use real-browser rendering.

## Install the binary

:::tabs
- go install

  ```bash
  go install github.com/kasyap1234/webx/cmd/webx@latest
  webx --version
  ```

- From source

  ```bash
  git clone https://github.com/kasyap1234/webx
  cd webx
  go build -o webx ./cmd/webx
  ./webx --version
  ```

- Docker

  ```bash
  docker compose up -d        # webx serve + searxng, see Self-hosting
  ```

:::

## Optional: render engines

Static HTTP fetch needs nothing extra. For JavaScript-heavy pages, install an engine once — webx auto-detects it:

```bash
webx install chrome       # Chrome for Testing (via rod's downloader)
webx install lightpanda   # Lightpanda — ~16x lighter than Chrome
```

Or delegate rendering entirely:

- `WEBX_RENDER_URL` — remote render service; the CLI never launches a browser
- `WEBX_CDP_URL` — attach to an existing Chrome over CDP (browserless/steel-style)
- `WEBX_CHROME_BIN` — point at a system Chrome/Chromium instead of downloading

## Optional: LLM + embeddings

Features that synthesize (`extract`, `ask --llm`, `research`, `verify`, `act:<english>` actions) or embed (`query --semantic`, `crawl --semantic`, auto-embed on `index`) use OpenAI-compatible endpoints. **Ollama works out of the box** — see [Configuration](/start/configuration/).

## The server binary

`webx serve` runs the HTTP API from the same binary. For production, build the dedicated server:

```bash
go build -o webxd ./cmd/webxd   # multi-tenant API server — Postgres-capable store
```

## Where state lives

| Path | Contents |
|---|---|
| `~/.webx/index.db` | FTS5 index + embeddings (default; `--collection <name>` → `index-<name>.db`) |
| `~/.webx/jobs.db` | Durable job queue |
| `~/.webx/profiles/` | Persistent browser profiles (`--profile`) |
| `~/.webx/bin/` | `webx install` engines |

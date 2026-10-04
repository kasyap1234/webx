# Changelog

Notable user-facing changes. This project follows semver-ish: breaking API
changes bump minor, fixes bump patch. `webx version` prints the build.

## [Unreleased]

### Added
- `webx eval` quality harness: search hit-rate/MRR with Wilson CI, extraction
  marker coverage, `--cite` citation-verifier accuracy, `--vs` competitor
  comparison (exa/tavily/firecrawl, key-gated), `--gen` corpus builder from
  ORCAS click logs + Stack Exchange + GitHub (~1300 real queries), `--json`,
  `--workers`.
- `POST /crawl/{id}/retry` — requeue failed crawl pages as a batch job.
- `GET /license` — deployment's active tier/features/expiry; `upgrade` field
  in all 402 license errors (`WEBX_UPGRADE_URL` overridable).
- Firecrawl `/v2/agent` + `GET /v2/agent/{id}`; object `formats`
  (screenshot/json) on `/v2/scrape`.
- Bounded search enrichment: top-K scrape with per-page deadline; simhash
  near-duplicate drop (`deduped` field); normalized 0–1 `score`.
- Semantic rerank on `--rerank`/`depth=advanced` when `WEBX_EMBED_MODEL` set.
- `citation_check` on `/answer --llm`: verified/unverified markers — digits,
  number-words, entities, and acronym expansions are hard requirements.
- Bounded `/agent` retry round; quality-scored extraction cascade; incremental
  chunk re-embedding; provider panic isolation; h2-uTLS transport fix.
- `cloud` docker-compose profile — webxd + Postgres + searxng + browserless +
  Caddy auto-TLS: a self-hosted production deploy in one command.
- `cmd/webx` test suite; jobs GC (`WEBX_JOB_TTL`, `webx jobs --gc`); embedding
  model stamping + scan bounds; per-command CLI split.
- `docs/` honest comparisons — vs Firecrawl, vs Crawl4AI, vs search APIs —
  dated and reproducible (`go run ./bench`, `webx eval --vs`).
- `.github` license-request issue template (structured purchase path) and
  FUNDING.yml → SPONSORS.md.
- goreleaser: ghcr.io docker login in the release job; commented Homebrew
  tap block ready for `kasyap1234/homebrew-tap`.

### Changed
- **Python SDK is `webx-sdk` on PyPI** (`pip install webx-sdk`,
  `import webx_sdk`) — the bare `webx` name belongs to an unrelated ASGI
  framework and would have installed the wrong package.
- CI `release` job now actually triggers: workflow listens for `v*` tags.

### Fixed
- CI `release` job was unreachable — the workflow only fired on `main`
  pushes/PRs, so tag pushes never started goreleaser.
- Removed tracked `webx.egg-info`/build artifacts; gitignored Python
  packaging outputs.
- `llms.txt` MCP tool count corrected (12 → 17).
- `statusWriter` now forwards `Flush()` — SSE job events work behind
  middleware.
- Search cache key includes depth/sources/subpages.
- Batch job worker-pool drain on cancel; nil-context deadlock in job ops.

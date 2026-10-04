# Contributing to webx

webx is a single-binary web search/extraction toolkit for AI coding agents.
Contributions are welcome — the design rules below are what keep the binary
small and the output honest.

## Setup

```bash
git clone https://github.com/kasyap1234/webx && cd webx
go build -o webx ./cmd/webx && ./webx doctor   # provider check
```

Requires Go 1.22+. No external services are needed for the core; search
quality improves with a local SearXNG (`docker compose up`) and
`WEBX_SEARXNG_URL`. Browser features need Chrome or Chromium on PATH.

## The design rules

- **HTTP first, Chrome last.** New extraction behavior belongs in `web/fetch`
  before `web/render`. A feature that only works rendered should set
  `needs_render`, not silently escalate.
- **Honest over fancy.** Report failures, warnings, tier used, cache age.
  Never swallow provider errors — aggregate and surface them.
- **Static-binary friendly.** No cgo, no required services, no network
  dependencies at build time. SQLite only (`modernc.org/sqlite`).
- **Content is untrusted.** Anything fetched can contain prompt injection.
  Keep injection warnings intact; don't feed pages to an LLM unsanitized.

## Testing

```bash
go test ./...        # unit tests — no network
go vet ./...
gofmt -l .           # must be empty
```

Write tests against `httptest` servers — no live-network tests in the suite.
If your feature touches the render path, verify it once against a real page
locally before opening the PR.

## Making changes

1. Small, single-purpose PRs. Describe the agent-facing benefit, not just
   the mechanism.
2. Update `README.md` for user-visible flags/endpoints and `CHANGELOG.md`
   under `[Unreleased]`.
3. Match existing style: compact code, comments only where the *why* isn't
   obvious, no new dependencies without discussion.
4. Provider additions: implement `search.Provider`, wire it in
   `search.DefaultProviders`, and degrade gracefully when unconfigured.

## Releasing (maintainers)

Releases run through GoReleaser on tag push:

```bash
git tag vX.Y.Z && git push origin vX.Y.Z
```

Binaries for linux/darwin/windows are built by CI; the `webx` formula and
docker image follow from `.goreleaser.yaml`.

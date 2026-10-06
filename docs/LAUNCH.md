# Launch checklist

One-time maintainer steps to take webx from repo → revenue. Everything
code-side is done; what's left is accounts, secrets, and DNS. ~2h total.

## 1. Release (done once CI is green on main)

```bash
git tag v0.1.0 && git push origin v0.1.0
```

The tag triggers: goreleaser → GitHub release artifacts, `ghcr.io/kasyap1234/webx`
image, Homebrew formula bump in `kasyap1234/homebrew-tap`, SDK publishes (gated, see §3).

- [x] `HOMEBREW_TAP_TOKEN` secret set — currently the gh CLI OAuth token.
      **Swap for a fine-grained PAT scoped to `kasyap1234/homebrew-tap`
      (contents:write)** and re-run `gh secret set HOMEBREW_TAP_TOKEN`.
- [x] Homebrew is a **source formula** (`Formula/webx.rb`, auto-bumped by the
      `tap-formula` CI job on each tag) — NOT a cask. Cask'd prebuilt binaries
      get Gatekeeper-quarantined (`Killed: 9`); source builds sign ad-hoc on
      install. Same caveat applies to manual release-tarball downloads via a
      browser: `xattr -d com.apple.quarantine ./webx`. Proper fix once revenue
      justifies it: Apple Developer ID + `quill` notarization in goreleaser
      ($99/yr).

## 2. Self-serve license sales (webxl)

`webxl` is **already deployed as a Cloudflare Worker** (free tier) at
`https://webxl.kasyap3103.workers.dev` — source in `workers/webxl/`, a
zero-dep port of `internal/fulfill`. Routes: `POST /webhooks/polar`,
`POST /webhooks/stripe`, `GET /health`, `GET /mailbox` (Bearer `ADMIN_TOKEN`;
serves licenses when Resend isn't configured). KV namespace `SEEN` handles
retry dedup + the mailbox spool. Already set: `WEBX_MAINTAINER_SEED`,
`ADMIN_TOKEN`. To redeploy: `cd workers/webxl && npx wrangler deploy`.

Remaining secrets to set (`npx wrangler secret put <NAME>` in `workers/webxl/`):

- [ ] `POLAR_WEBHOOK_SECRET` — Polar.sh org → webhook endpoint
      `https://webxl.kasyap3103.workers.dev/webhooks/polar`, event `order.paid`.
      (Stripe also works: `STRIPE_WEBHOOK_SECRET`, `STRIPE_SECRET_KEY`.)
- [ ] `POLAR_TIERS` / `STRIPE_TIERS` — product→tier maps (plain `[vars]`,
      edit `wrangler.toml`): `"prod_xxx:pro,prod_yyy:enterprise:400"`.
- [ ] `RESEND_API_KEY` + verify sender domain → set `LICENSE_FROM`.
      Until then licenses land in `/mailbox` — fetch with
      `curl -H "Authorization: Bearer $ADMIN_TOKEN" .../mailbox` then
      `?key=<name>`.

Alternative: the Go `webxl` binary is also in the release artifacts for a
VPS/fly.io deploy (env: `WEBX_MAINTAINER_KEY`, same webhook secrets; listens
:8090). The Worker is the recommended path — $0/month, no server.

- [ ] Create a **Polar.sh** org (built for OSS devs; merchant of record —
      they handle sales tax/VAT). Products: `webx Pro` ($49/mo subscription)
      and `webx Enterprise` (custom/invoice). Note each product ID →
      `POLAR_TIERS` in `workers/webxl/wrangler.toml`.
- [ ] Put the checkout link in README § Pricing + site Pricing.svelte
      (replace the issue-template CTA for Pro; keep it for Enterprise).
- [ ] Renewal handling is automatic: each billing cycle re-fires the paid
      event → customer gets a fresh license file.

The maintainer key lives at `~/.webx/maintainer.pem` on this machine —
**back it up somewhere safe (1Password/encrypted volume); losing it means
re-keying every customer.** It's never committed to the repo.

## 3. SDK registries

- [ ] **npm**: create account → `npm token create` (Granular, publish
      `webx-sdk`) → `gh secret set NPM_TOKEN --body <token>` →
      `gh variable set NPM_ENABLED --body true`. Next tag publishes.
- [ ] **PyPI**: create `webx-sdk` project → Publishing → add GitHub trusted
      publisher (repo `kasyap1234/webx`, workflow `publish-sdks.yml`,
      environment `pypi`) → repo Settings → Environments → create `pypi` →
      `gh variable set PYPI_ENABLED --body true`.
- [ ] Bump `version` in `sdk/python/pyproject.toml` +
      `sdk/javascript/package.json` before each release tag (workflow refuses
      to publish a duplicate version — that's the point).

## 4. Discovery

- [ ] **Domain** — webx.dev / usewebx.dev / webx.sh (~$12/yr). Sites are on
      Cloudflare Pages: `webx-5sp.pages.dev` (marketing) +
      `webx-docs.pages.dev` (docs), deployed via
      `npx wrangler pages deploy site/build|docs-site/dist
      --project-name=webx|webx-docs`. A custom domain is one CNAME in the
      Pages project settings. Then update: `site/src/lib/data.ts` `DOCS_URL`,
      `docs-site` SITE_URL env, repo homepage URL.
- [ ] **MCP registry** — `server.json` is at `mcp/server.json`; install
      `mcp-publisher` and `mcp-publisher publish` (GitHub OAuth to verify the
      `io.github.kasyap1234/*` namespace). Also list on Smithery + Glama.
- [ ] **Awesome lists** — PR into awesome-scraping, awesome-mcp-servers,
      awesome-selfhosted.
- [ ] **Launch posts** — Show HN (lead with the measured benchmarks +
      Wilson CIs — that's the differentiator), r/selfhosted, r/LocalLLaMA,
      a build-log post on dev.to. Best posting window: Tue–Thu morning PT.
- [ ] Docker Hub mirror (optional): `docker login` in CI + second `images:`
      entry in `.goreleaser.yaml` dockers_v2.

## 5. Hosted cloud (the bigger revenue line)

The metering/keys/usage-billing code already runs. What it needs:

- [ ] VPS (or fly.io) → `docker compose --profile cloud up -d` with `DOMAIN`,
      `WEBX_API_KEY`, `PG_PASSWORD` — gives TLS'd webxd+Postgres+searxng.
- [ ] Signup flow: a small page (can live in `site/`) → `POST /keys` on the
      deployment → issue first key. Free tier: `monthly_units` cap per key.
- [ ] Metered billing: `GET /usage` per key/day/endpoint → Polar metered
      billing or Stripe usage records on a cron.
- [ ] Point `WEBX_UPGRADE_URL` at your pricing page so 402s route to it.

## 6. Optional but cheap

- [ ] Cosign-signed releases (`signs:` in goreleaser) + SBOM — you ship a
      tool that signs outbound requests; supply-chain hygiene is on-brand.
- [ ] GitHub Discussions on; pin a "self-host stories" thread.
- [ ] `SECURITY.md` already exists — add a `security@` email when the domain
      exists.
- [ ] Opt-in anonymous telemetry (`--telemetry`, off by default): counts
      command usage to see the funnel. Be loud about it being opt-in —
      it fits the honest-metrics brand.
- [ ] Demo GIF for the README top — `brew install vhs ffmpeg`, record
      `docs/demo.tape` (scripted: version → scrape → search → serve).
      Host under `docs/assets/`.

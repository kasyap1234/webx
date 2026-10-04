# Security Policy

## Supported versions

| Version | Supported |
|---|---|
| latest `main` | yes |
| tagged releases | latest only — upgrade |

## Reporting a vulnerability

Do **not** open a public issue for security problems.

Email: **security@kasyap.dev** (or open a private
[GitHub Security Advisory](https://github.com/kasyap1234/webx/security/advisories/new))

Include: the affected version/commit, reproduction steps, and impact.
You'll get an acknowledgment within 72h and a fix or mitigation plan.

## Security-relevant surfaces

webx fetches arbitrary URLs by design — the SSRF guard (`WEBX_BLOCK_PRIVATE`,
`WEBX_ALLOW_DOMAINS`/`WEBX_DENY_DOMAINS`) is the primary boundary; report any
way to reach private/link-local addresses through the fetch, crawl, render,
webhook, or extract paths.

Also in scope:
- Webhook signature bypasses (`WEBX_WEBHOOK_SECRET`, HMAC-SHA256)
- API-key auth bypass on `serve`/`webxd` (billing/keys/audit routes)
- License signature forgery (`internal/license`, Ed25519)
- Credential exfiltration via render actions or extract prompts
- `job.db`/`jobs.db` injection through stored params

Not in scope: content fetched from third-party sites, rate-limit policy
disagreements, or issues in upstream providers themselves.

## Deployment hardening (operators)

- Always set `WEBX_API_KEY` on any non-loopback `serve`/`webxd`.
- `WEBX_BLOCK_PRIVATE=1` (default on) unless you *intend* intranet scraping.
- Terminate TLS at the edge (the `cloud` compose profile uses Caddy); webx
  speaks plain HTTP.
- Jobs/webhook URLs are user-controlled — `WEBX_BLOCK_PRIVATE` applies there
  too, but treat webhook targets as egress.

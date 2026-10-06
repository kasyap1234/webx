---
title: Licenses & tiers
description: Community is free and complete — Pro/Enterprise unlock tenancy features via signed license files.
---

## Tiers

| | community | pro | enterprise |
|---|---|---|---|
| Price | **$0** forever | **$49/mo** per deployment | contact |
| All 16+ commands | ✓ | ✓ | ✓ |
| API keys | 3 | unlimited | unlimited |
| RBAC roles | — | ✓ | ✓ |
| Audit log | — | ✓ | ✓ |
| Zero-data-retention mode | — | ✓ | ✓ |
| Support | community | priority | dedicated |
| Deployment | self-hosted | per deployment | air-gap ok |

The free tier is a product, not a demo — every command, the full fetch ladder, the index, MCP, and the HTTP API work with no license at all. Gating applies only to **team/tenancy features** a business actually needs: more than 3 API keys, admin roles, `GET /audit`, and `zdr` no-persistence mode.

## How licensing works

Licenses are **signed Ed25519 license files** — verified offline by an embedded public key, no phone-home:

```bash
webx license install webx-license.json   # verify + park at ~/.webx/license.json
webx license status                      # confirm the active tier

webx serve    # picks up ~/.webx/license.json automatically — no env needed
webxd --addr :8080
```

`WEBX_LICENSE=<path|inline-json>` still works and wins over the file when set.

Without a valid license, gated requests return **HTTP 402** with `upgrade` metadata pointing at `WEBX_UPGRADE_URL` — clients (and the SDKs) surface it as a purchase prompt, not a silent failure.

## Buying a license

Self-serve checkout (Polar/Stripe) emails a signed license within seconds — subscription renewals automatically issue a fresh license each billing cycle. Manual path: the [license request form](https://github.com/kasyap1234/webx/issues/new?template=license-request.yml), fulfilled within 1 business day.

## For operators: minting licenses

```bash
webx license keygen --key maintainer.pem                    # one-time keypair (never commit)
webx license gen --key maintainer.pem --tier pro \
    --email user@co.com --out lic.json                      # sign a customer license
webx license check lic.json                                 # verify
```

To automate delivery on purchases, run `webxl` — a tiny webhook service that verifies Polar/Stripe payment events, signs licenses with your maintainer key, and emails them via Resend (or spools to a mailbox dir). See `docs/LAUNCH.md` for wiring.

`WEBX_LICENSE_PUBKEY` overrides the verifier for development.

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
# set on the server — path or inline JSON
WEBX_LICENSE=/etc/webx/license.json webx serve
# or
webxd --addr :8080   # with WEBX_LICENSE in the environment
```

Without a valid license, gated requests return **HTTP 402** with `upgrade` metadata pointing at `WEBX_UPGRADE_URL` — clients (and the SDKs) surface it as a purchase prompt, not a silent failure.

## Buying a license

Self-serve checkout is coming; today, purchase is via the [license request form](https://github.com/kasyap1234/webx/issues/new?template=license-request.yml) — include your deployment type and use case, and a signed license file comes back by email.

## For operators: minting licenses

```bash
webx license keygen                                    # one-time maintainer keypair
webx license gen --tier pro --days 365 --out lic.json  # sign a customer license
webx license check lic.json                            # verify
```

`WEBX_LICENSE_PUBKEY` overrides the verifier for development.

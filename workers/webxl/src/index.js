// webxl on Workers — payment webhook → signed webx license → email.
// Faithful port of internal/fulfill: same wire formats, same license schema.
// Runs on the free tier; KV provides retry dedup + the mailbox spool.
// Ed25519 signing uses Workers-native Web Crypto — zero dependencies.
//
// Secrets (wrangler secret put): WEBX_MAINTAINER_SEED, POLAR_WEBHOOK_SECRET,
// STRIPE_WEBHOOK_SECRET, STRIPE_SECRET_KEY, RESEND_API_KEY, ADMIN_TOKEN.
// Vars (wrangler.toml): LICENSE_FROM, POLAR_TIERS, STRIPE_TIERS.

const SIG_TOLERANCE_S = 300;
const MAILBOX_TTL_S = 60 * 60 * 24 * 30; // unseen licenses live 30d in KV
const SEEN_TTL_S = 60 * 60 * 24 * 30;

const TIER_FEATURES = {
	pro: ["multikey", "rbac", "audit"],
	enterprise: ["multikey", "rbac", "audit", "zdr", "priorityq"],
};
const TIER_DAYS = { pro: 35, enterprise: 400 };

export default {
	async fetch(req, env, ctx) {
		const url = new URL(req.url);
		if (req.method === "GET" && url.pathname === "/health") {
			return Response.json({ ok: true, service: "webxl" });
		}
		if (req.method === "GET" && url.pathname === "/mailbox") {
			return mailboxList(req, env);
		}
		if (req.method !== "POST") {
			return new Response("method not allowed", { status: 405 });
		}

		const body = await req.text();
		let order = null;
		if (url.pathname === "/webhooks/polar") {
			if (!(await verifyPolar(env, req.headers, body))) return unauth();
			order = polarOrder(body);
		} else if (url.pathname === "/webhooks/stripe") {
			if (!(await verifyStripe(env, req.headers.get("Stripe-Signature"), body))) {
				return unauth();
			}
			order = await stripeOrder(env, body);
		} else {
			return new Response("not found", { status: 404 });
		}
		if (!order || !order.email) return Response.json({ ok: true, skipped: true });
		const id = await fulfill(env, ctx, order);
		return Response.json({ ok: true, license: id });
	},
};

const unauth = () => Response.json({ error: "bad signature" }, { status: 401 });

// ── signature verification ────────────────────────────────────────────

const b64d = (s) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
const te = new TextEncoder();

async function hmac(keyBytes, msg) {
	const key = await crypto.subtle.importKey(
		"raw", keyBytes, { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
	return new Uint8Array(await crypto.subtle.sign("HMAC", key, te.encode(msg)));
}
const b64 = (b) => btoa(String.fromCharCode(...b));
const hex = (b) => [...b].map((x) => x.toString(16).padStart(2, "0")).join("");
// constant-time for fixed-length digests
const eq = (a, b) => a.length === b.length && a.split("").reduce((r, c, i) => r | (c.charCodeAt(0) ^ b.charCodeAt(i)), 0) === 0;

// Standard Webhooks: HMAC-SHA256 base64 over "{id}.{ts}.{body}", key is the
// whsec_-stripped base64-decoded secret; header "v1,<sig> [v1,<sig>...]".
async function verifyPolar(env, headers, body) {
	const secret = env.POLAR_WEBHOOK_SECRET;
	if (!secret) return false;
	const id = headers.get("webhook-id");
	const ts = parseInt(headers.get("webhook-timestamp") || "", 10);
	const sigHeader = headers.get("webhook-signature") || "";
	if (!id || !ts || Math.abs(Date.now() / 1000 - ts) > SIG_TOLERANCE_S) return false;
	let key = te.encode(secret);
	if (secret.startsWith("whsec_")) {
		try { key = b64d(secret.slice(6)); } catch { /* raw secret */ }
	}
	const want = b64(await hmac(key, `${id}.${ts}.${body}`));
	return sigHeader.split(/\s+/).some((e) => {
		const i = e.indexOf(",");
		return i > 0 && e.slice(0, i) === "v1" && eq(e.slice(i + 1), want);
	});
}

// Stripe-Signature "t=…,v1=hex,…": HMAC-SHA256 hex over "{t}.{body}", raw secret.
async function verifyStripe(env, header, body) {
	const secret = env.STRIPE_WEBHOOK_SECRET;
	if (!secret || !header) return false;
	let ts = "";
	const sigs = [];
	for (const part of header.split(",")) {
		const i = part.indexOf("=");
		if (i < 0) continue;
		if (part.slice(0, i) === "t") ts = part.slice(i + 1);
		if (part.slice(0, i) === "v1") sigs.push(part.slice(i + 1));
	}
	const t = parseInt(ts, 10);
	if (!t || Math.abs(Date.now() / 1000 - t) > SIG_TOLERANCE_S) return false;
	const want = hex(await hmac(te.encode(secret), `${ts}.${body}`));
	return sigs.some((s) => eq(s, want));
}

// ── order extraction ──────────────────────────────────────────────────

// Polar order.paid — fires per billing cycle, so renewals re-issue licenses.
function polarOrder(body) {
	let ev;
	try { ev = JSON.parse(body); } catch { return null; }
	if (ev.type !== "order.paid") return null;
	const d = ev.data || {};
	return {
		eventID: "polar:" + (ev.id || d.id || ""),
		email: d.customer?.email || "",
		productID: d.product_id || d.product?.id || "",
		tier: "",
	};
}

async function stripeOrder(env, body) {
	let ev;
	try { ev = JSON.parse(body); } catch { return null; }
	const o = ev.data?.object || {};
	if (ev.type === "checkout.session.completed") {
		return {
			eventID: "stripe:" + ev.id,
			email: o.customer_details?.email || o.customer_email || "",
			productID: await stripeSessionPrice(env, o.id),
			tier: o.metadata?.tier || "",
		};
	}
	if (ev.type === "invoice.payment_succeeded") {
		return {
			eventID: "stripe:" + ev.id,
			email: o.customer_email || "",
			productID: o.lines?.data?.find((l) => l.price?.id)?.price.id || "",
			tier: "",
		};
	}
	return null;
}

// Session payload lacks the price — one API call for line items when keyed.
async function stripeSessionPrice(env, sessionID) {
	if (!env.STRIPE_SECRET_KEY || !sessionID) return "";
	const r = await fetch(
		`https://api.stripe.com/v1/checkout/sessions/${sessionID}/line_items?limit=1`,
		{ headers: { Authorization: `Bearer ${env.STRIPE_SECRET_KEY}` } });
	if (!r.ok) return "";
	const j = await r.json();
	return j.data?.[0]?.price?.id || "";
}

// "id:tier[:days],id2:tier" — missing days → per-tier default.
function parseProductMap(s) {
	const m = {};
	for (const part of (s || "").split(",")) {
		const f = part.trim().split(":");
		if (f.length < 2) continue;
		m[f[0]] = { tier: f[1], days: parseInt(f[2], 10) || TIER_DAYS[f[1]] || 0 };
	}
	return m;
}

// ── mint + deliver ────────────────────────────────────────────────────

// Go's json.Marshal HTML-escapes <>& and U+2028/9 inside strings — the
// canonical claims must be byte-identical to what webx verifies.
const goJSON = (v) => JSON.stringify(v)
	.replace(/&/g, "\\u0026").replace(/</g, "\\u003c").replace(/>/g, "\\u003e")
	.replace(/\u2028/g, "\\u2028").replace(/\u2029/g, "\\u2029");

const rfc3339 = (d) => d.toISOString().replace(/\.\d+Z$/, "Z");

// lic_YYYYMMDD_<hex> — mirrors license.NewID (hex of unix-nano).
function licID() {
	const now = new Date();
	const ymd = now.toISOString().slice(0, 10).replace(/-/g, "");
	const nano = BigInt(now.getTime()) * 1000000n +
		BigInt(crypto.getRandomValues(new Uint32Array(1))[0]);
	return `lic_${ymd}_${nano.toString(16)}`;
}

async function fulfill(env, ctx, order) {
	// KV dedup — provider retries after our 2xx must not double-email.
	if (order.eventID) {
		if (await env.SEEN.get("seen:" + order.eventID)) return "dup";
	}
	const maps = { ...parseProductMap(env.POLAR_TIERS), ...parseProductMap(env.STRIPE_TIERS) };
	const rule = maps[order.productID] || {};
	const tier = order.tier || rule.tier || "pro"; // unmapped → pro; mailbox flags it
	const days = rule.days || TIER_DAYS[tier] || 35;

	const now = new Date();
	const lic = {
		id: licID(),
		email: order.email,
		tier,
		issued: rfc3339(now),
		expires: rfc3339(new Date(now.getTime() + days * 86400e3)),
		features: TIER_FEATURES[tier] || [],
	};
	lic.sig = await signLicense(env, lic);

	const raw = pretty(lic);
	await deliver(env, order.email, lic, raw);
	if (order.eventID) {
		ctx.waitUntil(env.SEEN.put("seen:" + order.eventID, "1", { expirationTtl: SEEN_TTL_S }));
	}
	return lic.id;
}

// Sign claimsView — uppercase keys, struct order, exactly as Go marshals it.
// Workers' raw import accepts public keys only; a private key imports as
// PKCS8 — RFC 8410 fixed prefix + 32-byte seed (WEBX_MAINTAINER_SEED).
const PKCS8_PREFIX = new Uint8Array([
	0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06,
	0x03, 0x2b, 0x65, 0x70, 0x04, 0x22, 0x04, 0x20,
]);
async function signLicense(env, lic) {
	const claims = {
		ID: lic.id, Email: lic.email, Tier: lic.tier,
		Issued: lic.issued, Expires: lic.expires, Features: lic.features,
	};
	const pkcs8 = new Uint8Array(48);
	pkcs8.set(PKCS8_PREFIX);
	pkcs8.set(b64d(env.WEBX_MAINTAINER_SEED), PKCS8_PREFIX.length);
	const key = await crypto.subtle.importKey(
		"pkcs8", pkcs8, { name: "Ed25519" }, false, ["sign"]);
	const sig = await crypto.subtle.sign("Ed25519", key, te.encode(goJSON(claims)));
	return b64(new Uint8Array(sig));
}

// MarshalIndent(lic, "", "  ") equivalent for the license file itself.
function pretty(lic) {
	// lowercase keys, license-schema order, 2-space indent, Go escaping
	const ordered = {
		id: lic.id, email: lic.email, tier: lic.tier,
		issued: lic.issued, expires: lic.expires,
		features: lic.features, sig: lic.sig,
	};
	return JSON.stringify(ordered, (k, v) => v, 2)
		.replace(/&/g, "\\u0026").replace(/</g, "\\u003c").replace(/>/g, "\\u003e")
		.replace(/\u2028/g, "\\u2028").replace(/\u2029/g, "\\u2029");
}

async function deliver(env, email, lic, raw) {
	if (env.RESEND_API_KEY && env.LICENSE_FROM) {
		const r = await fetch("https://api.resend.com/emails", {
			method: "POST",
			headers: {
				Authorization: `Bearer ${env.RESEND_API_KEY}`,
				"Content-Type": "application/json",
			},
			body: JSON.stringify({
				from: env.LICENSE_FROM,
				to: [email],
				subject: `Your webx ${lic.tier} license`,
				html: `<p>Thanks — your webx <b>${lic.tier}</b> license is attached.</p>
<p>Install it on your deployment:</p>
<pre>webx license install webx-license.json</pre>
<p>Tier: ${lic.tier} · License: ${lic.id} · Expires: ${lic.expires}<br>
Subscriptions renew the license automatically — each billing cycle emails a fresh file.</p>`,
				attachments: [{ filename: "webx-license.json", content: b64(te.encode(raw)) }],
			}),
		});
		if (!r.ok) throw new Error(`resend: ${r.status} ${await r.text()}`);
		return;
	}
	// Mailbox fallback — retrievable via GET /mailbox with ADMIN_TOKEN.
	const safe = email.replace(/@/g, "_at_").replace(/[\\/]/g, "_");
	await env.SEEN.put(`mbx:${safe}:${lic.id}`, raw, { expirationTtl: MAILBOX_TTL_S });
}

// GET /mailbox — lists + serves spooled licenses when Resend is unconfigured.
async function mailboxList(req, env) {
	if (!env.ADMIN_TOKEN || req.headers.get("Authorization") !== `Bearer ${env.ADMIN_TOKEN}`) {
		return unauth();
	}
	const url = new URL(req.url);
	const key = url.searchParams.get("key");
	if (key) {
		const v = await env.SEEN.get(key);
		return v ? new Response(v, { headers: { "content-type": "application/json" } })
			: new Response("gone", { status: 404 });
	}
	const list = await env.SEEN.list({ prefix: "mbx:" });
	return Response.json(list.keys.map((k) => k.name));
}

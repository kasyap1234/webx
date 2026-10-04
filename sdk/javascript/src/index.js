/**
 * webx — JavaScript SDK for the webx web toolkit.
 * Zero dependencies — uses global fetch (Node 18+).
 *
 *   import { WebX } from "webx-sdk";
 *   const wx = new WebX({ baseUrl: "http://localhost:8080" });
 *   const doc = await wx.scrape("https://example.com");
 *   console.log(doc.data.markdown);
 */

export class WebXError extends Error {
  /** @param {string} message @param {number} status @param {string} code @param {object} [payment] */
  constructor(message, status = 0, code = "", payment = null) {
    super(message);
    this.name = "WebXError";
    this.status = status;
    this.code = code;       // payment_required | quota_exceeded | …
    this.payment = payment; // x402/AP2 metadata when code=payment_required
  }
}

export class Job {
  /** @param {WebX} client @param {string} id */
  constructor(client, id) {
    this.client = client;
    this.id = id;
    this.status = "queued";
    this.data = [];
  }

  /**
   * Poll until done/failed/cancelled.
   * @param {{interval?: number, timeout?: number, onTick?: (status: string, done: number, total: number) => void}} [opts]
   */
  async poll(opts = {}) {
    const { interval = 1500, timeout = 600000, onTick } = opts;
    const deadline = Date.now() + timeout;
    while (Date.now() < deadline) {
      const st = await this.client._get(`/crawl/${this.id}`);
      this.status = st.status || "?";
      this.data = st.data || [];
      this.result = st.result; // research/extract/agent payloads
      onTick?.(this.status, st.completed ?? 0, st.total ?? 0);
      if (["done", "failed", "cancelled"].includes(this.status)) {
        if (this.status === "failed") throw new WebXError(`job failed: ${st.error || ""}`);
        return this;
      }
      await new Promise(r => setTimeout(r, interval));
    }
    throw new WebXError(`job ${this.id} timed out`);
  }

  /** Pages that failed fetch → {errors: [{url, error, status}]}. */
  errors() { return this.client._get(`/crawl/${this.id}/errors`); }

  cancel() { return this.client._post(`/crawl/${this.id}/cancel`, {}); }
}

export class WebX {
  /**
   * @param {{baseUrl?: string, apiKey?: string, timeout?: number}} [opts]
   * apiKey — a webx_… key from POST /keys, or the deployment's WEBX_API_KEY.
   */
  constructor({ baseUrl = "http://localhost:8080", apiKey = null, timeout = 120000 } = {}) {
    this.base = baseUrl.replace(/\/+$/, "");
    this.apiKey = apiKey;
    this.timeout = timeout;
  }

  /** @param {"GET"|"POST"|"PATCH"|"DELETE"} method */
  async _req(method, path, body) {
    const headers = { "Content-Type": "application/json" };
    if (this.apiKey) headers.Authorization = `Bearer ${this.apiKey}`;
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), this.timeout);
    try {
      const resp = await fetch(this.base + path, {
        method, headers, signal: ctrl.signal,
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      const text = await resp.text();
      let json = null;
      try { json = JSON.parse(text); } catch { /* text body */ }
      if (resp.status >= 400) {
        const e = json || {};
        throw new WebXError(e.error || text || resp.statusText,
          resp.status, e.code || "", e.payment || null);
      }
      return json ?? text;
    } finally {
      clearTimeout(timer);
    }
  }

  _post(path, body) { return this._req("POST", path, body); }
  _get(path) { return this._req("GET", path); }
  _patch(path, body) { return this._req("PATCH", path, body); }
  _delete(path) { return this._req("DELETE", path); }

  // ── core API ───────────────────────────────────────────────────────────

  /**
   * Fetch a page → {success, data:{markdown,title,...}}.
   * @param {string} url
   * @param {object} [opts] — formats, render, actions, waitFor→wait_for,
   *   session, pageSession→page_session, engine:"light", cookies, maxAge→max_age,
   *   zdr… camelCase is converted to the API's snake_case.
   */
  scrape(url, opts = {}) {
    return this._post("/scrape", { url, ...snake(opts) });
  }

  /** @param {string} query */
  search(query, opts = {}) {
    return this._post("/search", { query, ...snake(opts) });
  }

  /**
   * Grounded answer — Exa's /answer. Returns {success, answer|null,
   * answer_type, evidence_md, citations[]}. answer_type is "llm" only
   * when a WEBX_LLM_* backend actually synthesized; otherwise extractive.
   */
  answer(query, opts = {}) {
    return this._post("/answer", { query, ...snake(opts) });
  }

  /** Sitemap/URL discovery → {success, links[]}. */
  map(url, opts = {}) {
    return this._post("/map", { url, ...snake(opts) });
  }

  /**
   * Structured extraction — {css:{field:selector}} is deterministic (zero LLM).
   * url may be a wildcard "example.com/*" (requires async:true); urls[] batches.
   * async:true (or wait:false) returns a Job — job.result holds the payload.
   * @param {string|object} url — url string, or an object of options incl. url/urls
   */
  async extract(url, opts = {}) {
    const { wait = true, ...rest } = opts;
    const body = { url, ...snake(rest) };
    if (!wait) body.async = true;
    const res = await this._post("/extract", body);
    if (body.async && res.id) {
      const job = new Job(this, res.id);
      return wait ? job.poll() : job;
    }
    return res;
  }

  /**
   * Deep research → cited report. outputSchema → JSON output.
   * async:true / wait:false returns a Job — job.result holds the report.
   */
  async research(question, opts = {}) {
    const { wait = true, ...rest } = opts;
    const body = { query: question, ...snake(rest) };
    if (!wait) body.async = true;
    const res = await this._post("/research", body);
    if (body.async && res.id) {
      const job = new Job(this, res.id);
      return wait ? job.poll() : job;
    }
    return res;
  }

  /**
   * Goal-only extraction — find the pages, extract what's asked
   * (Firecrawl /agent parity). wait:true (default) → result;
   * wait:false → Job handle (job.result holds the extraction).
   * @param {string} goal
   * @param {{url?: string, schema?: object, limit?: number, render?: boolean,
   *          webhookUrl?: string, webhookSecret?: string, wait?: boolean}} [opts]
   */
  async agent(goal, opts = {}) {
    const { wait = true, ...rest } = opts;
    const body = { goal, ...snake(rest) };
    if (!wait) body.async = true;
    const res = await this._post("/agent", body);
    if (!wait && res.id) return new Job(this, res.id);
    return res;
  }

  /** Fact-check → {verdict, confidence, sources}. */
  verify(claim) {
    return this._post("/verify", { claim });
  }

  similar(url, opts = {}) {
    return this._post("/similar", { url, ...snake(opts) });
  }

  /** Wayback: {at:"2020"} fetches the nearest capture, else lists snapshots. */
  wayback(url, opts = {}) {
    return this._post("/wayback", { url, ...snake(opts) });
  }

  /**
   * Async crawl → Job handle. Set wait:false to skip polling.
   * @param {string} url
   * @param {{goal?: string, limit?: number, depth?: number, semantic?: boolean,
   *          webhookUrl?: string, wait?: boolean}} [opts]
   */
  async crawl(url, opts = {}) {
    const { wait = true, ...rest } = opts;
    const res = await this._post("/crawl", { url, ...snake(rest) });
    const job = new Job(this, res.id);
    return wait ? job.poll() : job;
  }

  async batchScrape(urls, options = {}, { wait = true } = {}) {
    const res = await this._post("/batch/scrape", { urls, options });
    const job = new Job(this, res.id);
    return wait ? job.poll() : job;
  }

  // ── tenancy (admin key required) ────────────────────────────────────────

  /** Create an API key → {key} shown once. */
  createKey(name, opts = {}) {
    return this._post("/keys", { name, ...snake(opts) });
  }
  keys() { return this._get("/keys"); }
  updateKey(id, fields) { return this._patch(`/keys/${id}`, snake(fields)); }
  deleteKey(id) { return this._delete(`/keys/${id}`); }

  /** Metering report — members see own key, admins all. */
  usage({ days = 30, key = "" } = {}) {
    return this._get(`/usage?days=${days}${key ? `&key=${key}` : ""}`);
  }

  /** Request audit trail — licensed deployments only. */
  audit(limit = 500) { return this._get(`/audit?limit=${limit}`); }

  /** Recurring job — cron "0 9 * * *", action "crawl"|"batch". */
  schedule({ name, cron, action, params = {}, timezone = "", webhookUrl = "" }) {
    return this._post("/schedules", {
      name, cron, action, params, timezone, webhook_url: webhookUrl });
  }
  schedules() { return this._get("/schedules"); }
  updateSchedule(id, fields) { return this._patch(`/schedules/${id}`, fields); }
  deleteSchedule(id) { return this._delete(`/schedules/${id}`); }

  jobs() { return this._get("/jobs"); }
  jobErrors(jobId) { return this._get(`/crawl/${jobId}/errors`); }
  cancel(jobId) { return this._post(`/crawl/${jobId}/cancel`, {}); }
  doctor() { return this._get("/doctor"); }
  health() { return this._get("/health"); }
  ready() { return this._get("/ready"); }
  version() { return this._get("/version"); }
}

/** camelCase option keys → the API's snake_case. */
function snake(obj) {
  const out = {};
  for (const [k, v] of Object.entries(obj)) {
    if (v === undefined) continue;
    out[k.replace(/[A-Z]/g, c => "_" + c.toLowerCase())] = v;
  }
  return out;
}

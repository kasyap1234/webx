"""webx client — thin, dependency-free wrapper over the webx HTTP API.

Only stdlib (urllib) — safe to embed in agent sandboxes and lambdas.
"""

from __future__ import annotations

import json
import time
import urllib.error
import urllib.request
from typing import Any, Callable, Optional


class WebXError(Exception):
    """API error — carries HTTP status and webx's stable error `code`."""

    def __init__(self, message: str, status: int = 0, code: str = "",
                 payment: Optional[dict] = None):
        super().__init__(message)
        self.status = status
        self.code = code          # payment_required | quota_exceeded | …
        self.payment = payment    # x402/AP2 metadata when code=payment_required


class Job:
    """Handle for an async crawl/batch job — poll() blocks to completion."""

    def __init__(self, client: "WebX", job_id: str):
        self.client = client
        self.id = job_id
        self.status = "queued"

    def poll(self, interval: float = 1.5,
             on_tick: Optional[Callable[[str, int, int], None]] = None,
             timeout: float = 600) -> "Job":
        deadline = time.time() + timeout
        while time.time() < deadline:
            st = self.client._get(f"/crawl/{self.id}")
            self.status = st.get("status", "?")
            self.data = st.get("data") or []
            self.result = st.get("result")  # research/extract/agent payloads
            if on_tick:
                on_tick(self.status, st.get("completed", 0), st.get("total", 0))
            if self.status in ("done", "failed", "cancelled"):
                if self.status == "failed":
                    raise WebXError(f"job failed: {st.get('error','')}")
                return self
            time.sleep(interval)
        raise WebXError(f"job {self.id} timed out")

    def errors(self) -> dict:
        """Pages that failed fetch → {errors: [{url, error, status}]}."""
        return self.client._get(f"/crawl/{self.id}/errors")

    def cancel(self):
        self.client._post(f"/crawl/{self.id}/cancel", {})


class WebX:
    """Client for a webx server.

    base_url — e.g. http://localhost:8080 or https://webx.internal:443
    api_key  — a webx_… key from POST /keys, or the deployment's WEBX_API_KEY
    """

    def __init__(self, base_url: str = "http://localhost:8080",
                 api_key: Optional[str] = None, timeout: float = 120):
        self.base = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout

    # ── transport ────────────────────────────────────────────────────────

    def _req(self, method: str, path: str, body: Optional[dict]) -> dict:
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(
            self.base + path, data=data, method=method,
            headers={"Content-Type": "application/json"})
        if self.api_key:
            req.add_header("Authorization", f"Bearer {self.api_key}")
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                return json.loads(resp.read())
        except urllib.error.HTTPError as e:
            raw = e.read().decode(errors="replace")
            msg, code, payment = raw, "", None
            try:
                err = json.loads(raw)
                msg = err.get("error", raw)
                code = err.get("code", "")
                payment = err.get("payment")
            except ValueError:
                pass
            raise WebXError(msg, status=e.code, code=code, payment=payment)
        except urllib.error.URLError as e:
            raise WebXError(f"connection failed: {e.reason}") from e

    def _post(self, path: str, body: dict) -> dict:
        return self._req("POST", path, body)

    def _get(self, path: str) -> dict:
        return self._req("GET", path, None)

    def _patch(self, path: str, body: dict) -> dict:
        return self._req("PATCH", path, body)

    def _delete(self, path: str) -> dict:
        return self._req("DELETE", path, None)

    # ── core API ─────────────────────────────────────────────────────────

    def scrape(self, url: str, formats: Optional[list] = None,
               screenshot_options: Optional[dict] = None,
               lang: Optional[str] = None, **opts) -> dict:
        """Fetch a page → {success, data:{markdown,title,metadata,…}}.

        formats: ["markdown","html","links","images","screenshot","pdf",
                  "mhtml","summary","branding","chunks","a11y","agent_ready",
                  "transcript","redact_pii"]
        screenshot_options: {"full_page": bool, "quality": 0-100,
                             "viewport": {"width","height"}} — pairs with
                            formats=["screenshot"] + render=True.
        lang: preferred caption language for formats=["transcript"].
        opts: render=True, actions="click:.a | scroll", wait_for, session,
              page_session, engine="light", cookies="<netscape text>",
              max_age, zdr, … — passed through verbatim.
        """
        body = {"url": url}
        if formats:
            body["formats"] = formats
        if screenshot_options:
            body["screenshot_options"] = screenshot_options
        if lang:
            body["lang"] = lang
        body.update(opts)
        return self._post("/scrape", body)

    def search(self, query: str, limit: int = 10,
               providers: Optional[list] = None,
               scrape: bool = False, semantic: bool = False,
               domains: Optional[list] = None,
               exclude_domains: Optional[list] = None,
               after: Optional[str] = None, before: Optional[str] = None,
               topic: Optional[str] = None, lang: Optional[str] = None,
               exact: bool = False, highlights_only: bool = False,
               rerank: bool = False, auto_render: bool = False,
               depth: Optional[str] = None,
               sources: Optional[list] = None,
               subpages: int = 0,
               subpage_target: Optional[list] = None,
               collection: Optional[str] = None,
               **opts) -> dict:
        """Fused multi-provider search → {success, data:[results], errors}.

        Exa-parity filters: domains/exclude_domains (include/exclude),
        after/before (RFC3339 or YYYY-MM-DD published-date bounds),
        topic="news" for freshness, exact phrase-match. scrape adds
        content+highlights per result; highlights_only keeps the excerpts
        without full content; auto_render escalates scrapes past JS-walls.
        depth="fast|basic|advanced" picks the result tier (Tavily
        search_depth); sources=["web","news","images"] selects verticals;
        subpages/subpage_target read matching subpages per top result
        (Exa subpages); collection adds a named local-index corpus.
        Extra fields pass through verbatim.
        """
        body: dict[str, Any] = {"query": query, "limit": limit,
                                "scrape": scrape, "semantic": semantic,
                                "exact": exact,
                                "highlights_only": highlights_only,
                                "rerank": rerank,
                                "auto_render": auto_render}
        if providers:
            body["providers"] = providers
        if domains:
            body["domains"] = domains
        if exclude_domains:
            body["exclude_domains"] = exclude_domains
        if after:
            body["after"] = after
        if before:
            body["before"] = before
        if topic:
            body["topic"] = topic
        if lang:
            body["lang"] = lang
        if depth:
            body["depth"] = depth
        if sources:
            body["sources"] = sources
        if subpages:
            body["subpages"] = subpages
        if subpage_target:
            body["subpage_target"] = subpage_target
        if collection:
            body["collection"] = collection
        body.update(opts)
        return self._post("/search", body)

    def answer(self, query: str, num: int = 5, llm: bool = False,
               providers: Optional[list] = None,
               domains: Optional[list] = None,
               max_tokens: int = 0, **opts) -> dict:
        """Grounded answer — Exa's /answer. Returns {success, answer|null,
        answer_type, evidence_md, citations[]}. answer_type is "llm" only
        when a WEBX_LLM_* backend actually synthesized; otherwise the
        response stays extractive and llm_error explains why."""
        body: dict[str, Any] = {"query": query, "num": num, "llm": llm}
        if providers:
            body["providers"] = providers
        if domains:
            body["domains"] = domains
        if max_tokens:
            body["max_tokens"] = max_tokens
        body.update(opts)
        return self._post("/answer", body)

    def map(self, url: str, limit: int = 0) -> dict:
        """Sitemap/URL discovery → {success, links[]}."""
        body: dict[str, Any] = {"url": url}
        if limit:
            body["limit"] = limit
        return self._post("/map", body)

    def extract(self, url: str = "", urls: Optional[list] = None,
                schema: Optional[dict] = None,
                prompt: Optional[str] = None,
                css: Optional[dict] = None,
                type_: Optional[str] = None, async_: bool = False,
                wait: bool = True, **opts):
        """Structured extraction — css is deterministic, schema+prompt LLM.

        `url` may be a wildcard like "example.com/*" — the server maps the
        site then extracts in a queued job (requires async_=True); `urls`
        batches explicit URLs. Returns a Job when async_, a result dict
        otherwise. Poll job → job.result carries the extraction payload.
        """
        body: dict[str, Any] = {"url": url, "async": async_}
        if urls:
            body["urls"] = urls
        if schema:
            body["schema"] = schema
        if prompt:
            body["prompt"] = prompt
        if css:
            body["css"] = css
        if type_:
            body["type"] = type_
        body.update(opts)
        out = self._post("/extract", body)
        if async_ and "id" in out:
            job = Job(self, out["id"])
            if wait:
                job.poll()
            return job
        return out

    def research(self, question: str, schema: Optional[dict] = None,
                 sources: int = 0, async_: bool = False,
                 wait: bool = True, **opts):
        """Deep-research pipeline → cited report (schema → JSON output).
        async_=True returns a Job; job.result carries the report."""
        body: dict[str, Any] = {"query": question, "async": async_}
        if schema:
            body["output_schema"] = schema
        if sources:
            body["sources"] = sources
        body.update(opts)
        out = self._post("/research", body)
        if async_ and "id" in out:
            job = Job(self, out["id"])
            if wait:
                job.poll()
            return job
        return out

    def agent(self, goal: str, url: Optional[str] = None,
              schema: Optional[dict] = None, limit: int = 0,
              render: bool = False, webhook_url: Optional[str] = None,
              webhook_secret: Optional[str] = None,
              wait: bool = True, **opts):
        """Goal-only extraction — find the pages, extract what's asked.
        (Firecrawl /agent parity.) wait=True → result dict; wait=False →
        Job handle (job.result holds the extraction)."""
        body: dict[str, Any] = {"goal": goal, "render": render}
        if url:
            body["url"] = url
        if schema:
            body["schema"] = schema
        if limit:
            body["limit"] = limit
        if webhook_url:
            body["webhook_url"] = webhook_url
        if webhook_secret:
            body["webhook_secret"] = webhook_secret
        body.update(opts)
        if not wait:
            body["async"] = True
        out = self._post("/agent", body)
        if not wait and "id" in out:
            return Job(self, out["id"])
        return out

    def verify(self, claim: str) -> dict:
        """Fact-check → {success, data:{verdict,confidence,sources}}."""
        return self._post("/verify", {"claim": claim})

    def similar(self, url: str, limit: int = 10, web: bool = False) -> dict:
        """Pages like `url`. web=True distills the page's signature and
        searches live providers (Exa findSimilar, works on unindexed
        URLs); default searches the local index."""
        return self._post("/similar", {"url": url, "limit": limit, "web": web})

    def wayback(self, url: str, at: Optional[str] = None) -> dict:
        """List captures, or fetch nearest snapshot when `at` given."""
        body: dict[str, Any] = {"url": url}
        if at:
            body["at"] = at
        return self._post("/wayback", body)

    def crawl(self, url: str, goal: str = "", limit: int = 30,
              depth: int = 0, semantic: bool = False, concurrency: int = 0,
              webhook_url: Optional[str] = None,
              webhook_secret: Optional[str] = None, wait: bool = True,
              **opts) -> Job:
        """Async crawl → Job handle. wait=False returns without polling.
        webhook_secret signs deliveries HMAC-SHA256 (X-Webx-Signature)."""
        body: dict[str, Any] = {"url": url, "goal": goal, "limit": limit,
                                "depth": depth, "semantic": semantic}
        if concurrency:
            body["concurrency"] = concurrency
        if webhook_url:
            body["webhook_url"] = webhook_url
        if webhook_secret:
            body["webhook_secret"] = webhook_secret
        body.update(opts)
        job = Job(self, self._post("/crawl", body)["id"])
        if wait:
            job.poll()
        return job

    def batch_scrape(self, urls: list, options: Optional[dict] = None,
                     wait: bool = True) -> Job:
        job = Job(self, self._post("/batch/scrape",
                                   {"urls": urls, "options": options or {}})["id"])
        if wait:
            job.poll()
        return job

    # ── tenancy (admin key required) ─────────────────────────────────────

    def create_key(self, name: str, role: str = "", rpm: int = 0,
                   concurrency: int = 0, monthly_units: int = 0) -> dict:
        """Create an API key → {key} shown once. Admin only."""
        return self._post("/keys", {
            "name": name, "role": role, "rpm": rpm,
            "concurrency": concurrency, "monthly_units": monthly_units})

    def keys(self) -> dict:
        return self._get("/keys")

    def delete_key(self, key_id: str) -> dict:
        return self._delete(f"/keys/{key_id}")

    def update_key(self, key_id: str, **fields) -> dict:
        return self._patch(f"/keys/{key_id}", fields)

    def usage(self, days: int = 30, key: Optional[str] = None) -> dict:
        """Metering report — members see own key, admins all."""
        q = f"/usage?days={days}"
        if key:
            q += f"&key={key}"
        return self._get(q)

    def audit(self, limit: int = 500) -> dict:
        """Request audit trail — licensed deployments only."""
        return self._get(f"/audit?limit={limit}")

    def schedule(self, name: str, cron: str, action: str,
                 params: dict, timezone: str = "",
                 webhook_url: str = "") -> dict:
        """Recurring crawl/batch on a cron spec — e.g. "0 9 * * *"."""
        return self._post("/schedules", {
            "name": name, "cron": cron, "action": action, "params": params,
            "timezone": timezone, "webhook_url": webhook_url})

    def schedules(self) -> dict:
        return self._get("/schedules")

    def delete_schedule(self, sched_id: str) -> dict:
        return self._delete(f"/schedules/{sched_id}")

    def jobs(self) -> dict:
        return self._get("/jobs")

    def job_errors(self, job_id: str) -> dict:
        """Failed pages of a job → {errors: [{url, error, status}]}."""
        return self._get(f"/crawl/{job_id}/errors")

    def cancel(self, job_id: str) -> dict:
        return self._post(f"/crawl/{job_id}/cancel", {})

    def ready(self) -> dict:
        """Readiness probe — store + render reachability."""
        return self._get("/ready")

    def version(self) -> dict:
        """Build version + commit."""
        return self._get("/version")

    def doctor(self) -> dict:
        return self._get("/doctor")

    def health(self) -> dict:
        return self._get("/health")

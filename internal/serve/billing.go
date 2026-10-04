package serve

// billing.go — the multi-tenant substrate: per-key auth, metering,
// concurrency, quotas, schedules, audit. Free tier is genuinely useful
// (metering + ≤3 keys + schedules); license-gated extras live behind
// s.license.Allows(...).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kasyap1234/webx/internal/cron"
	"github.com/kasyap1234/webx/internal/license"
	"github.com/kasyap1234/webx/internal/store"
)

// ctxKey carries the resolved API key through the request lifecycle.
type ctxKey struct{}

func keyOf(r *http.Request) *store.APIKey {
	if k, ok := r.Context().Value(ctxKey{}).(*store.APIKey); ok {
		return k
	}
	return nil
}

// envKeyID identifies requests authenticated by the WEBX_API_KEY env var —
// the bootstrap credential that owns the deployment.
const envKeyID = "env"

// ── auth ─────────────────────────────────────────────────────────────────

// resolveKey maps a Bearer token to a key record. Order: env key (always
// admin, the deployment owner), then the api_keys table by sha256.
func (s *Server) resolveKey(r *http.Request) (*store.APIKey, bool) {
	auth := r.Header.Get("Authorization")
	tok := strings.TrimPrefix(auth, "Bearer ")
	if tok == "" || tok == auth {
		return nil, false
	}
	if env := s.envKey; env != "" && tok == env {
		return &store.APIKey{ID: envKeyID, Name: "env-key", Role: "admin"}, true
	}
	if s.store != nil {
		sum := sha256.Sum256([]byte(tok))
		if k, err := s.store.KeyByHash(r.Context(), hex.EncodeToString(sum[:])); err == nil && k != nil {
			return k, !k.Disabled
		}
	}
	return nil, false
}

// authRequired reports whether any credential is configured at all — an
// open server (no env key, no table keys) serves unauthenticated; that's
// the self-host default and stays honest about it.
func (s *Server) authRequired(ctx context.Context) bool {
	if s.envKey != "" {
		return true
	}
	if s.store != nil {
		if keys, err := s.store.ListKeys(ctx); err == nil {
			return len(keys) > 0
		}
	}
	return false
}

// ── key management ───────────────────────────────────────────────────────

// POST /keys {name, role?, rpm?, concurrency?, monthly_units?} → {id, key}
// The plaintext key is returned exactly once — only its sha256 persists.
func (s *Server) keysCreate(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no store configured")
		return
	}
	if !s.canManage(r) {
		writeErr(w, http.StatusForbidden, "key management needs an admin key")
		return
	}
	var req struct {
		Name         string `json:"name"`
		Role         string `json:"role"`
		RPM          int    `json:"rpm"`
		Concurrency  int    `json:"concurrency"`
		MonthlyUnits int64  `json:"monthly_units"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "body must be JSON {name}")
		return
	}
	if existing, err := s.store.ListKeys(r.Context()); err == nil &&
		len(existing) >= license.FreeMaxKeys && !s.license.Allows(license.FeatMultiKey) {
		writeLicenseErr(w, fmt.Sprintf(
			"free tier allows %d API keys — set WEBX_LICENSE for more", license.FreeMaxKeys))
		return
	}
	role := req.Role
	if role != "" && role != "member" && role != "admin" {
		writeErr(w, http.StatusBadRequest, "role must be member|admin")
		return
	}
	if role == "admin" && !s.license.Allows(license.FeatRBAC) {
		writeLicenseErr(w, "admin roles require a license (rbac)")
		return
	}
	raw := "webx_" + randHex(24)
	k := &store.APIKey{
		ID:           store.NewID(),
		Name:         req.Name,
		Prefix:       raw[:13],
		Hash:         hashKey(raw),
		Role:         role,
		RPM:          req.RPM,
		Concurrency:  req.Concurrency,
		MonthlyUnits: req.MonthlyUnits,
	}
	if k.Role == "" {
		k.Role = "member"
	}
	if err := s.store.CreateKey(r.Context(), k); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"success": true,
		"key":     raw, // shown once — store keeps only sha256
		"record":  k,
	})
}

func (s *Server) keysList(w http.ResponseWriter, r *http.Request) {
	if !s.canManage(r) {
		writeErr(w, http.StatusForbidden, "key management needs an admin key")
		return
	}
	keys, err := s.store.ListKeys(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "data": keys})
}

// DELETE /keys/{id} — also PATCH for disable/quota edits.
func (s *Server) keysDelete(w http.ResponseWriter, r *http.Request) {
	if !s.canManage(r) {
		writeErr(w, http.StatusForbidden, "key management needs an admin key")
		return
	}
	if err := s.store.DeleteKey(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

func (s *Server) keysPatch(w http.ResponseWriter, r *http.Request) {
	if !s.canManage(r) {
		writeErr(w, http.StatusForbidden, "key management needs an admin key")
		return
	}
	var req struct {
		Name         *string `json:"name"`
		Role         *string `json:"role"`
		RPM          *int    `json:"rpm"`
		Concurrency  *int    `json:"concurrency"`
		MonthlyUnits *int64  `json:"monthly_units"`
		Disabled     *bool   `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "body must be JSON")
		return
	}
	if req.Role != nil && *req.Role == "admin" && !s.license.Allows(license.FeatRBAC) {
		writeLicenseErr(w, "admin roles require a license (rbac)")
		return
	}
	err := s.store.UpdateKey(r.Context(), r.PathValue("id"), func(k *store.APIKey) {
		if req.Name != nil {
			k.Name = *req.Name
		}
		if req.Role != nil {
			k.Role = *req.Role
		}
		if req.RPM != nil {
			k.RPM = *req.RPM
		}
		if req.Concurrency != nil {
			k.Concurrency = *req.Concurrency
		}
		if req.MonthlyUnits != nil {
			k.MonthlyUnits = *req.MonthlyUnits
		}
		if req.Disabled != nil {
			k.Disabled = *req.Disabled
		}
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

// canManage — env key always admin; table keys need role=admin which itself
// is license-gated, keeping the free tier single-admin honest.
func (s *Server) canManage(r *http.Request) bool {
	k := keyOf(r)
	return k != nil && (k.ID == envKeyID || (k.Role == "admin" && s.license.Allows(license.FeatRBAC)))
}

// ── metering ─────────────────────────────────────────────────────────────

// meterUnit prices a request in credit-units — the number a billing layer
// would invoice. Mirrors market convention: renders & LLM formats cost more.
func meterUnits(path string, status int) int64 {
	if status >= 400 {
		return 0 // failed requests don't bill — same convention as Firecrawl
	}
	switch {
	case path == "/scrape" || path == "/v2/scrape" || path == "/v1/scrape":
		return 1
	case strings.HasSuffix(path, "/search"):
		return 1
	case strings.HasSuffix(path, "/map") || strings.HasSuffix(path, "/wayback"):
		return 1
	case strings.HasSuffix(path, "/extract"):
		return 2 // LLM extraction — pricier like Firecrawl's +4
	default:
		return 1
	}
}

// meterQueue is a buffered single-writer pipeline so metering never
// serializes requests against sqlite.
type meterQueue struct {
	ch chan meterEvent
}

type meterEvent struct {
	keyID, endpoint string
	units, ms       int64
	status          int
}

func newMeterQueue(st store.Store) *meterQueue {
	q := &meterQueue{ch: make(chan meterEvent, 4096)}
	go func() {
		for e := range q.ch {
			_ = st.PutUsage(context.Background(), e.keyID, e.endpoint, e.units, e.ms)
		}
	}()
	return q
}

func (q *meterQueue) record(keyID, path string, units, ms int64) {
	select {
	case q.ch <- meterEvent{keyID, path, units, ms, 200}:
	default: // queue full — drop rather than block a request
	}
}

// GET /usage?days=30[&key=<id>] — the billing substrate's read side.
func (s *Server) usage(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no store configured")
		return
	}
	k := keyOf(r)
	if k == nil {
		writeErr(w, http.StatusUnauthorized, "auth required")
		return
	}
	// members see only their own usage; admins see all
	keyID := r.URL.Query().Get("key")
	if keyID != "" && !s.canManage(r) {
		writeErr(w, http.StatusForbidden, "usage of other keys needs admin")
		return
	}
	if keyID == "" && !s.canManage(r) {
		keyID = k.ID
	}
	days := 30
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)
	since := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")
	rows, err := s.store.UsageReport(r.Context(), since, keyID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"success": true, "data": rows, "since": since, "key": keyID,
	})
}

// GET /audit — license-gated compliance trail.
func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	if !s.license.Allows(license.FeatAudit) {
		writeLicenseErr(w, "audit log requires a license")
		return
	}
	if !s.canManage(r) {
		writeErr(w, http.StatusForbidden, "audit needs an admin key")
		return
	}
	limit := 500
	fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
	evs, err := s.store.ListAudit(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "data": evs})
}

// ── schedules ────────────────────────────────────────────────────────────

// POST /schedules {name, cron, timezone?, action:"crawl"|"batch", params{},
// webhook_url?} — recurring jobs through the same job pipeline.
func (s *Server) schedulesCreate(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusServiceUnavailable, "no store configured")
		return
	}
	var req struct {
		Name     string          `json:"name"`
		Cron     string          `json:"cron"`
		Timezone string          `json:"timezone"`
		Action   string          `json:"action"`
		Params   json.RawMessage `json:"params"`
		Webhook  string          `json:"webhook_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Cron == "" {
		writeErr(w, http.StatusBadRequest, `body must be JSON {cron:"* * * * *", action:"crawl", params:{...}}`)
		return
	}
	if req.Action != "crawl" && req.Action != "batch" {
		writeErr(w, http.StatusBadRequest, `action must be "crawl" or "batch"`)
		return
	}
	cs, err := cron.Parse(req.Cron, req.Timezone)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sc := &store.Schedule{
		ID: store.NewID(), Name: req.Name, Cron: req.Cron, Timezone: req.Timezone,
		Action: req.Action, Params: req.Params, Webhook: req.Webhook,
		Enabled: true, NextRun: cs.Next(time.Now()),
	}
	if err := s.store.CreateSchedule(r.Context(), sc); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "data": sc})
}

func (s *Server) schedulesList(w http.ResponseWriter, r *http.Request) {
	all, err := s.store.ListSchedules(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true, "data": all})
}

func (s *Server) schedulesPatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled *bool  `json:"enabled"`
		Cron    string `json:"cron"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "body must be JSON")
		return
	}
	var next time.Time
	if req.Cron != "" {
		cs, err := cron.Parse(req.Cron, "")
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		next = cs.Next(time.Now())
	}
	err := s.store.UpdateSchedule(r.Context(), r.PathValue("id"), func(sc *store.Schedule) {
		if req.Enabled != nil {
			sc.Enabled = *req.Enabled
		}
		if req.Cron != "" {
			sc.Cron = req.Cron
			sc.NextRun = next
		}
		if req.Name != "" {
			sc.Name = req.Name
		}
	})
	if err != nil {
		writeErr(w, http.StatusNotFound, "schedule not found")
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

func (s *Server) schedulesDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSchedule(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

// RunScheduler fires due schedules — each tick enqueues the schedule's
// action as a normal job so retries/cancel/pages reuse the whole pipeline.
func (s *Server) RunScheduler(ctx context.Context) {
	if s.store == nil {
		return
	}
	// Job retention: finished jobs pile up forever otherwise.
	// WEBX_JOB_TTL sets the horizon (default 168h — a week; "0" disables).
	ttl := 7 * 24 * time.Hour
	if v := os.Getenv("WEBX_JOB_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ttl = d
		}
	}
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	sweeps := 0
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			s.fireDue(ctx, now)
			if sweeps++; ttl > 0 && sweeps%120 == 1 { // first tick, then hourly
				if n, err := s.store.SweepFinished(ctx, ttl); err == nil && n > 0 {
					slog.Info("webx scheduler: swept old jobs", "removed", n, "older_than", ttl)
				}
			}
		}
	}
}

func (s *Server) fireDue(ctx context.Context, now time.Time) {
	all, err := s.store.ListSchedules(ctx)
	if err != nil {
		return
	}
	for _, sc := range all {
		if !sc.Enabled || sc.NextRun.After(now) {
			continue
		}
		params := sc.Params
		if sc.Webhook != "" {
			var p map[string]any
			if json.Unmarshal(params, &p) == nil {
				p["webhook_url"] = sc.Webhook
				params, _ = json.Marshal(p)
			}
		}
		job := &store.Job{ID: store.NewID(), Kind: sc.Action, Params: params}
		var p struct {
			Limit int `json:"limit"`
		}
		_ = json.Unmarshal(params, &p)
		job.Total = p.Limit
		if err := s.store.CreateJob(ctx, job); err != nil {
			slog.Warn("webx scheduler: create job failed", "err", err)
			continue
		}
		cs, err := cron.Parse(sc.Cron, sc.Timezone)
		next := time.Now().Add(time.Hour) // unparseable → retry hourly, never hot-loop
		if err == nil {
			next = cs.Next(now)
		}
		_ = s.store.UpdateSchedule(ctx, sc.ID, func(u *store.Schedule) {
			u.LastRun = now
			u.NextRun = next
		})
		slog.Info("webx scheduler: fired",
			"name", sc.Name, "schedule", sc.ID, "job", job.ID,
			"next", next.Format(time.RFC3339))
	}
}

// ── helpers ──────────────────────────────────────────────────────────────

func hashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

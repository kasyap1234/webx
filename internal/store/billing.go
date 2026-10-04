package store

// billing.go — types for the metering/auth/scheduling layer that turns a
// self-hosted webxd into a multi-tenant service (and feeds a hosted tier's
// billing later). API keys store only the sha256 of the presented secret —
// plaintext keys exist in the response once, then never again.

import (
	"context"
	"encoding/json"
	"time"
)

// APIKey is a tenant credential. Hash is never serialized.
type APIKey struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Prefix       string    `json:"prefix"` // "webx_ab12cd" — for display/key-lookup
	Hash         string    `json:"-"`
	Role         string    `json:"role"` // "member" | "admin" (admin manages keys/schedules/audit)
	RPM          int       `json:"rpm"`
	Concurrency  int       `json:"concurrency"`   // max in-flight (0 = global default)
	MonthlyUnits int64     `json:"monthly_units"` // credit quota (0 = unlimited)
	Disabled     bool      `json:"disabled"`
	CreatedAt    time.Time `json:"created_at"`
}

// UsageRow is one day-bucket of metered usage for one key+endpoint.
type UsageRow struct {
	KeyID    string `json:"key_id"`
	Day      string `json:"day"` // YYYY-MM-DD
	Endpoint string `json:"endpoint"`
	Requests int64  `json:"requests"`
	Units    int64  `json:"units"` // credits — endpoint-weighted
	Ms       int64  `json:"ms"`    // total handler time
}

// Schedule is a recurring job — cron spec + the action payload to enqueue.
type Schedule struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Cron     string          `json:"cron"`
	Timezone string          `json:"timezone"`
	Action   string          `json:"action"` // "crawl" | "batch"
	Params   json.RawMessage `json:"params"`
	Webhook  string          `json:"webhook_url"`
	Enabled  bool            `json:"enabled"`
	NextRun  time.Time       `json:"next_run"`
	LastRun  time.Time       `json:"last_run,omitempty"`
}

// AuditEvent is one authenticated request — the enterprise compliance trail.
type AuditEvent struct {
	KeyID  string    `json:"key_id"`
	Method string    `json:"method"`
	Path   string    `json:"path"`
	Status int       `json:"status"`
	Ms     int64     `json:"ms"`
	At     time.Time `json:"at"`
}

// Billing is the metering/tenancy half of Store — same interface across
// editions, so the lite and durable servers share key management and the
// usage ledger.
type Billing interface {
	// API keys
	CreateKey(ctx context.Context, k *APIKey) error
	ListKeys(ctx context.Context) ([]*APIKey, error)
	KeyByHash(ctx context.Context, hash string) (*APIKey, error)
	UpdateKey(ctx context.Context, id string, mutate func(*APIKey)) error
	DeleteKey(ctx context.Context, id string) error
	// Metering — one row per key/day/endpoint, upserted
	PutUsage(ctx context.Context, keyID, endpoint string, units, ms int64) error
	UsageReport(ctx context.Context, sinceDay string, keyID string) ([]UsageRow, error)
	MonthUnits(ctx context.Context, keyID string) (int64, error)
	// Schedules
	CreateSchedule(ctx context.Context, s *Schedule) error
	ListSchedules(ctx context.Context) ([]*Schedule, error)
	UpdateSchedule(ctx context.Context, id string, mutate func(*Schedule)) error
	DeleteSchedule(ctx context.Context, id string) error
	// Audit
	PutAudit(ctx context.Context, e AuditEvent) error
	ListAudit(ctx context.Context, limit int) ([]AuditEvent, error)
}

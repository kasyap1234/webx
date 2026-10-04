// Package store is the seam between webx's two editions: the lite CLI's
// embedded SQLite and the server's Postgres backend implement the same
// interfaces, so everything above them — routes, crawl engine, extraction —
// is identical code.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Job is one async unit of work (crawl, batch, research, extract, agent —
// the same lifecycle carries them all).
type Job struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`   // "crawl" | "batch" | "research" | "extract" | "agent"
	Status    Status          `json:"status"` // queued|running|done|failed|cancelled
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Total     int             `json:"total"` // expected units (e.g. crawl limit)
	Done      int             `json:"done"`  // completed units
	Params    json.RawMessage `json:"params"`
	Error     string          `json:"error,omitempty"`
	// Result carries the job's primary payload for single-artifact kinds
	// (research report, agent answer) — pages still hold per-URL output.
	Result string `json:"result,omitempty"`
}

// Status is a job's lifecycle position.
type Status string

const (
	Queued    Status = "queued"
	Running   Status = "running"
	Completed Status = "done"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

// JobPage is one fetched page attached to a job.
type JobPage struct {
	URL       string          `json:"url"`
	Title     string          `json:"title,omitempty"`
	Body      string          `json:"body,omitempty"`
	Meta      json.RawMessage `json:"meta,omitempty"` // relevance, depth, final_url, error…
	CreatedAt time.Time       `json:"created_at"`
}

// Store is the durable interface both editions share.
type Store interface {
	// Job lifecycle
	CreateJob(ctx context.Context, j *Job) error
	GetJob(ctx context.Context, id string) (*Job, error)
	UpdateJob(ctx context.Context, id string, mutate func(*Job)) error
	ListJobs(ctx context.Context, limit int) ([]*Job, error)
	// ClaimJob atomically moves one queued job to running — SKIP LOCKED
	// semantics where the backend supports them (pg), a transaction here.
	ClaimJob(ctx context.Context) (*Job, error)
	// Job output
	PutPage(ctx context.Context, jobID string, p JobPage) error
	Pages(ctx context.Context, jobID string, limit, offset int) ([]JobPage, error)
	// RateCheck atomically counts one request for scope inside the current
	// window and reports whether it fits under limit — the shared,
	// multi-replica rate check (approximate: fixed windows, not a sliding
	// log). Implementations must be atomic under concurrent calls.
	RateCheck(ctx context.Context, scope string, limit, windowSec int) (bool, error)
	// SweepFinished deletes terminal jobs (done/failed/cancelled) whose
	// updated_at is older than the cutoff, plus their pages — retention for
	// the jobs store, which otherwise grows without bound. Queued/running
	// jobs are never touched. Returns the number of jobs removed.
	SweepFinished(ctx context.Context, olderThan time.Duration) (int64, error)
	// Billing — keys, metering, schedules, audit (billing.go)
	Billing
	Close() error
}

// ErrNoJob is returned by ClaimJob when the queue is empty.
var ErrNoJob = errors.New("store: no queued job")

// NewID makes a short job id.
func NewID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

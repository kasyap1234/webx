package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Postgres is the durable-edition Store — multi-worker safe via
// FOR UPDATE SKIP LOCKED job claims.
type Postgres struct {
	db            *sql.DB
	lastRateSweep atomic.Int64 // unix window of last rate_windows cleanup
}

// OpenPostgres connects to dsn (postgres://...) and ensures the schema.
func OpenPostgres(dsn string) (*Postgres, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS jobs(
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			total INTEGER NOT NULL DEFAULT 0,
			done INTEGER NOT NULL DEFAULT 0,
			params JSONB NOT NULL DEFAULT '{}',
			error TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS job_pages(
			job_id TEXT NOT NULL REFERENCES jobs(id),
			url TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL DEFAULT '',
			meta JSONB NOT NULL DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
		`CREATE INDEX IF NOT EXISTS job_pages_job ON job_pages(job_id)`,
		`CREATE INDEX IF NOT EXISTS jobs_status ON jobs(status, created_at)`,
		`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS result TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS rate_windows(
			scope TEXT NOT NULL,
			window_start BIGINT NOT NULL,
			count BIGINT NOT NULL DEFAULT 0,
			PRIMARY KEY(scope, window_start))`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			return nil, err
		}
	}
	p := &Postgres{db: db}
	if err := p.initBilling(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Postgres) Close() error { return p.db.Close() }

func (p *Postgres) CreateJob(ctx context.Context, j *Job) error {
	now := time.Now()
	j.CreatedAt, j.UpdatedAt, j.Status = now, now, Queued
	if j.Params == nil {
		j.Params = json.RawMessage(`{}`)
	}
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO jobs(id,kind,status,created_at,updated_at,total,done,params)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		j.ID, j.Kind, string(j.Status), now, now, j.Total, j.Done, j.Params)
	return err
}

func (p *Postgres) GetJob(ctx context.Context, id string) (*Job, error) {
	var j Job
	var status string
	err := p.db.QueryRowContext(ctx,
		`SELECT id,kind,status,created_at,updated_at,total,done,params,error,result
		 FROM jobs WHERE id = $1`, id).
		Scan(&j.ID, &j.Kind, &status, &j.CreatedAt, &j.UpdatedAt, &j.Total, &j.Done, &j.Params, &j.Error, &j.Result)
	if err != nil {
		return nil, err
	}
	j.Status = Status(status)
	return &j, nil
}

func (p *Postgres) UpdateJob(ctx context.Context, id string, mutate func(*Job)) error {
	j, err := p.GetJob(ctx, id)
	if err != nil {
		return err
	}
	mutate(j)
	j.UpdatedAt = time.Now()
	_, err = p.db.ExecContext(ctx,
		`UPDATE jobs SET status=$1, updated_at=$2, total=$3, done=$4, params=$5, error=$6, result=$7 WHERE id=$8`,
		string(j.Status), j.UpdatedAt, j.Total, j.Done, j.Params, j.Error, j.Result, id)
	return err
}

func (p *Postgres) ListJobs(ctx context.Context, limit int) ([]*Job, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT id,kind,status,created_at,updated_at,total,done,params,error,result
		 FROM jobs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		var j Job
		var status string
		if err := rows.Scan(&j.ID, &j.Kind, &status, &j.CreatedAt, &j.UpdatedAt, &j.Total, &j.Done, &j.Params, &j.Error, &j.Result); err != nil {
			return nil, err
		}
		j.Status = Status(status)
		out = append(out, &j)
	}
	return out, rows.Err()
}

// ClaimJob is the real SKIP LOCKED claim — many workers can poll safely.
func (p *Postgres) ClaimJob(ctx context.Context) (*Job, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM jobs WHERE status='queued' ORDER BY created_at
		 LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, ErrNoJob
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE jobs SET status='running', updated_at=now() WHERE id=$1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p.GetJob(ctx, id)
}

func (p *Postgres) PutPage(ctx context.Context, jobID string, pg JobPage) error {
	meta := pg.Meta
	if meta == nil {
		meta = json.RawMessage(`{}`)
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO job_pages(job_id,url,title,body,meta) VALUES ($1,$2,$3,$4,$5)`,
		jobID, pg.URL, pg.Title, pg.Body, meta); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE jobs SET done = done + 1, updated_at=now() WHERE id=$1`, jobID); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Postgres) Pages(ctx context.Context, jobID string, limit, offset int) ([]JobPage, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT url,title,body,meta,created_at FROM job_pages WHERE job_id=$1 ORDER BY created_at LIMIT $2 OFFSET $3`,
		jobID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobPage
	for rows.Next() {
		var pg JobPage
		if err := rows.Scan(&pg.URL, &pg.Title, &pg.Body, &pg.Meta, &pg.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, pg)
	}
	return out, rows.Err()
}

// RateCheck is a fixed-window counter shared by every webxd replica —
// one UPSERT per keyed request, atomic under Postgres. Approximate by
// design (hard window edges, bursts can straddle) — documented tradeoff.
func (p *Postgres) RateCheck(ctx context.Context, scope string, limit, windowSec int) (bool, error) {
	if windowSec <= 0 {
		windowSec = 60
	}
	window := time.Now().Unix() / int64(windowSec)
	var count int64
	err := p.db.QueryRowContext(ctx,
		`INSERT INTO rate_windows(scope,window_start,count) VALUES($1,$2,1)
		 ON CONFLICT(scope,window_start) DO UPDATE SET count=rate_windows.count+1
		 RETURNING count`, scope, window).Scan(&count)
	if err != nil {
		return false, err
	}
	if p.lastRateSweep.Load() != window {
		p.lastRateSweep.Store(window)
		_, _ = p.db.ExecContext(context.Background(),
			`DELETE FROM rate_windows WHERE window_start < $1`, window-2)
	}
	return count <= int64(limit), nil
}

// SweepFinished drops terminal jobs past the cutoff along with their pages.
func (p *Postgres) SweepFinished(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM job_pages WHERE job_id IN
		 (SELECT id FROM jobs WHERE status IN ('done','failed','cancelled') AND updated_at < $1)`,
		cutoff); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM jobs WHERE status IN ('done','failed','cancelled') AND updated_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

var _ Store = (*Postgres)(nil)

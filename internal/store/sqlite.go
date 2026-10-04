package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// SQLite is the lite-edition Store — one file, zero services.
type SQLite struct {
	db            *sql.DB
	lastRateSweep atomic.Int64 // unix minute of last rate_windows cleanup
}

// OpenSQLite opens (or creates) the job store. Empty path → ~/.webx/jobs.db.
func OpenSQLite(path string) (*SQLite, error) {
	if path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, ".webx", "jobs.db")
		} else {
			path = filepath.Join(os.TempDir(), "webx-jobs.db")
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS jobs(
		id TEXT PRIMARY KEY,
		kind TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		total INTEGER NOT NULL DEFAULT 0,
		done INTEGER NOT NULL DEFAULT 0,
		params TEXT NOT NULL,
		error TEXT NOT NULL DEFAULT '')`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS job_pages(
		job_id TEXT NOT NULL,
		url TEXT NOT NULL,
		title TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT '',
		meta TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL)`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS job_pages_job ON job_pages(job_id)`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS jobs_status ON jobs(status, created_at)`); err != nil {
		return nil, err
	}
	// result column landed after the jobs table shipped — ALTER once,
	// tolerate "duplicate column" on existing databases.
	if _, err := db.Exec(`ALTER TABLE jobs ADD COLUMN result TEXT NOT NULL DEFAULT ''`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS rate_windows(
		scope TEXT NOT NULL,
		window_start INTEGER NOT NULL,
		count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY(scope, window_start))`); err != nil {
		return nil, err
	}
	s := &SQLite{db: db}
	if err := s.initBilling(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLite) Close() error { return s.db.Close() }

func (s *SQLite) CreateJob(ctx context.Context, j *Job) error {
	now := time.Now()
	j.CreatedAt, j.UpdatedAt, j.Status = now, now, Queued
	if j.Params == nil {
		j.Params = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs(id,kind,status,created_at,updated_at,total,done,params)
		 VALUES (?,?,?,?,?,?,?,?)`,
		j.ID, j.Kind, j.Status, now.Unix(), now.Unix(), j.Total, j.Done, string(j.Params))
	return err
}

func (s *SQLite) GetJob(ctx context.Context, id string) (*Job, error) {
	var j Job
	var created, updated int64
	var params, status string
	err := s.db.QueryRowContext(ctx,
		`SELECT id,kind,status,created_at,updated_at,total,done,params,error,result
		 FROM jobs WHERE id = ?`, id).
		Scan(&j.ID, &j.Kind, &status, &created, &updated, &j.Total, &j.Done, &params, &j.Error, &j.Result)
	if err != nil {
		return nil, err
	}
	j.Status = Status(status)
	j.CreatedAt, j.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
	j.Params = json.RawMessage(params)
	return &j, nil
}

func (s *SQLite) UpdateJob(ctx context.Context, id string, mutate func(*Job)) error {
	j, err := s.GetJob(ctx, id)
	if err != nil {
		return err
	}
	mutate(j)
	j.UpdatedAt = time.Now()
	_, err = s.db.ExecContext(ctx,
		`UPDATE jobs SET status=?, updated_at=?, total=?, done=?, params=?, error=?, result=? WHERE id=?`,
		string(j.Status), j.UpdatedAt.Unix(), j.Total, j.Done, string(j.Params), j.Error, j.Result, id)
	return err
}

func (s *SQLite) ListJobs(ctx context.Context, limit int) ([]*Job, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,kind,status,created_at,updated_at,total,done,params,error,result
		 FROM jobs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Job
	for rows.Next() {
		var j Job
		var created, updated int64
		var params, status string
		if err := rows.Scan(&j.ID, &j.Kind, &status, &created, &updated, &j.Total, &j.Done, &params, &j.Error, &j.Result); err != nil {
			return nil, err
		}
		j.Status = Status(status)
		j.CreatedAt, j.UpdatedAt = time.Unix(created, 0), time.Unix(updated, 0)
		j.Params = json.RawMessage(params)
		out = append(out, &j)
	}
	return out, rows.Err()
}

// ClaimJob atomically transitions the oldest queued job to running — the
// lite equivalent of Postgres `FOR UPDATE SKIP LOCKED`.
func (s *SQLite) ClaimJob(ctx context.Context) (*Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM jobs WHERE status='queued' ORDER BY created_at LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, ErrNoJob
	}
	if err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE jobs SET status='running', updated_at=? WHERE id=? AND status='queued'`,
		time.Now().Unix(), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNoJob // lost the race — another worker got it
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetJob(ctx, id)
}

func (s *SQLite) PutPage(ctx context.Context, jobID string, p JobPage) error {
	meta := p.Meta
	if meta == nil {
		meta = json.RawMessage(`{}`)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO job_pages(job_id,url,title,body,meta,created_at) VALUES (?,?,?,?,?,?)`,
		jobID, p.URL, p.Title, p.Body, string(meta), time.Now().Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE jobs SET done = done + 1, updated_at=? WHERE id=?`,
		time.Now().Unix(), jobID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) Pages(ctx context.Context, jobID string, limit, offset int) ([]JobPage, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT url,title,body,meta,created_at FROM job_pages WHERE job_id=? ORDER BY created_at LIMIT ? OFFSET ?`,
		jobID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobPage
	for rows.Next() {
		var p JobPage
		var meta string
		var created int64
		if err := rows.Scan(&p.URL, &p.Title, &p.Body, &meta, &created); err != nil {
			return nil, err
		}
		p.Meta = json.RawMessage(meta)
		p.CreatedAt = time.Unix(created, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

// RateCheck is a fixed-window counter shared by every process on this
// store — one UPSERT per keyed request, atomic under WAL + busy_timeout.
// Approximate by design: hard edges at window boundaries, and a burst can
// straddle two windows. That tradeoff is documented and cheap.
func (s *SQLite) RateCheck(ctx context.Context, scope string, limit, windowSec int) (bool, error) {
	if windowSec <= 0 {
		windowSec = 60
	}
	window := time.Now().Unix() / int64(windowSec)
	var count int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO rate_windows(scope,window_start,count) VALUES(?,?,1)
		 ON CONFLICT(scope,window_start) DO UPDATE SET count=count+1
		 RETURNING count`, scope, window).Scan(&count)
	if err != nil {
		return false, err
	}
	// Sweep dead windows at most once a minute — the table stays tiny.
	if m := window; s.lastRateSweep.Load() != m {
		s.lastRateSweep.Store(m)
		_, _ = s.db.ExecContext(context.Background(),
			`DELETE FROM rate_windows WHERE window_start < ?`, window-2)
	}
	return count <= int64(limit), nil
}

// SweepFinished drops terminal jobs past the cutoff along with their pages.
func (s *SQLite) SweepFinished(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan).Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM job_pages WHERE job_id IN
		 (SELECT id FROM jobs WHERE status IN ('done','failed','cancelled') AND updated_at < ?)`,
		cutoff); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM jobs WHERE status IN ('done','failed','cancelled') AND updated_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

var _ Store = (*SQLite)(nil)
var _ = fmt.Sprint // keep fmt for future error wrapping

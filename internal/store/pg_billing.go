package store

// pg_billing.go — Postgres impl of Billing. Identical semantics to
// sqlite_billing.go; syntax deltas are $-params and TIMESTAMPTZ/BOOLEAN.

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

var pgBillingDDL = []string{
	`CREATE TABLE IF NOT EXISTS api_keys(
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		prefix TEXT NOT NULL DEFAULT '',
		hash TEXT NOT NULL UNIQUE,
		role TEXT NOT NULL DEFAULT 'member',
		rpm INTEGER NOT NULL DEFAULT 0,
		concurrency INTEGER NOT NULL DEFAULT 0,
		monthly_units BIGINT NOT NULL DEFAULT 0,
		disabled BOOLEAN NOT NULL DEFAULT false,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
	`CREATE TABLE IF NOT EXISTS usage(
		key_id TEXT NOT NULL,
		day TEXT NOT NULL,
		endpoint TEXT NOT NULL,
		requests BIGINT NOT NULL DEFAULT 0,
		units BIGINT NOT NULL DEFAULT 0,
		ms BIGINT NOT NULL DEFAULT 0,
		PRIMARY KEY(key_id, day, endpoint))`,
	`CREATE TABLE IF NOT EXISTS schedules(
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		cron TEXT NOT NULL,
		timezone TEXT NOT NULL DEFAULT '',
		action TEXT NOT NULL,
		params JSONB NOT NULL DEFAULT '{}',
		webhook TEXT NOT NULL DEFAULT '',
		enabled BOOLEAN NOT NULL DEFAULT true,
		next_run TIMESTAMPTZ NOT NULL,
		last_run TIMESTAMPTZ)`,
	`CREATE TABLE IF NOT EXISTS audit(
		id BIGSERIAL PRIMARY KEY,
		key_id TEXT NOT NULL,
		method TEXT NOT NULL,
		path TEXT NOT NULL,
		status INTEGER NOT NULL,
		ms BIGINT NOT NULL,
		at TIMESTAMPTZ NOT NULL DEFAULT now())`,
	`CREATE INDEX IF NOT EXISTS usage_key_day ON usage(key_id, day)`,
	`CREATE INDEX IF NOT EXISTS audit_at ON audit(at)`,
}

func (p *Postgres) initBilling() error {
	for _, ddl := range pgBillingDDL {
		if _, err := p.db.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}

func (p *Postgres) CreateKey(ctx context.Context, k *APIKey) error {
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now()
	}
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO api_keys(id,name,prefix,hash,role,rpm,concurrency,monthly_units,disabled,created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		k.ID, k.Name, k.Prefix, k.Hash, k.Role, k.RPM, k.Concurrency, k.MonthlyUnits,
		k.Disabled, k.CreatedAt)
	return err
}

func (p *Postgres) ListKeys(ctx context.Context) ([]*APIKey, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id,name,prefix,role,rpm,concurrency,monthly_units,disabled,created_at FROM api_keys ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.Role, &k.RPM, &k.Concurrency,
			&k.MonthlyUnits, &k.Disabled, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

func (p *Postgres) KeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	var k APIKey
	err := p.db.QueryRowContext(ctx,
		`SELECT id,name,prefix,role,rpm,concurrency,monthly_units,disabled,created_at
		 FROM api_keys WHERE hash=$1`, hash).
		Scan(&k.ID, &k.Name, &k.Prefix, &k.Role, &k.RPM, &k.Concurrency,
			&k.MonthlyUnits, &k.Disabled, &k.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	k.Hash = hash
	return &k, nil
}

func (p *Postgres) UpdateKey(ctx context.Context, id string, mutate func(*APIKey)) error {
	all, err := p.ListKeys(ctx)
	if err != nil {
		return err
	}
	var k *APIKey
	for _, r := range all {
		if r.ID == id {
			k = r
			break
		}
	}
	if k == nil {
		return sql.ErrNoRows
	}
	mutate(k)
	_, err = p.db.ExecContext(ctx,
		`UPDATE api_keys SET name=$1, role=$2, rpm=$3, concurrency=$4, monthly_units=$5, disabled=$6 WHERE id=$7`,
		k.Name, k.Role, k.RPM, k.Concurrency, k.MonthlyUnits, k.Disabled, id)
	return err
}

func (p *Postgres) DeleteKey(ctx context.Context, id string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id=$1`, id)
	return err
}

func (p *Postgres) PutUsage(ctx context.Context, keyID, endpoint string, units, ms int64) error {
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO usage(key_id,day,endpoint,requests,units,ms) VALUES ($1,$2,$3,1,$4,$5)
		 ON CONFLICT(key_id,day,endpoint) DO UPDATE SET
		 requests=usage.requests+1, units=usage.units+excluded.units, ms=usage.ms+excluded.ms`,
		keyID, time.Now().UTC().Format("2006-01-02"), endpoint, units, ms)
	return err
}

func (p *Postgres) UsageReport(ctx context.Context, sinceDay, keyID string) ([]UsageRow, error) {
	q := `SELECT key_id,day,endpoint,requests,units,ms FROM usage WHERE day>=$1`
	args := []any{sinceDay}
	if keyID != "" {
		q += ` AND key_id=$2`
		args = append(args, keyID)
	}
	q += ` ORDER BY day DESC, key_id, endpoint`
	rows, err := p.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageRow
	for rows.Next() {
		var u UsageRow
		if err := rows.Scan(&u.KeyID, &u.Day, &u.Endpoint, &u.Requests, &u.Units, &u.Ms); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (p *Postgres) MonthUnits(ctx context.Context, keyID string) (int64, error) {
	var n int64
	err := p.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(units),0) FROM usage WHERE key_id=$1 AND day>=$2`,
		keyID, time.Now().UTC().Format("2006-01")+"-01").Scan(&n)
	return n, err
}

func (p *Postgres) CreateSchedule(ctx context.Context, sc *Schedule) error {
	if sc.Params == nil {
		sc.Params = json.RawMessage(`{}`)
	}
	var last any
	if !sc.LastRun.IsZero() {
		last = sc.LastRun
	}
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO schedules(id,name,cron,timezone,action,params,webhook,enabled,next_run,last_run)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		sc.ID, sc.Name, sc.Cron, sc.Timezone, sc.Action, sc.Params, sc.Webhook,
		sc.Enabled, sc.NextRun, last)
	return err
}

func (p *Postgres) ListSchedules(ctx context.Context) ([]*Schedule, error) {
	rows, err := p.db.QueryContext(ctx,
		`SELECT id,name,cron,timezone,action,params,webhook,enabled,next_run,last_run FROM schedules ORDER BY next_run`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Schedule
	for rows.Next() {
		var sc Schedule
		var last sql.NullTime
		if err := rows.Scan(&sc.ID, &sc.Name, &sc.Cron, &sc.Timezone, &sc.Action,
			&sc.Params, &sc.Webhook, &sc.Enabled, &sc.NextRun, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			sc.LastRun = last.Time
		}
		out = append(out, &sc)
	}
	return out, rows.Err()
}

func (p *Postgres) UpdateSchedule(ctx context.Context, id string, mutate func(*Schedule)) error {
	all, err := p.ListSchedules(ctx)
	if err != nil {
		return err
	}
	var sc *Schedule
	for _, r := range all {
		if r.ID == id {
			sc = r
			break
		}
	}
	if sc == nil {
		return sql.ErrNoRows
	}
	mutate(sc)
	var last any
	if !sc.LastRun.IsZero() {
		last = sc.LastRun
	}
	_, err = p.db.ExecContext(ctx,
		`UPDATE schedules SET name=$1,cron=$2,timezone=$3,action=$4,params=$5,webhook=$6,enabled=$7,next_run=$8,last_run=$9 WHERE id=$10`,
		sc.Name, sc.Cron, sc.Timezone, sc.Action, sc.Params, sc.Webhook,
		sc.Enabled, sc.NextRun, last, id)
	return err
}

func (p *Postgres) DeleteSchedule(ctx context.Context, id string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM schedules WHERE id=$1`, id)
	return err
}

func (p *Postgres) PutAudit(ctx context.Context, e AuditEvent) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO audit(key_id,method,path,status,ms,at) VALUES ($1,$2,$3,$4,$5,$6)`,
		e.KeyID, e.Method, e.Path, e.Status, e.Ms, e.At)
	return err
}

func (p *Postgres) ListAudit(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := p.db.QueryContext(ctx,
		`SELECT key_id,method,path,status,ms,at FROM audit ORDER BY at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.KeyID, &e.Method, &e.Path, &e.Status, &e.Ms, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

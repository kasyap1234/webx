package store

// sqlite_billing.go — SQLite impl of the Billing half: api_keys, usage
// day-buckets, schedules, audit. Same tables exist in pg_billing.go with
// identical semantics.

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const sqliteBillingDDL = `
CREATE TABLE IF NOT EXISTS api_keys(
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	prefix TEXT NOT NULL DEFAULT '',
	hash TEXT NOT NULL UNIQUE,
	role TEXT NOT NULL DEFAULT 'member',
	rpm INTEGER NOT NULL DEFAULT 0,
	concurrency INTEGER NOT NULL DEFAULT 0,
	monthly_units INTEGER NOT NULL DEFAULT 0,
	disabled INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS usage(
	key_id TEXT NOT NULL,
	day TEXT NOT NULL,
	endpoint TEXT NOT NULL,
	requests INTEGER NOT NULL DEFAULT 0,
	units INTEGER NOT NULL DEFAULT 0,
	ms INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY(key_id, day, endpoint));
CREATE TABLE IF NOT EXISTS schedules(
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	cron TEXT NOT NULL,
	timezone TEXT NOT NULL DEFAULT '',
	action TEXT NOT NULL,
	params TEXT NOT NULL DEFAULT '{}',
	webhook TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	next_run INTEGER NOT NULL,
	last_run INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS audit(
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	key_id TEXT NOT NULL,
	method TEXT NOT NULL,
	path TEXT NOT NULL,
	status INTEGER NOT NULL,
	ms INTEGER NOT NULL,
	at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS usage_key_day ON usage(key_id, day);
CREATE INDEX IF NOT EXISTS audit_at ON audit(at);
`

func (s *SQLite) initBilling() error {
	_, err := s.db.Exec(sqliteBillingDDL)
	return err
}

func (s *SQLite) CreateKey(ctx context.Context, k *APIKey) error {
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys(id,name,prefix,hash,role,rpm,concurrency,monthly_units,disabled,created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		k.ID, k.Name, k.Prefix, k.Hash, k.Role, k.RPM, k.Concurrency, k.MonthlyUnits,
		boolInt(k.Disabled), k.CreatedAt.Unix())
	return err
}

func (s *SQLite) ListKeys(ctx context.Context) ([]*APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,name,prefix,role,rpm,concurrency,monthly_units,disabled,created_at FROM api_keys ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKey
	for rows.Next() {
		var k APIKey
		var dis, created int64
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.Role, &k.RPM, &k.Concurrency,
			&k.MonthlyUnits, &dis, &created); err != nil {
			return nil, err
		}
		k.Disabled = dis != 0
		k.CreatedAt = time.Unix(created, 0)
		out = append(out, &k)
	}
	return out, rows.Err()
}

func (s *SQLite) KeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	var k APIKey
	var dis, created int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id,name,prefix,role,rpm,concurrency,monthly_units,disabled,created_at
		 FROM api_keys WHERE hash=?`, hash).
		Scan(&k.ID, &k.Name, &k.Prefix, &k.Role, &k.RPM, &k.Concurrency,
			&k.MonthlyUnits, &dis, &created)
	if err == sql.ErrNoRows {
		return nil, nil // unknown key — auth layer decides what that means
	}
	if err != nil {
		return nil, err
	}
	k.Disabled = dis != 0
	k.CreatedAt = time.Unix(created, 0)
	k.Hash = hash
	return &k, nil
}

func (s *SQLite) UpdateKey(ctx context.Context, id string, mutate func(*APIKey)) error {
	rows, err := s.ListKeys(ctx)
	if err != nil {
		return err
	}
	var k *APIKey
	for _, r := range rows {
		if r.ID == id {
			k = r
			break
		}
	}
	if k == nil {
		return sql.ErrNoRows
	}
	mutate(k)
	_, err = s.db.ExecContext(ctx,
		`UPDATE api_keys SET name=?, role=?, rpm=?, concurrency=?, monthly_units=?, disabled=? WHERE id=?`,
		k.Name, k.Role, k.RPM, k.Concurrency, k.MonthlyUnits, boolInt(k.Disabled), id)
	return err
}

func (s *SQLite) DeleteKey(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id=?`, id)
	return err
}

// PutUsage upserts the key/day/endpoint bucket — one row per endpoint per
// day per key, cheap to aggregate for reports.
func (s *SQLite) PutUsage(ctx context.Context, keyID, endpoint string, units, ms int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO usage(key_id,day,endpoint,requests,units,ms) VALUES (?,?,?,1,?,?)
		 ON CONFLICT(key_id,day,endpoint) DO UPDATE SET
		 requests=requests+1, units=units+excluded.units, ms=ms+excluded.ms`,
		keyID, time.Now().UTC().Format("2006-01-02"), endpoint, units, ms)
	return err
}

func (s *SQLite) UsageReport(ctx context.Context, sinceDay, keyID string) ([]UsageRow, error) {
	q := `SELECT key_id,day,endpoint,requests,units,ms FROM usage WHERE day>=?`
	args := []any{sinceDay}
	if keyID != "" {
		q += ` AND key_id=?`
		args = append(args, keyID)
	}
	q += ` ORDER BY day DESC, key_id, endpoint`
	rows, err := s.db.QueryContext(ctx, q, args...)
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

// MonthUnits sums this calendar month's units for a key — the quota check.
func (s *SQLite) MonthUnits(ctx context.Context, keyID string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(units),0) FROM usage WHERE key_id=? AND day>=?`,
		keyID, time.Now().UTC().Format("2006-01")+"-01").Scan(&n)
	return n, err
}

func (s *SQLite) CreateSchedule(ctx context.Context, sc *Schedule) error {
	if sc.Params == nil {
		sc.Params = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO schedules(id,name,cron,timezone,action,params,webhook,enabled,next_run,last_run)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		sc.ID, sc.Name, sc.Cron, sc.Timezone, sc.Action, string(sc.Params), sc.Webhook,
		boolInt(sc.Enabled), sc.NextRun.Unix(), sc.LastRun.Unix())
	return err
}

func (s *SQLite) ListSchedules(ctx context.Context) ([]*Schedule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,name,cron,timezone,action,params,webhook,enabled,next_run,last_run FROM schedules ORDER BY next_run`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Schedule
	for rows.Next() {
		var sc Schedule
		var en, nx, lr int64
		var params string
		if err := rows.Scan(&sc.ID, &sc.Name, &sc.Cron, &sc.Timezone, &sc.Action,
			&params, &sc.Webhook, &en, &nx, &lr); err != nil {
			return nil, err
		}
		sc.Params = json.RawMessage(params)
		sc.Enabled = en != 0
		sc.NextRun = time.Unix(nx, 0)
		if lr > 0 {
			sc.LastRun = time.Unix(lr, 0)
		}
		out = append(out, &sc)
	}
	return out, rows.Err()
}

func (s *SQLite) UpdateSchedule(ctx context.Context, id string, mutate func(*Schedule)) error {
	all, err := s.ListSchedules(ctx)
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
	_, err = s.db.ExecContext(ctx,
		`UPDATE schedules SET name=?,cron=?,timezone=?,action=?,params=?,webhook=?,enabled=?,next_run=?,last_run=? WHERE id=?`,
		sc.Name, sc.Cron, sc.Timezone, sc.Action, string(sc.Params), sc.Webhook,
		boolInt(sc.Enabled), sc.NextRun.Unix(), sc.LastRun.Unix(), id)
	return err
}

func (s *SQLite) DeleteSchedule(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE id=?`, id)
	return err
}

func (s *SQLite) PutAudit(ctx context.Context, e AuditEvent) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit(key_id,method,path,status,ms,at) VALUES (?,?,?,?,?,?)`,
		e.KeyID, e.Method, e.Path, e.Status, e.Ms, e.At.Unix())
	return err
}

func (s *SQLite) ListAudit(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT key_id,method,path,status,ms,at FROM audit ORDER BY at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var at int64
		if err := rows.Scan(&e.KeyID, &e.Method, &e.Path, &e.Status, &e.Ms, &at); err != nil {
			return nil, err
		}
		e.At = time.Unix(at, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

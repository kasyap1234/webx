package search

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kasyap1234/webx/web/index"
	_ "modernc.org/sqlite"
)

// cacheTTL bounds how long a fused search result is reused — long enough to
// dedupe agent loops and eval runs, short enough that news stays fresh.
const cacheTTL = 10 * time.Minute

var (
	cacheOnce sync.Once
	cacheDB   *sql.DB
	cacheErr  error
)

// cacheKey hashes everything that changes the result set — every filter
// field must participate or a filtered query replays an unfiltered cache
// (and vice versa). Exact is hashed as the *effective* provider query —
// the quoted form — so exact and non-exact never collide.
func cacheKey(req Request) string {
	q := req.Query
	if req.Exact && !strings.HasPrefix(q, `"`) {
		q = `"` + q + `"`
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%t\x00%s\x00%s\x00%s",
		q, req.Num, req.Site,
		strings.Join(req.Providers, ","),
		req.After.Format("2006-01-02"), req.Before.Format("2006-01-02"),
		req.Topic, req.Lang,
		strings.Join(req.Domains, ","), strings.Join(req.ExcludeDomains, ","),
		req.Semantic, req.Depth, req.Collection,
		strings.Join(req.Sources, ","))
	return hex.EncodeToString(h.Sum(nil))
}

// cachedResponse stores a fused Response blob; scrape/rerank run after the
// cache hit so cached rows only persist the search stage.
func cachedResponse(ctx context.Context, key string) (*Response, bool) {
	db, err := cacheConn()
	if err != nil {
		return nil, false
	}
	var blob string
	err = db.QueryRowContext(ctx,
		`SELECT payload FROM search_cache WHERE key = ? AND created_at > strftime('%s','now') - ?`,
		key, int64(cacheTTL.Seconds())).Scan(&blob)
	if err != nil {
		return nil, false
	}
	var resp Response
	if json.Unmarshal([]byte(blob), &resp) != nil {
		return nil, false
	}
	return &resp, true
}

func storeResponse(ctx context.Context, key string, resp *Response) {
	db, err := cacheConn()
	if err != nil {
		return
	}
	blob, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = db.ExecContext(ctx,
		`INSERT OR REPLACE INTO search_cache(key, payload, created_at) VALUES (?,?,strftime('%s','now'))`,
		key, string(blob))
}

func cacheConn() (*sql.DB, error) {
	cacheOnce.Do(func() {
		path := filepath.Join(filepath.Dir(index.DefaultPathEnv()), "cache.db")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			cacheErr = err
			return
		}
		db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
		if err != nil {
			cacheErr = err
			return
		}
		_, cacheErr = db.Exec(`CREATE TABLE IF NOT EXISTS search_cache(
			key        TEXT PRIMARY KEY,
			payload    TEXT NOT NULL,
			created_at INTEGER NOT NULL)`)
		if cacheErr == nil {
			// opportunistic sweep of expired rows, once per process
			_, _ = db.Exec(`DELETE FROM search_cache WHERE created_at < strftime('%s','now') - ?`, int64((24 * time.Hour).Seconds()))
		}
		cacheDB = db
	})
	if cacheErr != nil {
		return nil, cacheErr
	}
	return cacheDB, nil
}

// Package index is webx's own search index: a single SQLite file holding a
// full-text FTS5 corpus of crawled docs pages. This is what makes `webx
// search` ours rather than a pure wrapper over upstream engines.
package index

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Index is a SQLite FTS5-backed page index.
type Index struct {
	db   *sql.DB
	path string
	emb  *embedder // nil unless WEBX_EMBED_MODEL is set
}

// Hit is one index lookup result.
type Hit struct {
	URL     string
	Title   string
	Snippet string
	Score   float64 // bm25 (lower is better in sqlite; negated on return)
}

const schema = `
CREATE VIRTUAL TABLE IF NOT EXISTS pages USING fts5(
	url        UNINDEXED,
	title,
	headings,
	body,
	fetched_at UNINDEXED,
	tokenize  = 'porter unicode61',
	prefix    = '2 3'
);`

// DefaultPath is ~/.webx/index.db — one file holds the whole local index.
func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "webx-index.db"
	}
	return filepath.Join(home, ".webx", "index.db")
}

// DefaultPathEnv honors WEBX_INDEX_DB overrides.
func DefaultPathEnv() string {
	if p := os.Getenv("WEBX_INDEX_DB"); p != "" {
		return p
	}
	return DefaultPath()
}

// Exists reports whether an index file is present at path.
func Exists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Size() > 0
}

// collNameRe keeps collection names filesystem-safe — one word, no paths.
var collNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// PathFor resolves a named collection to its index file — "" means the
// default corpus (WEBX_INDEX_DB or ~/.webx/index.db); "docs" maps to
// ~/.webx/index-docs.db alongside it. Invalid names fall back to the
// default so a flag typo can't write somewhere unexpected.
func PathFor(collection string) string {
	if collection == "" || !collNameRe.MatchString(collection) {
		return DefaultPathEnv()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "webx-index-" + collection + ".db"
	}
	return filepath.Join(home, ".webx", "index-"+collection+".db")
}

// DeleteOlderThan removes pages last fetched before cutoff plus their
// embeddings — the index-GC half of retention. Returns rows deleted.
// Embeddings die by join on url: after deleting pages, orphan vectors
// (urls no longer present) are swept too.
func (i *Index) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// fetched_at is TEXT (strftime returns strings) — CAST or the
	// int-vs-text comparison never matches.
	res, err := tx.ExecContext(ctx,
		`DELETE FROM pages WHERE CAST(fetched_at AS INTEGER) < ?`, cutoff.Unix())
	if err != nil {
		return 0, err
	}
	// Orphaned embeds go too — a vector without a page is dead weight.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM embeds WHERE url NOT IN (SELECT url FROM pages)`); err != nil {
		// embeds table may not exist in this DB — that's fine, GC still worked
		_ = err
	}
	n, _ := res.RowsAffected()
	return int(n), tx.Commit()
}

// Vacuum rebuilds the database file — reclaim space after a big GC.
// Not in the delete path since VACUUM can't run inside a transaction.
func (i *Index) Vacuum(ctx context.Context) error {
	_, err := i.db.ExecContext(ctx, `VACUUM`)
	return err
}

// Open opens or creates the index at path.
func Open(path string) (*Index, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	idx := &Index{db: db, path: path, emb: embedFromEnv()}
	if idx.emb != nil {
		if _, err := db.Exec(embedSchema); err != nil {
			db.Close()
			return nil, fmt.Errorf("init embeds: %w", err)
		}
		if _, err := db.Exec(metaSchema); err != nil {
			db.Close()
			return nil, fmt.Errorf("init meta: %w", err)
		}
		if err := idx.migrateEmbeds(context.Background()); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate embeds: %w", err)
		}
	}
	return idx, nil
}

func (i *Index) Close() error { return i.db.Close() }

// ListHost returns up to limit pages under host, ordered by URL — the raw
// material for a whole-site llms.txt.
func (i *Index) ListHost(ctx context.Context, host string, limit int) ([]Page, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := i.db.QueryContext(ctx,
		`SELECT url, title, body, fetched_at FROM pages
		 WHERE url LIKE ? ESCAPE '\' ORDER BY url LIMIT ?`,
		"%://"+strings.ReplaceAll(host, "%", `\%`)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Page
	for rows.Next() {
		var p Page
		var ts int64
		if err := rows.Scan(&p.URL, &p.Title, &p.Body, &ts); err != nil {
			return nil, err
		}
		p.FetchedAt = time.Unix(ts, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

// Page is one indexed document.
type Page struct {
	URL       string
	Title     string
	Body      string // markdown text
	FetchedAt time.Time
}

var headingRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)

// Put inserts or replaces a page. Headings are extracted into a separate,
// higher-weighted FTS column so section-level matches rank well.
func (i *Index) Put(ctx context.Context, p Page) error {
	var headings []string
	for _, m := range headingRe.FindAllStringSubmatch(p.Body, 50) {
		headings = append(headings, m[1])
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM pages WHERE url = ?`, p.URL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO pages(url, title, headings, body, fetched_at) VALUES (?,?,?,?,strftime('%s','now'))`,
		p.URL, p.Title, strings.Join(headings, " | "), p.Body); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	i.maybeEmbed(ctx, p) // best-effort vector for hybrid search
	return nil
}

// stopwords are dropped before widening — an OR over "a", "is", "the" would
// match every page in the corpus and flood the fused ranking with noise.
// (Lucene's standard English list + question words.)
var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "but": true, "by": true, "for": true, "if": true, "in": true,
	"into": true, "is": true, "it": true, "no": true, "not": true, "of": true,
	"on": true, "or": true, "such": true, "that": true, "the": true,
	"their": true, "then": true, "there": true, "these": true, "they": true,
	"to": true, "was": true, "will": true, "with": true,
	"what": true, "how": true, "why": true, "when": true, "where": true,
	"which": true, "who": true, "do": true, "does": true, "did": true,
	"can": true, "could": true, "i": true, "me": true, "my": true, "you": true,
}

func contentTerms(query string) []string {
	var terms []string
	for t := range strings.FieldsSeq(query) {
		if !stopwords[strings.ToLower(t)] {
			terms = append(terms, t)
		}
	}
	return terms
}

// Query runs an FTS5 MATCH: content terms AND'd first, widening to OR when
// precision starves recall. Weights: title 10, headings 5, body 1.
func (i *Index) Query(ctx context.Context, query string, limit int) ([]Hit, error) {
	terms := contentTerms(query)
	if len(terms) == 0 {
		// Pure-stopword query — a phrase match is the only honest attempt.
		phrase := `"` + strings.ReplaceAll(strings.TrimSpace(query), `"`, `""`) + `"`
		return i.runQuery(ctx, phrase, limit)
	}
	match := matchExpr(terms, " AND ")
	hits, err := i.runQuery(ctx, match, limit)
	if err != nil {
		return nil, err
	}
	if len(hits) < limit/2 && len(terms) > 1 {
		if wider, werr := i.runQuery(ctx, matchExpr(terms, " OR "), limit); werr == nil && len(wider) > len(hits) {
			hits = filterWeakOR(wider, terms, 6)
		}
	}
	return hits, nil
}

// filterWeakOR drops OR-widened hits that matched too few content terms —
// a page mentioning one of six query words is noise, not recall. Keeps at
// most cap hits.
func filterWeakOR(hits []Hit, terms []string, cap int) []Hit {
	minTerms := 2
	if len(terms) < 3 {
		minTerms = 1
	}
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		matched := 0
		corpus := strings.ToLower(h.Title + " " + h.Snippet)
		for _, t := range terms {
			if strings.Contains(corpus, strings.ToLower(t)) {
				matched++
			}
		}
		if matched >= minTerms {
			out = append(out, h)
			if len(out) >= cap {
				break
			}
		}
	}
	return out
}

func matchExpr(terms []string, joiner string) string {
	q := make([]string, len(terms))
	for i, t := range terms {
		q[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
	}
	return strings.Join(q, joiner)
}

func (i *Index) runQuery(ctx context.Context, match string, limit int) ([]Hit, error) {
	rows, err := i.db.QueryContext(ctx, `
		SELECT url, title,
		       snippet(pages, 3, '', '', '…', 40) AS snip,
		       bm25(pages, 0.0, 10.0, 5.0, 1.0, 0.0) AS score
		FROM pages
		WHERE pages MATCH ?
		ORDER BY score
		LIMIT ?`, match, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.URL, &h.Title, &h.Snippet, &h.Score); err != nil {
			return nil, err
		}
		h.Score = -h.Score // bm25 in sqlite: lower = better; flip so higher wins
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// Stats reports basic index counts.
func (i *Index) Stats(ctx context.Context) (pages int, err error) {
	err = i.db.QueryRowContext(ctx, `SELECT count(*) FROM pages`).Scan(&pages)
	return
}

// Get returns a stored page by URL — for diffing current content against
// what the index saw last. Tries trailing-slash and www variants since
// sites canonicalize inconsistently.
func (i *Index) Get(ctx context.Context, u string) (*Page, error) {
	var p Page
	var ts int64
	var err error
	for _, v := range urlVariants(u) {
		err = i.db.QueryRowContext(ctx,
			`SELECT url, title, body, fetched_at FROM pages WHERE url = ?`, v).
			Scan(&p.URL, &p.Title, &p.Body, &ts)
		if err == nil {
			p.FetchedAt = time.Unix(ts, 0)
			return &p, nil
		}
	}
	return nil, err
}

func urlVariants(u string) []string {
	variants := []string{u}
	if strings.HasSuffix(u, "/") {
		variants = append(variants, strings.TrimSuffix(u, "/"))
	} else {
		variants = append(variants, u+"/")
	}
	return variants
}

// LastFetched reports when a URL was last indexed — zero time if unknown.
func (i *Index) LastFetched(ctx context.Context, u string) time.Time {
	var ts int64
	if err := i.db.QueryRowContext(ctx, `SELECT fetched_at FROM pages WHERE url = ?`, u).Scan(&ts); err != nil {
		return time.Time{}
	}
	return time.Unix(ts, 0)
}

// Fresh reports whether a URL was indexed within ttl — used to skip pages
// that don't need re-crawling yet.
func (i *Index) Fresh(ctx context.Context, u string, ttl time.Duration) bool {
	if ttl <= 0 {
		return false
	}
	return time.Since(i.LastFetched(ctx, u)) < ttl
}

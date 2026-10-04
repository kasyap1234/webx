package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// openRaw bypasses Open() — for seeding pre-migration schemas that Open
// would immediately rewrite.
func openRaw(path string) (*sql.DB, error) { return sql.Open("sqlite", path) }

// stubEmbedServer is an OpenAI-compatible /embeddings stub — returns a
// fixed 4-dim vector per input, honoring both single-string and batch
// input shapes.
func stubEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input json.RawMessage `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", 400)
			return
		}
		n := 1
		var arr []any
		if err := json.Unmarshal(req.Input, &arr); err == nil {
			n = len(arr)
		}
		data := make([]map[string]any, n)
		for i := range data {
			data[i] = map[string]any{"embedding": []float32{1, 1, 1, 1}, "index": i}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestChunkEmbedEndToEnd(t *testing.T) {
	srv := stubEmbedServer(t)
	t.Setenv("WEBX_EMBED_MODEL", "test-embed")
	t.Setenv("WEBX_EMBED_BASE", srv.URL)

	ctx := context.Background()
	idx, err := Open(filepath.Join(t.TempDir(), "idx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	body := "# Guide\n\nFirst section about widgets and their care.\n\n## Install\n\nSteps to install on linux with apt.\n\n## Usage\n\nHow to run the widget daily."
	if err := idx.Put(ctx, Page{URL: "https://ex.com/guide", Title: "Guide", Body: body}); err != nil {
		t.Fatal(err)
	}
	// Chunk mode: multiple rows keyed (url, seq>=1) with excerpt text.
	var n int
	if err := idx.db.QueryRow(
		`SELECT count(*) FROM embeds WHERE url = ?`, "https://ex.com/guide").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no chunk vectors stored")
	}
	var txt string
	if err := idx.db.QueryRow(
		`SELECT text FROM embeds WHERE url = ? ORDER BY seq LIMIT 1`, "https://ex.com/guide").Scan(&txt); err != nil {
		t.Fatal(err)
	}
	if txt == "" {
		t.Fatal("chunk text not stored for snippets")
	}
	hits, err := idx.QuerySemantic(ctx, "widget care", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].URL != "https://ex.com/guide" {
		t.Fatalf("semantic hits = %+v", hits)
	}
	// Re-Put: vectors replace, not accumulate (seq count stays stable).
	if err := idx.Put(ctx, Page{URL: "https://ex.com/guide", Title: "Guide", Body: body}); err != nil {
		t.Fatal(err)
	}
	var n2 int
	idx.db.QueryRow(`SELECT count(*) FROM embeds WHERE url = ?`, "https://ex.com/guide").Scan(&n2)
	if n2 != n {
		t.Fatalf("re-index changed vec count %d -> %d", n, n2)
	}
}

// TestModelStampMismatch — the corpus records its embed model; querying
// with a different one must fail loudly, not return garbage scores.
func TestModelStampMismatch(t *testing.T) {
	srv := stubEmbedServer(t)
	t.Setenv("WEBX_EMBED_MODEL", "model-a")
	t.Setenv("WEBX_EMBED_BASE", srv.URL)
	path := filepath.Join(t.TempDir(), "idx.db")
	ctx := context.Background()

	idx, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Put(ctx, Page{URL: "https://ex.com/a", Title: "A", Body: "alpha body text"}); err != nil {
		t.Fatal(err)
	}
	idx.Close()

	// Reopen the same file with a different model → semantic must refuse.
	t.Setenv("WEBX_EMBED_MODEL", "model-b")
	idx2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer idx2.Close()
	if _, err := idx2.QuerySemantic(ctx, "alpha", 5); err == nil {
		t.Fatal("model mismatch should fail, not score garbage")
	}
}

func TestPageModeStillWorks(t *testing.T) {
	srv := stubEmbedServer(t)
	t.Setenv("WEBX_EMBED_MODEL", "test-embed")
	t.Setenv("WEBX_EMBED_BASE", srv.URL)
	t.Setenv("WEBX_INDEX_EMBED", "page")

	ctx := context.Background()
	idx, err := Open(filepath.Join(t.TempDir(), "idx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if err := idx.Put(ctx, Page{URL: "https://ex.com/p", Title: "T", Body: "body text"}); err != nil {
		t.Fatal(err)
	}
	var n, seq int
	if err := idx.db.QueryRow(
		`SELECT count(*), max(seq) FROM embeds WHERE url = ?`, "https://ex.com/p").Scan(&n, &seq); err != nil {
		t.Fatal(err)
	}
	if n != 1 || seq != 0 {
		t.Fatalf("page mode should store exactly one seq=0 vector, got n=%d seq=%d", n, seq)
	}
}

func TestEmbedsMigration(t *testing.T) {
	srv := stubEmbedServer(t)
	t.Setenv("WEBX_EMBED_MODEL", "test-embed")
	t.Setenv("WEBX_EMBED_BASE", srv.URL)

	path := filepath.Join(t.TempDir(), "old.db")
	// Seed the original page-level schema by hand.
	db, err := openRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE embeds(url TEXT PRIMARY KEY, vec BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO embeds VALUES('u', x'00000000')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	idx, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	has, err := idx.embedsHaveSeq(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatal("migration did not upgrade embeds to the chunk schema")
	}
}

func TestBlobRoundTrip(t *testing.T) {
	v := []float32{1.5, -2.25, 0.001, 768.0}
	got := blobToF32(f32ToBlob(v))
	for i := range v {
		if got[i] != v[i] {
			t.Fatalf("roundtrip[%d]: %v != %v", i, got[i], v[i])
		}
	}
}
func TestCosine(t *testing.T) {
	if c := cosine([]float32{1, 0}, []float32{1, 0}); c != 1.0 {
		t.Fatalf("identical cosine = %v", c)
	}
	if c := cosine([]float32{1, 0}, []float32{0, 1}); c != 0.0 {
		t.Fatalf("orthogonal cosine = %v", c)
	}
	if c := cosine([]float32{1, 1}, []float32{1, 1}); c < 0.99 {
		t.Fatalf("same-dir cosine = %v", c)
	}
}

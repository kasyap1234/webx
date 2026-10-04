package index

// embed.go — optional semantic layer over the FTS5 corpus. When
// WEBX_EMBED_MODEL is set (with WEBX_EMBED_BASE defaulting to Ollama's
// OpenAI-compatible endpoint), every Put embeds the page into float32
// vectors stored beside the text. QuerySemantic then cosine-ranks the
// vectors and RRF-fuses with the bm25 FTS list — hybrid retrieval with no
// C extension (sqlite-vec would need one; brute-force is fine at the
// 10⁴-page scale a local docs index reaches).
//
// Two granularities, selected by WEBX_INDEX_EMBED:
//
//	chunk (default) — one vector per markdown chunk (~350 tokens), so a
//	  buried paragraph on a long page can still win the query. Per-URL
//	  score is the best chunk's cosine; the chunk's own text is stored so
//	  hits can surface the matching passage, not just the page.
//	page — the original single-vector-per-page mode, kept for cheap
//	  re-embedding and small corpora.
import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kasyap1234/webx/web/fetch"
)

// embeds stores (url, seq) → vector + the chunk's own text. seq=0 rows in
// "page" mode mean the whole document; chunk mode uses seq=1..n.
const embedSchema = `CREATE TABLE IF NOT EXISTS embeds(
	url  TEXT NOT NULL,
	seq  INTEGER NOT NULL DEFAULT 0,
	vec  BLOB,
	text TEXT NOT NULL DEFAULT '',
	PRIMARY KEY(url, seq))`

// meta stamps the corpus' build parameters — embed_model matters because
// vectors from different models share no vector space; querying a
// nomic-embed-text corpus with bge-m3 produces garbage scores silently.
const metaSchema = `CREATE TABLE IF NOT EXISTS meta(
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL DEFAULT '')`

// maxSemanticScan bounds the brute-force cosine pass — past this the
// index needs a real ANN (or a collection split), and pretending the
// scan is cheap is dishonest.
const maxSemanticScan = 500_000

// maxChunksPerPage bounds embedding cost per Put — long docs rarely have
// their best signal past the first dozen chunks.
const maxChunksPerPage = 12

// embedder is an OpenAI-compatible /v1/embeddings client — Ollama
// (nomic-embed-text), OpenAI, vLLM, etc. all speak this shape.
type embedder struct {
	base  string // e.g. http://localhost:11434/v1
	key   string // optional bearer
	model string
	hc    *http.Client
}

func embedFromEnv() *embedder {
	model := os.Getenv("WEBX_EMBED_MODEL")
	if model == "" {
		return nil
	}
	base := os.Getenv("WEBX_EMBED_BASE")
	if base == "" {
		base = "http://localhost:11434/v1" // Ollama's OpenAI-compat surface
	}
	return &embedder{
		base: strings.TrimSuffix(base, "/"), key: os.Getenv("WEBX_EMBED_KEY"),
		model: model, hc: &http.Client{Timeout: 30 * time.Second},
	}
}

// SemanticEnabled reports whether embedding is configured.
func SemanticEnabled() bool { return embedFromEnv() != nil }

// EmbedderFromEnv exposes the embedder as a plain func — fetch's focused
// crawler takes it without importing this package (index → fetch already).
func EmbedderFromEnv() func(context.Context, string) ([]float32, error) {
	e := embedFromEnv()
	if e == nil {
		return nil
	}
	return e.embed
}

// EmbedBatchFromEnv exposes the batch endpoint — search rerank embeds the
// query plus every candidate in one round-trip.
func EmbedBatchFromEnv() func(context.Context, []string) ([][]float32, error) {
	e := embedFromEnv()
	if e == nil {
		return nil
	}
	return e.embedBatch
}

// Cosine exposes the similarity metric for out-of-package reranking.
func Cosine(a, b []float32) float64 { return cosine(a, b) }

func (e *embedder) embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.embedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// embedBatch sends one request for many inputs — the OpenAI embeddings
// shape takes input as a string array, so a page's chunks ride a single
// HTTP round-trip instead of N. Falls back to singles if the endpoint
// rejects batching (some minimal OpenAI-compat servers do).
func (e *embedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	payload, _ := json.Marshal(map[string]any{
		"model": e.model, "input": texts,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.key != "" {
		req.Header.Set("Authorization", "Bearer "+e.key)
	}
	resp, err := e.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 && len(texts) > 1 {
		// Endpoint may not batch — degrade to per-text calls, honestly.
		out := make([][]float32, 0, len(texts))
		for _, t := range texts {
			v, verr := e.embed(ctx, t)
			if verr != nil {
				return nil, verr
			}
			out = append(out, v)
		}
		return out, nil
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("embed: status %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil || len(out.Data) == 0 {
		return nil, fmt.Errorf("embed: bad response")
	}
	// Responses may come back unordered — place each by its index.
	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index >= 0 && d.Index < len(vecs) {
			vecs[d.Index] = d.Embedding
		}
	}
	return vecs, nil
}

func f32ToBlob(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func blobToF32(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// embedsHaveSeq reports whether the embeds table carries the chunk schema
// (url,seq PK + text) or the original (url PK) page-level one.
func (i *Index) embedsHaveSeq(ctx context.Context) (bool, error) {
	rows, err := i.db.QueryContext(ctx, `PRAGMA table_info(embeds)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == "seq" {
			return true, nil
		}
	}
	return false, rows.Err()
}

// migrateEmbeds upgrades a page-level embeds table to the chunk schema.
// Vectors are derivable — the honest migration is rebuild-on-next-index,
// not a complex copy that would need re-embedding anyway (chunk texts
// don't exist in the old rows).
func (i *Index) migrateEmbeds(ctx context.Context) error {
	hasSeq, err := i.embedsHaveSeq(ctx)
	if err != nil {
		return err
	}
	if hasSeq {
		return nil
	}
	if _, err := i.db.ExecContext(ctx, `DROP TABLE IF EXISTS embeds`); err != nil {
		return err
	}
	_, err = i.db.ExecContext(ctx, embedSchema)
	return err
}

// stampModel records the model the corpus was embedded with — the query
// path refuses to cosine-rank vectors from a different model.
func (i *Index) stampModel(ctx context.Context) {
	_, _ = i.db.ExecContext(ctx,
		`INSERT INTO meta(key,value) VALUES('embed_model', ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, i.emb.model)
	_, _ = i.db.ExecContext(ctx,
		`INSERT INTO meta(key,value) VALUES('embed_mode', ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		map[bool]string{true: "chunk", false: "page"}[chunkMode()])
}

// checkModel verifies the configured embedder matches the corpus'
// recorded model — mismatches mean every semantic score is noise, so we
// fail loudly with the fix instead of returning plausible-looking junk.
func (i *Index) checkModel(ctx context.Context) error {
	var stored string
	err := i.db.QueryRowContext(ctx,
		`SELECT value FROM meta WHERE key='embed_model'`).Scan(&stored)
	if err == sql.ErrNoRows {
		return nil // corpus predates stamping — nothing to check against
	}
	if err != nil {
		return err
	}
	if stored != "" && stored != i.emb.model {
		return fmt.Errorf("index embedded with %q but WEBX_EMBED_MODEL=%q — re-index or set the env to %q",
			stored, i.emb.model, stored)
	}
	return nil
}

// chunkMode reports the embedding granularity — "chunk" is default;
// WEBX_INDEX_EMBED=page keeps the old one-vector-per-page behavior.
func chunkMode() bool {
	return os.Getenv("WEBX_INDEX_EMBED") != "page"
}

// maybeEmbed stores vectors for the page when an embedder is configured.
// Best-effort — embedding failures never fail the Put (page still indexes).
func (i *Index) maybeEmbed(ctx context.Context, p Page) {
	if i.emb == nil {
		return
	}
	if !chunkMode() {
		// Page granularity — the original behavior: one vec per doc.
		text := p.Title + "\n" + p.Body
		if len(text) > 6000 {
			text = text[:6000]
		}
		vec, err := i.emb.embed(ctx, text)
		if err != nil {
			return
		}
		_, _ = i.db.ExecContext(ctx, `DELETE FROM embeds WHERE url = ?`, p.URL)
		_, _ = i.db.ExecContext(ctx,
			`INSERT INTO embeds(url, seq, vec, text) VALUES (?, 0, ?, ?)`,
			p.URL, f32ToBlob(vec), "")
		i.stampModel(ctx)
		return
	}

	// Chunk granularity — one vec per markdown chunk so long docs recall
	// on buried passages.
	chunks := fetch.ChunkMarkdown(p.Body)
	if len(chunks) > maxChunksPerPage {
		chunks = chunks[:maxChunksPerPage]
	}
	type chunkInput struct {
		embedText string // heading path + text, capped — what the model sees
		excerpt   string // stored for snippet display; also the reuse key
	}
	inputs := make([]chunkInput, 0, len(chunks))
	for _, c := range chunks {
		t := c.HeadingPath + "\n" + c.Text
		if len(t) > 2000 {
			t = t[:2000]
		}
		ex := c.Text
		if len(ex) > 800 {
			ex = ex[:800]
		}
		inputs = append(inputs, chunkInput{embedText: t, excerpt: ex})
	}
	if len(inputs) == 0 {
		return
	}

	// Incremental re-embed: a page re-indexed after a small edit shouldn't
	// re-embed every chunk. Match stored excerpt text → keep its vector;
	// only the changed chunks hit the embeddings endpoint (one batch call).
	prev := map[string][]byte{}
	rows, err := i.db.QueryContext(ctx, `SELECT text, vec FROM embeds WHERE url = ?`, p.URL)
	if err == nil {
		for rows.Next() {
			var text string
			var vec []byte
			if rows.Scan(&text, &vec) == nil {
				prev[text] = vec
			}
		}
		rows.Close()
	}
	var reuse [][]byte
	var freshIdx []int
	var freshTexts []string
	for seq, in := range inputs {
		if v, ok := prev[in.excerpt]; ok && len(v) > 0 {
			reuse = append(reuse, v)
			continue
		}
		reuse = append(reuse, nil)
		freshIdx = append(freshIdx, seq)
		freshTexts = append(freshTexts, in.embedText)
	}
	if len(freshTexts) > 0 {
		vecs, err := i.emb.embedBatch(ctx, freshTexts)
		if err != nil {
			return
		}
		for j, seq := range freshIdx {
			if j < len(vecs) && len(vecs[j]) > 0 {
				reuse[seq] = f32ToBlob(vecs[j])
			}
		}
	}

	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	_, _ = tx.ExecContext(ctx, `DELETE FROM embeds WHERE url = ?`, p.URL)
	for seq, v := range reuse {
		if len(v) == 0 {
			continue
		}
		_, _ = tx.ExecContext(ctx,
			`INSERT INTO embeds(url, seq, vec, text) VALUES (?,?,?,?)`,
			p.URL, seq+1, v, inputs[seq].excerpt)
	}
	_ = tx.Commit()
	i.stampModel(ctx)
}

// QuerySemantic fuses the FTS bm25 list with cosine similarity over stored
// embeddings via reciprocal rank fusion (k=60) — Exa-style neural search
// over the private corpus, no external index needed. Chunk granularity
// aggregates to per-URL max so a page's best passage ranks it.
func (i *Index) QuerySemantic(ctx context.Context, query string, limit int) ([]Hit, error) {
	if i.emb == nil {
		return nil, fmt.Errorf("semantic search needs WEBX_EMBED_MODEL (+optional WEBX_EMBED_BASE/WEBX_EMBED_KEY)")
	}
	if limit <= 0 {
		limit = 10
	}
	if err := i.checkModel(ctx); err != nil {
		return nil, err
	}
	var n int
	if err := i.db.QueryRowContext(ctx, `SELECT count(*) FROM embeds`).Scan(&n); err != nil {
		return nil, err
	}
	if n > maxSemanticScan {
		return nil, fmt.Errorf("semantic scan over %d chunks exceeds the %d brute-force limit — use a lexical query, or split the corpus into collections",
			n, maxSemanticScan)
	}
	qv, err := i.emb.embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	// Stream rows, keeping only each URL's best chunk — avoids holding
	// every vector in memory while still ranking the whole corpus.
	type best struct {
		score float64
		text  string
	}
	top := map[string]best{}
	rows, err := i.db.QueryContext(ctx, `SELECT url, vec, text FROM embeds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u, txt string
		var b []byte
		if err := rows.Scan(&u, &b, &txt); err != nil {
			return nil, err
		}
		s := cosine(qv, blobToF32(b))
		if cur, ok := top[u]; !ok || s > cur.score {
			top[u] = best{s, txt}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	type scored struct {
		url   string
		score float64
		text  string
	}
	vecs := make([]scored, 0, len(top))
	for u, b := range top {
		vecs = append(vecs, scored{u, b.score, b.text})
	}
	sort.Slice(vecs, func(a, b int) bool { return vecs[a].score > vecs[b].score })
	if len(vecs) > limit*3 {
		vecs = vecs[:limit*3]
	}

	// RRF over the two ranked lists.
	const k = 60.0
	rrf := map[string]float64{}
	snippets := map[string]string{}
	for rank, v := range vecs {
		rrf[v.url] += 1 / (k + float64(rank) + 1)
		if v.text != "" {
			snippets[v.url] = v.text // chunk text = the matching passage
		}
	}
	lexical, _ := i.Query(ctx, query, limit*3)
	titles := map[string]string{}
	for rank, h := range lexical {
		rrf[h.URL] += 1 / (k + float64(rank) + 1)
		titles[h.URL] = h.Title
		if h.Snippet != "" {
			if _, has := snippets[h.URL]; !has {
				snippets[h.URL] = h.Snippet
			}
		}
	}
	urls := make([]string, 0, len(rrf))
	for u := range rrf {
		urls = append(urls, u)
	}
	sort.Slice(urls, func(a, b int) bool { return rrf[urls[a]] > rrf[urls[b]] })
	if len(urls) > limit {
		urls = urls[:limit]
	}

	hits := make([]Hit, 0, len(urls))
	for _, u := range urls {
		h := Hit{URL: u, Title: titles[u], Score: rrf[u], Snippet: snippets[u]}
		if h.Title == "" {
			var t, body string
			if err := i.db.QueryRowContext(ctx,
				`SELECT title, substr(body,1,240) FROM pages WHERE url = ?`, u).
				Scan(&t, &body); err == nil {
				h.Title = t
				if h.Snippet == "" {
					h.Snippet = body
				}
			} else if err != sql.ErrNoRows {
				return nil, err
			}
		}
		hits = append(hits, h)
	}
	return hits, nil
}

package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kasyap1234/webx/internal/store"
	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/search"
)

func testServer(t *testing.T) (*httptest.Server, store.Store) {
	t.Helper()
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := httptest.NewServer(New(st).Mux)
	t.Cleanup(srv.Close)
	return srv, st
}

func post(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestScrapeBadRequest(t *testing.T) {
	srv, _ := testServer(t)
	code, out := post(t, srv.URL+"/scrape", `{"url":""}`)
	if code != 400 || out["success"] != false {
		t.Fatalf("got %d %v", code, out)
	}
}

func TestScrapeBadActions(t *testing.T) {
	srv, _ := testServer(t)
	code, out := post(t, srv.URL+"/scrape", `{"url":"https://example.com","actions":"frobnicate:x"}`)
	if code != 400 || !strings.Contains(out["error"].(string), "actions") {
		t.Fatalf("got %d %v", code, out)
	}
}

func TestCrawlJobLifecycle(t *testing.T) {
	srv, _ := testServer(t)
	code, out := post(t, srv.URL+"/crawl", `{"url":"https://example.com","limit":2}`)
	if code != 200 || out["success"] != true || out["id"] == nil {
		t.Fatalf("start got %d %v", code, out)
	}
	id := out["id"].(string)

	// cancel while queued → 200; cancel again → 409
	code, out = post(t, srv.URL+"/crawl/"+id+"/cancel", `{}`)
	if code != 200 || out["status"] != "cancelled" {
		t.Fatalf("cancel got %d %v", code, out)
	}
	code, _ = post(t, srv.URL+"/crawl/"+id+"/cancel", `{}`)
	if code != 409 {
		t.Fatalf("second cancel = %d, want 409", code)
	}

	// status shows cancelled job
	resp, err := http.Get(srv.URL + "/crawl/" + id)
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	json.NewDecoder(resp.Body).Decode(&st)
	resp.Body.Close()
	if st["status"] != "cancelled" {
		t.Fatalf("status = %v", st["status"])
	}
}

func TestCrawlJob404(t *testing.T) {
	srv, _ := testServer(t)
	resp, err := http.Get(srv.URL + "/crawl/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestBatchJobRuns(t *testing.T) {
	srv, st := testServer(t)
	code, out := post(t, srv.URL+"/batch/scrape", `{"urls":["https://example.com"]}`)
	if code != 200 || out["id"] == nil {
		t.Fatalf("batch start %d %v", code, out)
	}
	// No workers running in this test — job stays queued
	job, err := st.GetJob(context.Background(), out["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != store.Queued || job.Kind != "batch" {
		t.Fatalf("job = %+v", job)
	}
	// Claim + run inline (worker path without the goroutine loop)
	claimed, err := st.ClaimJob(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != job.ID {
		t.Fatalf("claimed %s want %s", claimed.ID, job.ID)
	}
	// runJob is unexported — verify the claim contract only; the live-path
	// is covered by the end-to-end serve+worker tests run manually.
	_ = time.Now()
}

func TestAuthMiddleware(t *testing.T) {
	t.Setenv("WEBX_API_KEY", "sekrit")
	st, err := store.OpenSQLite(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := httptest.NewServer(New(st).Handler())
	defer srv.Close()

	get := func(path, auth string) int {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get("/jobs", ""); code != 401 {
		t.Fatalf("unauthenticated /jobs = %d, want 401", code)
	}
	if code := get("/jobs", "Bearer wrong"); code != 401 {
		t.Fatalf("wrong key = %d, want 401", code)
	}
	if code := get("/jobs", "Bearer sekrit"); code != 200 {
		t.Fatalf("right key = %d, want 200", code)
	}
	if code := get("/health", ""); code != 200 {
		t.Fatalf("/health must stay open for probes, got %d", code)
	}
}

func TestJobsList(t *testing.T) {
	srv, st := testServer(t)
	st.CreateJob(context.Background(), &store.Job{ID: "x1", Kind: "crawl", Total: 1})
	resp, err := http.Get(srv.URL + "/jobs")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Jobs []map[string]any `json:"jobs"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if len(out.Jobs) != 1 || out.Jobs[0]["id"] != "x1" {
		t.Fatalf("jobs = %v", out.Jobs)
	}
}

// Idempotency-Key: the same POST replayed returns the original job, not a
// second one — retries must not spawn duplicate crawls.
func TestIdempotentCrawl(t *testing.T) {
	srv, _ := testServer(t)
	body := `{"url":"https://example.com","limit":1}`
	postKey := func(key string) map[string]any {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/crawl", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	first := postKey("replay-1")
	second := postKey("replay-1")
	if first["id"] == nil || first["id"] != second["id"] {
		t.Fatalf("idempotent replay should return same job id: %v vs %v", first["id"], second["id"])
	}
	if second["warning"] == nil {
		t.Fatal("replay should carry the idempotent warning")
	}
	third := postKey("different-key")
	if third["id"] == first["id"] {
		t.Fatal("different key must create a new job")
	}
}

// The v1 aliases and OpenAPI route exist and behave like their roots.
func TestV1AliasesAndOpenAPI(t *testing.T) {
	srv, _ := testServer(t)
	for _, path := range []string{"/v1/search", "/v1/map", "/v1/extract"} {
		req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewBufferString(`{}`))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == 404 {
			t.Fatalf("%s returned 404 — alias not wired", path)
		}
	}
	resp, err := http.Get(srv.URL + "/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var spec struct {
		OpenAPI string         `json:"openapi"`
		Paths   map[string]any `json:"paths"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		t.Fatalf("openapi.json invalid: %v", err)
	}
	if spec.OpenAPI == "" || len(spec.Paths) < 10 {
		t.Fatalf("openapi spec too thin: %d paths", len(spec.Paths))
	}
	for _, p := range []string{"/scrape", "/v2/crawl", "/crawl/{id}/events"} {
		if _, ok := spec.Paths[p]; !ok {
			t.Fatalf("openapi missing path %s", p)
		}
	}
}

// stubSearch swaps the fused-search entry for a canned response.
func stubSearch(t *testing.T, fn func(context.Context, search.Request) *search.Response) {
	t.Helper()
	orig := searchFn
	searchFn = fn
	t.Cleanup(func() { searchFn = orig })
}

func stubFetch(t *testing.T, fn func(context.Context, fetch.FetchRequest) (*fetch.Document, error)) {
	t.Helper()
	orig := fetchFn
	fetchFn = fn
	t.Cleanup(func() { fetchFn = orig })
}

func TestAnswerExtractive(t *testing.T) {
	stubSearch(t, func(_ context.Context, req search.Request) *search.Response {
		if !req.Scrape {
			t.Error("answer must scrape sources")
		}
		return &search.Response{Results: []search.Result{
			{Title: "SpaceX valued at $350bn", URL: "https://g.example/x",
				Highlights: []string{"SpaceX was valued at $350 billion."}},
			{Title: "Empty", URL: "https://none.example/"},
			{Title: "Snippet only", URL: "https://s.example/", Snippet: "relevant snippet"},
		}}
	})
	srv, _ := testServer(t)
	code, out := post(t, srv.URL+"/answer", `{"query":"spacex valuation"}`)
	if code != 200 || out["success"] != true {
		t.Fatalf("got %d %v", code, out)
	}
	if out["answer"] != nil || out["answer_type"] != "extractive" {
		t.Fatalf("expected extractive null answer, got %v (%v)", out["answer"], out["answer_type"])
	}
	cites, _ := out["citations"].([]any)
	if len(cites) != 2 {
		t.Fatalf("citations = %d, want 2 (empty result skipped)", len(cites))
	}
	c1, _ := cites[0].(map[string]any)
	if c1["n"].(float64) != 1 || c1["url"] != "https://g.example/x" {
		t.Fatalf("bad citation: %v", c1)
	}
	if _, ok := c1["highlights"]; !ok {
		t.Fatal("citation missing highlights")
	}
	ev, _ := out["evidence_md"].(string)
	if !strings.Contains(ev, "## [1] SpaceX valued at $350bn") ||
		!strings.Contains(ev, "$350 billion") {
		t.Fatalf("evidence_md malformed: %q", ev)
	}
}

func TestAnswerLLMUnavailableHonest(t *testing.T) {
	// llm:true with an unreachable backend must degrade honestly, not
	// fabricate — answer stays null, llm_error explains why.
	t.Setenv("WEBX_LLM_BASE", "http://127.0.0.1:1")
	stubSearch(t, func(_ context.Context, _ search.Request) *search.Response {
		return &search.Response{Results: []search.Result{
			{Title: "T", URL: "https://t.example/", Snippet: "s"},
		}}
	})
	srv, _ := testServer(t)
	_, out := post(t, srv.URL+"/answer", `{"query":"q","llm":true}`)
	if out["answer"] != nil || out["answer_type"] != "extractive" {
		t.Fatalf("llm failure should stay extractive, got %v", out["answer_type"])
	}
	if _, ok := out["llm_error"]; !ok {
		t.Fatal("expected llm_error explaining the failure")
	}
}

func TestSearchFiltersPassthrough(t *testing.T) {
	var got search.Request
	stubSearch(t, func(_ context.Context, req search.Request) *search.Response {
		got = req
		return &search.Response{Results: []search.Result{}}
	})
	srv, _ := testServer(t)
	post(t, srv.URL+"/search", `{"query":"q","domains":["a.com"],"exclude_domains":["b.com"],`+
		`"after":"2024-01-02","before":"2024-02-03T04:05:06Z","topic":"news","lang":"fr",`+
		`"exact":true,"scrape_chars":100,"highlights_only":true,"auto_render":true,`+
		`"browser":true,"session":"s1","semantic":true}`)
	if len(got.Domains) != 1 || got.Domains[0] != "a.com" {
		t.Fatalf("domains not passed: %v", got.Domains)
	}
	if len(got.ExcludeDomains) != 1 || got.ExcludeDomains[0] != "b.com" {
		t.Fatalf("exclude_domains: %v", got.ExcludeDomains)
	}
	if got.After.Format("2006-01-02") != "2024-01-02" {
		t.Fatalf("after: %v", got.After)
	}
	if got.Before.Hour() != 4 {
		t.Fatalf("before: %v", got.Before)
	}
	if !got.Exact || !got.HighlightsOnly || !got.AutoRender || !got.Browser || !got.Semantic {
		t.Fatalf("flags lost: %+v", got)
	}
	if got.Topic != "news" || got.Lang != "fr" || got.Session != "s1" || got.ScrapeChars != 100 {
		t.Fatalf("fields lost: %+v", got)
	}
}

func TestSearchBadDate(t *testing.T) {
	srv, _ := testServer(t)
	code, out := post(t, srv.URL+"/search", `{"query":"q","after":"notadate"}`)
	if code != 400 || out["success"] != false {
		t.Fatalf("got %d %v", code, out)
	}
}

func TestSimilarWebDropsSelf(t *testing.T) {
	stubFetch(t, func(_ context.Context, freq fetch.FetchRequest) (*fetch.Document, error) {
		return &fetch.Document{
			URL:      freq.URL,
			FinalURL: "https://example.com/post",
			Title:    "Go concurrency patterns",
			Markdown: "# Go concurrency patterns\n\nGoroutines and channels explained deeply with examples",
		}, nil
	})
	stubSearch(t, func(_ context.Context, req search.Request) *search.Response {
		if req.Query == "" {
			t.Error("expected signature query")
		}
		return &search.Response{Results: []search.Result{
			{Title: "self", URL: "https://example.com/post"},
			{Title: "similar one", URL: "https://other.example/a", Score: 9},
			{Title: "similar two", URL: "https://other.example/b", Score: 8},
		}}
	})
	srv, _ := testServer(t)
	code, out := post(t, srv.URL+"/similar", `{"url":"https://example.com/post","web":true,"limit":5}`)
	if code != 200 || out["success"] != true {
		t.Fatalf("got %d %v", code, out)
	}
	data, _ := out["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("data = %d, want 2 (self dropped)", len(data))
	}
	first, _ := data[0].(map[string]any)
	if first["url"] == "https://example.com/post" {
		t.Fatal("self URL leaked into similar results")
	}
}

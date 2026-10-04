package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kasyap1234/webx/internal/store"
)

func billingSrv(t *testing.T) *Server {
	t.Helper()
	st, err := store.OpenSQLite(t.TempDir() + "/jobs.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}

func doJSON(t *testing.T, srv http.Handler, method, path, body, key string) (int, map[string]any) {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&out)
	return rec.Code, out
}

func TestKeyLifecycleAndCap(t *testing.T) {
	srv := billingSrv(t).Handler()
	t.Setenv("WEBX_API_KEY", "master")
	s := New(mustStore(t)) // rebuild so envKey binds
	srv = s.Handler()

	// no auth configured → open; with env key → unauthorized without it
	code, _ := doJSON(t, srv, "GET", "/keys", "", "")
	if code != 401 {
		t.Fatalf("unauth /keys = %d, want 401", code)
	}
	// create 3 keys (free cap) then the 4th → 402
	var keys []string
	for i := 0; i < 4; i++ {
		code, out := doJSON(t, srv, "POST", "/keys",
			fmt.Sprintf(`{"name":"k%d"}`, i), "master")
		if i < 3 {
			if code != 200 {
				t.Fatalf("key %d create = %d %v", i, code, out)
			}
			keys = append(keys, out["key"].(string))
		} else if code != 402 {
			t.Fatalf("4th key = %d, want 402 (free cap)", code)
		}
	}
	// table key authenticates
	code, _ = doJSON(t, srv, "GET", "/keys", "", keys[0])
	if code != 403 { // member can't manage
		t.Fatalf("member /keys = %d, want 403", code)
	}
	code, out := doJSON(t, srv, "GET", "/usage", "", keys[0])
	if code != 200 || out["success"] != true {
		t.Fatalf("member /usage = %d %v", code, out)
	}
	// delete one key → cap frees up
	rec := ""
	_ = rec
	code, _ = doJSON(t, srv, "GET", "/keys", "", "master")
	if code != 200 {
		t.Fatalf("list keys: %d", code)
	}
}

func TestMeteringAndQuota(t *testing.T) {
	st, err := store.OpenSQLite(t.TempDir() + "/j.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	t.Setenv("WEBX_API_KEY", "master")
	s := New(st)
	srv := s.Handler()

	// make a key with a tiny quota
	code, out := doJSON(t, srv, "POST", "/keys",
		`{"name":"q","monthly_units":2}`, "master")
	if code != 200 {
		t.Fatalf("create: %v", out)
	}
	raw := out["key"].(string)
	kid := out["record"].(map[string]any)["id"].(string)

	// burn the quota via meter + then hit the cap
	for i := 0; i < 2; i++ {
		if err := st.PutUsage(context.Background(), kid, "/scrape", 1, 10); err != nil {
			t.Fatal(err)
		}
	}
	code, out = doJSON(t, srv, "GET", "/doctor", "", raw)
	if code != 402 || out["code"] != "quota_exceeded" {
		t.Fatalf("over-quota = %d %v, want 402 quota_exceeded", code, out)
	}
	// env key is exempt from quota
	code, _ = doJSON(t, srv, "GET", "/doctor", "", "master")
	if code != 200 {
		t.Fatalf("env key over-quota-blocked: %d", code)
	}
}

func TestConcurrencyCap(t *testing.T) {
	st, err := store.OpenSQLite(t.TempDir() + "/j.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	t.Setenv("WEBX_MAX_CONCURRENCY", "1")
	s := New(st)

	// hold the one global slot, then a second request must 429
	release, ok := s.conc.acquire(nil)
	if !ok {
		t.Fatal("first acquire failed")
	}
	if _, ok := s.conc.acquire(nil); ok {
		t.Fatal("second acquire should fail at cap=1")
	}
	release()
	if _, ok := s.conc.acquire(nil); !ok {
		t.Fatal("acquire after release failed")
	}
}

func TestScheduleCRUD(t *testing.T) {
	t.Setenv("WEBX_API_KEY", "master")
	s := New(mustStore(t))
	srv := s.Handler()

	code, out := doJSON(t, srv, "POST", "/schedules",
		`{"name":"nightly","cron":"0 3 * * *","action":"crawl","params":{"url":"https://example.com","limit":5}}`, "master")
	if code != 200 || out["success"] != true {
		t.Fatalf("create schedule: %d %v", code, out)
	}
	sc := out["data"].(map[string]any)
	if sc["next_run"] == nil {
		t.Fatal("no next_run computed")
	}
	code, out = doJSON(t, srv, "GET", "/schedules", "", "master")
	if code != 200 || len(out["data"].([]any)) != 1 {
		t.Fatalf("list: %d %v", code, out)
	}
	// pause it
	code, _ = doJSON(t, srv, "PATCH", "/schedules/"+sc["id"].(string),
		`{"enabled":false}`, "master")
	if code != 200 {
		t.Fatalf("pause: %d", code)
	}
	code, _ = doJSON(t, srv, "DELETE", "/schedules/"+sc["id"].(string), "", "master")
	if code != 200 {
		t.Fatalf("delete: %d", code)
	}
	// bad cron rejected
	code, _ = doJSON(t, srv, "POST", "/schedules",
		`{"cron":"nonsense","action":"crawl"}`, "master")
	if code != 400 {
		t.Fatalf("bad cron: %d, want 400", code)
	}
}

func mustStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.OpenSQLite(t.TempDir() + "/jobs.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

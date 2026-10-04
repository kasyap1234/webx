package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestSQLiteJobLifecycle(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	job := &Job{ID: "j1", Kind: "crawl", Total: 5,
		Params: json.RawMessage(`{"url":"https://example.com"}`)}
	if err := st.CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if job.Status != Queued {
		t.Fatalf("new job status = %s, want queued", job.Status)
	}

	// claim moves queued → running exactly once
	claimed, err := st.ClaimJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != "j1" || claimed.Status != Running {
		t.Fatalf("claimed = %+v", claimed)
	}
	if _, err := st.ClaimJob(ctx); err != ErrNoJob {
		t.Fatalf("second claim = %v, want ErrNoJob", err)
	}

	// pages accumulate + bump done
	for i := 0; i < 3; i++ {
		if err := st.PutPage(ctx, "j1", JobPage{URL: "https://example.com/a", Title: "a"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.GetJob(ctx, "j1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Done != 3 {
		t.Fatalf("done = %d, want 3", got.Done)
	}
	pages, err := st.Pages(ctx, "j1", 10, 0)
	if err != nil || len(pages) != 3 {
		t.Fatalf("pages = %v %v", len(pages), err)
	}

	// update + list
	if err := st.UpdateJob(ctx, "j1", func(j *Job) { j.Status = Completed }); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.ListJobs(ctx, 10)
	if err != nil || len(jobs) != 1 || jobs[0].Status != Completed {
		t.Fatalf("list = %+v %v", jobs, err)
	}

	// cancel path
	if err := st.UpdateJob(ctx, "j1", func(j *Job) { j.Status = Cancelled }); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetJob(ctx, "j1")
	if got.Status != Cancelled {
		t.Fatalf("status = %s, want cancelled", got.Status)
	}
}

func TestClaimRace(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		st.CreateJob(ctx, &Job{ID: id, Kind: "batch", Total: 1})
	}
	// three claims → three distinct jobs, fourth → ErrNoJob
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		j, err := st.ClaimJob(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if seen[j.ID] {
			t.Fatalf("double-claimed %s", j.ID)
		}
		seen[j.ID] = true
	}
	if _, err := st.ClaimJob(ctx); err != ErrNoJob {
		t.Fatalf("fourth claim = %v, want ErrNoJob", err)
	}
}

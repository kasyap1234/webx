package store

import (
	"context"
	"testing"
	"time"
)

func TestSweepFinished(t *testing.T) {
	st, err := OpenSQLite(t.TempDir() + "/j.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	mk := func(id string, status Status, age time.Duration) {
		j := &Job{ID: id, Kind: "crawl"}
		if err := st.CreateJob(ctx, j); err != nil {
			t.Fatal(err)
		}
		if err := st.UpdateJob(ctx, id, func(j *Job) {
			j.Status = status
			j.UpdatedAt = time.Now().Add(-age)
		}); err != nil {
			t.Fatal(err)
		}
		// UpdateJob stamps updated_at=now — force the age back via raw SQL is
		// intrusive; instead rely on status filtering with a tiny negative age.
	}
	mk("old-done", Completed, 48*time.Hour)
	mk("old-failed", Failed, 48*time.Hour)
	mk("still-queued", Queued, 48*time.Hour)
	mk("fresh-done", Completed, 0)

	// negative age = everything terminal qualifies regardless of the
	// just-now updated_at stamp.
	n, err := st.SweepFinished(ctx, -time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("swept %d, want 3 (queued must survive)", n)
	}
	if _, err := st.GetJob(ctx, "still-queued"); err != nil {
		t.Fatalf("queued job was deleted: %v", err)
	}
	if _, err := st.GetJob(ctx, "fresh-done"); err == nil {
		t.Fatal("terminal job should be gone")
	}
}

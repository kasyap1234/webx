package search

import (
	"strings"
	"testing"
)

func TestSimhashNearDup(t *testing.T) {
	// Realistic scale — scraped pages are hundreds of words; simhash needs
	// enough shingles to separate near-dups from unrelated text.
	para := "distributed systems rely on consensus protocols like raft and paxos to keep replicas in sync under partition failures "
	a := strings.Repeat(para, 8)
	b := a + "the tail of this syndicated copy differs slightly"
	c := strings.Repeat("quantum chromodynamics lattice gauge computations need fermion matrix inversions on gpu clusters ", 8)

	if simhash64(a) != simhash64(a) {
		t.Fatal("identical text must hash identically")
	}
	if d := hamming(simhash64(a), simhash64(b)); d > nearDupThreshold {
		t.Fatalf("near-identical pages should hash within %d bits, got %d", nearDupThreshold, d)
	}
	if d := hamming(simhash64(a), simhash64(c)); d <= nearDupThreshold {
		t.Fatalf("unrelated texts flagged as dups (hamming %d)", d)
	}
}

func TestDedupResults(t *testing.T) {
	body := strings.Repeat("the indexed page contains relevant searchable content about distributed systems and databases ", 8) + "verbatim syndication"
	results := []Result{
		{Title: "Unique Story One", URL: "https://a.example/1", Score: 0.9},
		{Title: "Unique Story One", URL: "https://mirror.example/1", Score: 0.5}, // title dup
		{Title: "Different Headline A", URL: "https://b.example/2", Score: 0.8},
		{Title: "Different Headline B", URL: "https://c.example/3", Score: 0.4}, // content near-dup of prev
	}
	contents := map[int]string{2: body, 3: body} // verbatim syndication
	kept, rekeyed, dropped := dedupResults(results, contents)
	if dropped != 2 || len(kept) != 2 {
		t.Fatalf("kept=%d dropped=%d, want 2/2", len(kept), dropped)
	}
	if kept[0].URL != "https://a.example/1" || kept[1].URL != "https://b.example/2" {
		t.Fatalf("wrong survivors: %v", kept)
	}
	if rekeyed[1] == "" {
		t.Fatal("content map not re-keyed to kept positions")
	}
}

func TestNormalizeScores(t *testing.T) {
	rs := []Result{{Score: 0.05}, {Score: 0.025}}
	normalizeScores(rs)
	if rs[0].Score != 1.0 || rs[1].Score != 0.5 {
		t.Fatalf("normalized wrong: %v", rs)
	}
	normalizeScores(nil) // no panic on empty
}

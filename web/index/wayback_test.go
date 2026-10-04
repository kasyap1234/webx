package index

import (
	"testing"
)

func TestNearestSnapshot(t *testing.T) {
	snaps := []WaybackSnapshot{
		{Timestamp: "20200101000000"},
		{Timestamp: "20210615000000"},
		{Timestamp: "20230101000000"},
	}
	// Nearest to mid-2022 should be the 2023 capture.
	if s := nearestSnapshot(snaps, "20220601"); s.Timestamp != "20230101000000" {
		t.Fatalf("nearest 2022-06 → %s, want 20230101", s.Timestamp)
	}
	// Empty ts → latest.
	if s := nearestSnapshot(snaps, ""); s.Timestamp != "20230101000000" {
		t.Fatalf("latest → %s", s.Timestamp)
	}
	// Exact year prefix pads right — "2020" ≈ 20200101000000.
	if s := nearestSnapshot(snaps, "2020"); s.Timestamp != "20200101000000" {
		t.Fatalf("prefix 2020 → %s", s.Timestamp)
	}
	if s := nearestSnapshot(nil, ""); s != nil {
		t.Fatal("empty snapshots should give nil")
	}
}

func TestWARCRecordShape(t *testing.T) {
	// exercised via fetch.WriteWARCResponse — see resilience tests; here we
	// just sanity-check the timestamp helper doesn't mangle CDX stamps.
	if got := tsToDate("20210615080910"); got != "2021-06-15 08:09:10Z" {
		t.Fatalf("tsToDate = %q", got)
	}
}

package cron

import (
	"testing"
	"time"
)

func TestNext(t *testing.T) {
	cases := []struct {
		expr string
		from string
		want string
	}{
		{"* * * * *", "2026-01-01T00:00:00Z", "2026-01-01T00:01:00Z"},
		{"*/15 * * * *", "2026-01-01T00:02:00Z", "2026-01-01T00:15:00Z"},
		{"0 9 * * *", "2026-01-01T08:00:00Z", "2026-01-01T09:00:00Z"},
		{"0 9 * * *", "2026-01-01T09:00:01Z", "2026-01-02T09:00:00Z"},
		{"30 14 1,15 * *", "2026-01-10T00:00:00Z", "2026-01-15T14:30:00Z"},
		{"0 0 * * 0", "2026-01-01T00:00:00Z", "2026-01-04T00:00:00Z"}, // thu → sun
	}
	for _, c := range cases {
		s, err := Parse(c.expr, "UTC")
		if err != nil {
			t.Fatalf("%q: %v", c.expr, err)
		}
		from, _ := time.Parse(time.RFC3339, c.from)
		got := s.Next(from).UTC().Format(time.RFC3339)
		if got != c.want {
			t.Errorf("%q from %s → %s, want %s", c.expr, c.from, got, c.want)
		}
	}
}

func TestBadCron(t *testing.T) {
	for _, bad := range []string{"* * * *", "x y z a b", "99 * * * *", "*/0 * * * *"} {
		if _, err := Parse(bad, ""); err == nil {
			t.Errorf("%q parsed — want error", bad)
		}
	}
	if _, err := Parse("* * * * *", "Not/AZone"); err == nil {
		t.Error("bad timezone parsed")
	}
}

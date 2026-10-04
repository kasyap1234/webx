// Package cron is a minimal 5-field cron parser — schedules fire on minute
// granularity, which is all server-side recurring crawls need. Supports:
// "*", "*/n", "n", "a-b", "a-b/n", comma lists, and names aren't supported
// (numbers only — keeps the grammar honest).
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a parsed 5-field cron expression (min hour dom mon dow).
type Schedule struct {
	min, hour, dom, mon, dow *field
	tz                       *time.Location
}

type field struct {
	vals  map[int]bool
	min   int
	max   int
	any   bool
	step  int
	start int
}

// Parse compiles a 5-field expression + optional IANA timezone.
func Parse(expr, tzname string) (*Schedule, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("cron: want 5 fields, got %d", len(parts))
	}
	ranges := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	s := &Schedule{tz: time.UTC}
	if tzname != "" {
		loc, err := time.LoadLocation(tzname)
		if err != nil {
			return nil, fmt.Errorf("cron: bad timezone %q", tzname)
		}
		s.tz = loc
	}
	var err error
	if s.min, err = parseField(parts[0], ranges[0]); err != nil {
		return nil, err
	}
	if s.hour, err = parseField(parts[1], ranges[1]); err != nil {
		return nil, err
	}
	if s.dom, err = parseField(parts[2], ranges[2]); err != nil {
		return nil, err
	}
	if s.mon, err = parseField(parts[3], ranges[3]); err != nil {
		return nil, err
	}
	if s.dow, err = parseField(parts[4], ranges[4]); err != nil {
		return nil, err
	}
	return s, nil
}

func parseField(spec string, bounds [2]int) (*field, error) {
	f := &field{vals: map[int]bool{}, min: bounds[0], max: bounds[1]}
	for _, item := range strings.Split(spec, ",") {
		step := 1
		base := item
		if i := strings.Index(item, "/"); i >= 0 {
			base = item[:i]
			n, err := strconv.Atoi(item[i+1:])
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("cron: bad step in %q", item)
			}
			step = n
		}
		lo, hi := bounds[0], bounds[1]
		switch {
		case base == "*":
			f.any = true
		case strings.Contains(base, "-"):
			ab := strings.SplitN(base, "-", 2)
			a, e1 := strconv.Atoi(ab[0])
			b, e2 := strconv.Atoi(ab[1])
			if e1 != nil || e2 != nil || a > b {
				return nil, fmt.Errorf("cron: bad range %q", base)
			}
			lo, hi = a, b
		default:
			v, err := strconv.Atoi(base)
			if err != nil {
				return nil, fmt.Errorf("cron: bad value %q", base)
			}
			lo, hi = v, v
		}
		for v := lo; v <= hi; v += step {
			if v < bounds[0] || v > bounds[1] {
				return nil, fmt.Errorf("cron: %d out of range %d-%d", v, bounds[0], bounds[1])
			}
			f.vals[v] = true
		}
	}
	return f, nil
}

// Next returns the first time strictly after `from` matching the schedule.
// Minute-iteration bounded at ~2 years — a pathological "0 0 29 2 *" still
// resolves.
func (s *Schedule) Next(from time.Time) time.Time {
	t := from.In(s.tz).Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 2*366*24*60; i++ {
		if s.match(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

func (s *Schedule) match(t time.Time) bool {
	_, _, _ = t.Date()
	return s.min.vals[t.Minute()] &&
		s.hour.vals[t.Hour()] &&
		s.dom.vals[t.Day()] &&
		s.mon.vals[int(t.Month())] &&
		s.dow.vals[int(t.Weekday())]
}

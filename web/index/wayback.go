package index

// wayback.go — Wayback Machine integration. The CDX API is free and keyless:
// `webx wayback <url>` lists every capture; `wayback --at <ts>` replays the
// snapshot nearest a timestamp — the dead-page-recovery path when a cited
// URL has gone 404. id_ replay returns the original bytes unrewritten.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const waybackCDX = "https://web.archive.org/cdx/search/cdx"
const waybackReplay = "https://web.archive.org/web/"

// WaybackSnapshot is one archived capture of a URL.
type WaybackSnapshot struct {
	Timestamp string `json:"timestamp"` // yyyyMMddHHmmss
	URL       string `json:"url"`
	Status    string `json:"status"`
	MIME      string `json:"mime"`
	Digest    string `json:"digest"`
}

// WaybackSnapshots lists captures for a URL via the CDX API. from/to are
// optional yyyy[MMdd[HHmmss]] bounds; limit 0 → 200.
func WaybackSnapshots(ctx context.Context, rawURL, from, to string, limit int) ([]WaybackSnapshot, error) {
	if limit <= 0 {
		limit = 200
	}
	return waybackCDXQuery(ctx, rawURL, normalizeTS(from), normalizeTSEnd(to), limit)
}

// nearestSnapshot picks the capture closest to ts (14-digit or prefix).
// Empty ts → the latest capture.
func nearestSnapshot(snaps []WaybackSnapshot, ts string) *WaybackSnapshot {
	if len(snaps) == 0 {
		return nil
	}
	best := &snaps[len(snaps)-1] // CDX order is chronological — last = latest
	if ts == "" {
		return best
	}
	ts = strings.ReplaceAll(ts, "-", "")
	ts = strings.ReplaceAll(ts, ":", "")
	ts = strings.ReplaceAll(ts, " ", "")
	// Partial prefixes pad right BEFORE parsing — "20220601" parses as an
	// int fine, but 8 digits can't compare against 14-digit stamps.
	for len(ts) < 14 {
		ts += "0"
	}
	tn, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return best
	}
	var bestDist int64 = -1
	for i := range snaps {
		sn, err := strconv.ParseInt(snaps[i].Timestamp, 10, 64)
		if err != nil {
			continue
		}
		d := sn - tn
		if d < 0 {
			d = -d
		}
		if bestDist < 0 || d < bestDist {
			bestDist = d
			best = &snaps[i]
		}
	}
	return best
}

// FetchWayback retrieves the capture nearest ts (empty = latest). Uses the
// availability API for real nearest-match (CDX page-of-100 would skew early);
// `id_` replay returns original bytes — no toolbar — so extraction works.
func FetchWayback(ctx context.Context, rawURL, ts string) (body []byte, contentType, snapTS string, err error) {
	snapURL, snapTS, mime, err := nearestCapture(ctx, rawURL, ts)
	if err != nil {
		return nil, "", "", err
	}
	// id_ = original bytes; snapURL already embeds the ts.
	replayURL := waybackReplay + snapTS + "id_/" + snapURL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, replayURL, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("User-Agent", "webx/0.1 (+https://github.com/kasyap1234/webx)")
	resp, err := ccClient.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("wayback replay: status %d", resp.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, "", "", err
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = mime
	}
	return body, ct, snapTS, nil
}

// nearestCapture resolves the capture closest to ts — CDX both directions:
// first capture at-or-after (from=ts) and the last before (to=ts, limit=-1).
// Single-API, works even when archive.org's availability endpoint throttles.
func nearestCapture(ctx context.Context, rawURL, ts string) (origURL, snapTS, mime string, err error) {
	stamp := normalizeTS(ts)
	if stamp == "" {
		snaps, err := WaybackSnapshots(ctx, rawURL, "", "", 1)
		if err != nil {
			return "", "", "", err
		}
		if len(snaps) == 0 {
			return "", "", "", fmt.Errorf("no wayback captures for %s", rawURL)
		}
		s := snaps[len(snaps)-1]
		return s.URL, s.Timestamp, s.MIME, nil
	}
	// after: earliest capture ≥ ts; before: latest capture ≤ ts (limit=-1
	// returns the most recent when `to` bounds the window).
	after, err1 := waybackCDXQuery(ctx, rawURL, stamp, "", 1)
	before, err2 := waybackCDXQuery(ctx, rawURL, "", stamp, -1)
	if err1 != nil && err2 != nil {
		return "", "", "", err1
	}
	cands := append(append([]WaybackSnapshot{}, after...), before...)
	if len(cands) == 0 {
		return "", "", "", fmt.Errorf("no wayback captures for %s near %s", rawURL, ts)
	}
	s := nearestSnapshot(cands, stamp)
	return s.URL, s.Timestamp, s.MIME, nil
}

// waybackCDXQuery wraps the CDX endpoint with from/to bounds — used by both
// the list path and the nearest-resolution path.
func waybackCDXQuery(ctx context.Context, rawURL, from, to string, limit int) ([]WaybackSnapshot, error) {
	q := url.Values{
		"url":    {rawURL},
		"output": {"json"},
		"fl":     {"timestamp,original,statuscode,mimetype,digest"},
		"filter": {"statuscode:200"},
	}
	if limit != 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if from != "" {
		q.Set("from", from)
	}
	if to != "" {
		q.Set("to", to)
	}
	u := waybackCDX + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "webx/0.1 (+https://github.com/kasyap1234/webx)")
	resp, err := ccClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wayback cdx: status %d", resp.StatusCode)
	}
	var rows [][]string
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("wayback cdx: %w", err)
	}
	var out []WaybackSnapshot
	for i, r := range rows {
		if i == 0 || len(r) < 5 {
			continue
		}
		out = append(out, WaybackSnapshot{
			Timestamp: r[0], URL: r[1], Status: r[2], MIME: r[3], Digest: r[4],
		})
	}
	return out, nil
}

// normalizeTS pads a partial timestamp (yyyy → yyyymmddhhmmss center-biased
// at mid-year so "2020" lands mid-2020, not January 1st).
func normalizeTS(ts string) string {
	ts = strings.ReplaceAll(ts, "-", "")
	ts = strings.ReplaceAll(ts, ":", "")
	ts = strings.ReplaceAll(ts, " ", "")
	switch len(ts) {
	case 0:
		return ""
	case 4:
		return ts + "0701" + "000000" // mid-year — least surprising "nearest"
	case 6:
		return ts + "15" + "000000" // mid-month
	case 8:
		return ts + "120000" // noon
	case 10:
		return ts + "0000"
	case 12:
		return ts + "00"
	default:
		return ts
	}
}

// normalizeTSEnd pads a `to` bound to the END of the period — "2020" as a
// bound means "all captures through end of 2020", not mid-year.
func normalizeTSEnd(ts string) string {
	ts = strings.ReplaceAll(ts, "-", "")
	ts = strings.ReplaceAll(ts, ":", "")
	ts = strings.ReplaceAll(ts, " ", "")
	for len(ts) > 0 && len(ts) < 14 {
		ts += "9"
	}
	return ts
}

// tsToDate renders a CDX timestamp human-readable for output.
func tsToDate(ts string) string {
	if t, err := time.Parse("20060102150405", ts); err == nil {
		return t.UTC().Format("2006-01-02 15:04:05Z")
	}
	return ts
}

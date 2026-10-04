package index

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kasyap1234/webx/web/fetch"
)

// Common Crawl bootstrap: instead of live-crawling a docs domain, query the
// CDXJ index for its captured pages and pull WARC records straight from S3.
// Zero load on the target site, ~3B pages of history behind it.

const cdxIndex = "https://index.commoncrawl.org"
const ccData = "https://data.commoncrawl.org"

// CDXRecord is one line of the CDXJ index response.
type CDXRecord struct {
	URL       string `json:"url"`
	Timestamp string `json:"timestamp"`
	Filename  string `json:"filename"`
	Offset    int64  `json:"offset,string"`
	Length    int64  `json:"length,string"`
	Status    string `json:"status"`
	Mime      string `json:"mime"`
	Digest    string `json:"digest"`
}

var ccClient = &http.Client{Timeout: 60 * time.Second}

// LatestCrawl returns the newest crawl collection id (e.g. CC-MAIN-2026-40).
func LatestCrawl(ctx context.Context) (string, error) {
	var cols []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cdxIndex+"/collinfo.json", nil)
	if err != nil {
		return "", err
	}
	resp, err := ccClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("collinfo: status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&cols); err != nil {
		return "", err
	}
	if len(cols) == 0 {
		return "", fmt.Errorf("collinfo: no collections")
	}
	return cols[0].ID, nil
}

// QueryCDX lists captured pages for a URL pattern — `url=example.com/*`
// matches subdomains in SURT order. collapse=urlkey dedupes captures so we
// get one (the latest) per URL.
func QueryCDX(ctx context.Context, crawl, urlPattern string, limit int) ([]CDXRecord, error) {
	if limit <= 0 {
		limit = 500
	}
	q := url.Values{
		"url":      {urlPattern},
		"output":   {"json"},
		"filter":   {"status:200", "mime:text/html"},
		"collapse": {"urlkey"},
		"limit":    {fmt.Sprint(limit)},
	}
	u := fmt.Sprintf("%s/%s-index?%s", cdxIndex, crawl, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := ccClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no captures for this pattern
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cdx: status %d", resp.StatusCode)
	}
	var recs []CDXRecord
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 1<<20), 4<<20)
	for sc.Scan() {
		var r CDXRecord
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.URL != "" {
			recs = append(recs, r)
		}
	}
	return recs, sc.Err()
}

// FetchWARCRecord range-requests one record from the crawl data bucket and
// unwraps WARC → HTTP response → body.
func FetchWARCRecord(ctx context.Context, rec CDXRecord) (body []byte, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ccData+"/"+rec.Filename, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rec.Offset, rec.Offset+rec.Length-1))
	resp, err := ccClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("warc range: status %d", resp.StatusCode)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("warc gzip: %w", err)
	}
	defer gz.Close()
	warc, err := io.ReadAll(io.LimitReader(gz, 16<<20))
	if err != nil {
		return nil, "", err
	}
	return parseWARC(warc)
}

// parseWARC unwraps the WARC envelope then the embedded HTTP response.
// Layout: WARC headers \r\n\r\n HTTP/1.1 status \r\n headers \r\n\r\n body.
func parseWARC(warc []byte) ([]byte, string, error) {
	// Skip WARC headers.
	idx := bytes.Index(warc, []byte("\r\n\r\n"))
	if idx < 0 {
		return nil, "", fmt.Errorf("warc: no header terminator")
	}
	httpMsg := warc[idx+4:]

	// Parse the embedded HTTP response: status line + headers to \r\n\r\n.
	hidx := bytes.Index(httpMsg, []byte("\r\n\r\n"))
	if hidx < 0 {
		return nil, "", fmt.Errorf("warc: no http header terminator")
	}
	head := string(httpMsg[:hidx])
	body := httpMsg[hidx+4:]

	ct := ""
	for _, line := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "content-type:") {
			ct = strings.TrimSpace(line[len("content-type:"):])
		}
	}
	// Chunked transfer-coding inside WARC — decode defensively.
	if strings.Contains(strings.ToLower(head), "transfer-encoding: chunked") {
		if d, err := dechunk(body); err == nil {
			body = d
		}
	}
	return body, ct, nil
}

// dechunk decodes HTTP chunked transfer encoding — CC stores raw responses.
func dechunk(b []byte) ([]byte, error) {
	var out bytes.Buffer
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			return out.Bytes(), nil
		}
		var size int64
		if _, err := fmt.Sscanf(strings.TrimSpace(string(b[:i])), "%x", &size); err != nil || size <= 0 {
			return out.Bytes(), nil
		}
		b = b[i+1:]
		if int64(len(b)) < size+2 {
			out.Write(b)
			return out.Bytes(), nil
		}
		out.Write(b[:size])
		b = b[size+2:]
	}
	return out.Bytes(), nil
}

// IndexFromCC pulls a domain's pages from Common Crawl into the index —
// no requests to the target host at all.
func IndexFromCC(ctx context.Context, domain string, opts IndexOptions, logf func(string, ...any)) (*Stats, error) {
	crawl := opts.CCrawl
	if crawl == "" {
		var err error
		if crawl, err = LatestCrawl(ctx); err != nil {
			return nil, fmt.Errorf("latest crawl: %w", err)
		}
	}
	logf("using crawl %s", crawl)
	if opts.Limit <= 0 {
		opts.Limit = defaultLimit
	}
	if opts.DBPath == "" {
		opts.DBPath = DefaultPathEnv()
	}

	pattern := domain
	if !strings.HasSuffix(pattern, "/*") {
		pattern += "/*"
	}
	logf("querying CDX for %s …", pattern)
	recs, err := QueryCDX(ctx, crawl, pattern, opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("cdx query: %w", err)
	}
	logf("%d captures found, extracting…", len(recs))

	idx, err := Open(opts.DBPath)
	if err != nil {
		return nil, err
	}
	defer idx.Close()

	var stats Stats
	stats.Discovered = len(recs)
	conc := opts.Concurrency
	if conc <= 0 {
		conc = defaultConcurrency
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, conc)

	for _, rec := range recs {
		select {
		case <-ctx.Done():
			wg.Wait()
			return &stats, nil
		default:
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(rec CDXRecord) {
			defer wg.Done()
			defer func() { <-sem }()
			body, ct, err := FetchWARCRecord(ctx, rec)
			if err != nil {
				mu.Lock()
				stats.Failed++
				mu.Unlock()
				return
			}
			doc, err := fetch.ExtractFromBody(body, ct, rec.URL)
			if err != nil {
				mu.Lock()
				stats.Failed++
				mu.Unlock()
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if perr := idx.Put(ctx, Page{URL: doc.FinalURL, Title: doc.Title, Body: doc.Markdown}); perr != nil {
				stats.Failed++
				return
			}
			stats.Pages++
			if stats.Pages%25 == 0 {
				logf("indexed %d/%d pages…", stats.Pages, len(recs))
			}
		}(rec)
	}
	wg.Wait()
	return &stats, nil
}

package fetch

// warc.go — WARC/1.0 export (ISO 28500). One `response` record per URL:
// the format every archiver (webrecorder, wget --warc, Heritrix) replays,
// so `webx archive` output opens in replayweb.page or indexes elsewhere.
// We write the real wire response — status line + headers + body.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WriteWARCResponse appends one WARC response record for a completed
// fetch — statusLine/headers/body are what the wire returned.
func WriteWARCResponse(w io.Writer, targetURL string, status int, header http.Header, body []byte) error {
	var httpMsg bytes.Buffer
	fmt.Fprintf(&httpMsg, "HTTP/1.1 %d %s\r\n", status, http.StatusText(status))
	header.Del("Content-Length") // recompute — chunked arrives decoded
	header.Write(&httpMsg)
	fmt.Fprintf(&httpMsg, "Content-Length: %d\r\n\r\n", len(body))
	httpMsg.Write(body)

	hdr := fmt.Sprintf(
		"WARC/1.0\r\n"+
			"WARC-Type: response\r\n"+
			"WARC-Target-URI: %s\r\n"+
			"WARC-Date: %s\r\n"+
			"WARC-Record-ID: <urn:uuid:%s>\r\n"+
			"Content-Type: application/http; msgtype=response\r\n"+
			"Content-Length: %d\r\n\r\n",
		targetURL, time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		warcUUID(), httpMsg.Len())
	if _, err := w.Write(append([]byte(hdr), httpMsg.Bytes()...)); err != nil {
		return err
	}
	_, err := w.Write([]byte("\r\n\r\n")) // record separator
	return err
}

// warcUUID mints an RFC 4122 v4 uuid without pulling a dep.
func warcUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// FetchWARC performs a raw GET preserving the wire response (status,
// headers, body) for archival — unlike Fetch it skips extraction entirely.
func FetchWARC(ctx context.Context, rawURL string, timeout time.Duration, browser, skipTLS bool, session, proxy, cookieFile string) (status int, h http.Header, body []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	if browser {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	}
	signBotRequest(req)
	if cookieFile != "" {
		if loaded, lerr := LoadCookieFile(cookieFile); lerr == nil {
			for _, c := range cookiesFor(loaded, req.URL) {
				req.AddCookie(c)
			}
		}
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	resp, err := ClientTLS(browser, session, timeout, proxy, skipTLS).Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return 0, nil, nil, err
	}
	return resp.StatusCode, resp.Header, body, nil
}

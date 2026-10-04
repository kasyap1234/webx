package fetch

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// Client returns an http.Client for page fetches. browser=true puts a real
// Chrome TLS fingerprint on the wire (uTLS) — the difference between a
// net/http ClientHello (blocked by Cloudflare/Akamai on sight) and traffic
// that passes first-line bot checks. session names a persistent cookie jar
// under ~/.webx/sessions/ so a solved challenge survives the process.
// proxy routes the request through an HTTP(S) proxy — "" falls back to
// WEBX_PROXY, then no proxy. (The uTLS browser path dials raw TCP and
// ignores proxy — honest limitation, proxy users want the http tier.)
func Client(browser bool, session string, timeout time.Duration, proxy string) *http.Client {
	return ClientTLS(browser, session, timeout, proxy, false)
}

// ClientTLS is Client with an optional skip-verify — Firecrawl's
// skipTlsVerification, for dev/staging targets with bad certs.
func ClientTLS(browser bool, session string, timeout time.Duration, proxy string, skipTLS bool) *http.Client {
	var tr http.RoundTripper
	if browser {
		tr = &browserTransport{insecure: skipTLS}
	} else {
		t := &http.Transport{
			MaxIdleConns:        16,
			IdleConnTimeout:     60 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		}
		if skipTLS {
			t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
		p := proxy
		if p == "" {
			p = os.Getenv("WEBX_PROXY")
		}
		if p != "" {
			if pu, err := url.Parse(p); err == nil {
				t.Proxy = http.ProxyURL(pu)
			}
		}
		tr = t
	}
	c := &http.Client{Transport: tr, Timeout: timeout}
	if session != "" {
		c.Jar = openSessionJar(session)
	}
	return c
}

// browserTransport is a RoundTripper that handshakes TLS with a Chrome
// fingerprint then speaks whichever protocol ALPN negotiated — h2 through
// x/net/http2, http/1.1 by hand. No pooling: each request is a fresh conn
// (one extra handshake per fetch, acceptable on the escalation path).
type browserTransport struct{ insecure bool }

func (t *browserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	addr := req.URL.Host
	if _, _, err := net.SplitHostPort(addr); err != nil {
		if req.URL.Scheme == "http" {
			addr += ":80"
		} else {
			addr += ":443"
		}
	}
	conn, err := dialChromeTLSInsecure(req.Context(), "tcp", addr, t.insecure)
	if err != nil {
		return nil, err
	}
	// Chrome speaks h2 on ~every modern host — when ALPN negotiated it,
	// drive the conn with x/net/http2 instead of hand-rolled http/1.1.
	// Both fingerprint-faithful (real Chrome sends h2 in ALPN) and faster.
	if uconn, ok := conn.(*utls.UConn); ok &&
		uconn.ConnectionState().NegotiatedProtocol == "h2" {
		return http2RoundTrip(conn, req)
	}
	return http1RoundTrip(conn, req)
}

// h2Transport mints ClientConns over pre-dialed uTLS sockets. It MUST come
// from ConfigureTransports — a bare &http2.Transport{} has a nil internal
// h1 transport and NewClientConn segfaults on it under concurrency.
var h2Transport = func() *http2.Transport {
	t2, err := http2.ConfigureTransports(&http.Transport{MaxIdleConns: 16})
	if err != nil {
		return &http2.Transport{} // unreachable on a fresh transport
	}
	return t2
}()

func http2RoundTrip(conn net.Conn, req *http.Request) (*http.Response, error) {
	cc, err := h2Transport.NewClientConn(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := cc.RoundTrip(req)
	if err != nil {
		cc.Close()
		return nil, err
	}
	resp.Body = &h2Body{ReadCloser: resp.Body, cc: cc}
	return resp, nil
}

// h2Body closes the whole HTTP/2 conn with the body — no pooling here,
// so a stream's end is the connection's end.
type h2Body struct {
	io.ReadCloser
	cc *http2.ClientConn
}

func (b *h2Body) Close() error {
	err := b.ReadCloser.Close()
	b.cc.Close()
	return err
}

// http1RoundTrip writes one HTTP/1.1 request and reads the response —
// minimal, no keep-alive (conn dies with the body).
func http1RoundTrip(conn net.Conn, req *http.Request) (*http.Response, error) {
	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "gzip")
	}
	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	var body io.ReadCloser = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			conn.Close()
			return nil, err
		}
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		body = gz
	}
	resp.Body = &connBody{ReadCloser: body, conn: conn}
	return resp, nil
}

// connBody closes the underlying connection when the body is done — no
// pooling on the browser path.
type connBody struct {
	io.ReadCloser
	conn net.Conn
}

func (b *connBody) Close() error {
	b.ReadCloser.Close()
	return b.conn.Close()
}

// dialChromeTLS completes a TLS handshake impersonating desktop Chrome —
// the JA3/JA4 fingerprint anti-bot layers actually check.
func dialChromeTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	return dialChromeTLSInsecure(ctx, network, addr, false)
}

func dialChromeTLSInsecure(ctx context.Context, network, addr string, insecure bool) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	host, _, _ := net.SplitHostPort(addr)
	uconn := utls.UClient(conn, &utls.Config{ServerName: host, InsecureSkipVerify: insecure}, utls.HelloChrome_Auto)
	// Advertise h2+http/1.1 like a real Chrome hello — JA3/JA4 hash the
	// extension *types* and their order, not the ALPN list contents, so
	// this stays fingerprint-faithful while unlocking h2 on the wire.
	if err := uconn.BuildHandshakeState(); err != nil {
		conn.Close()
		return nil, err
	}
	for _, e := range uconn.Extensions {
		if alpn, ok := e.(*utls.ALPNExtension); ok {
			alpn.AlpnProtocols = []string{"h2", "http/1.1"}
		}
	}
	if err := uconn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("utls handshake %s: %w", host, err)
	}
	return uconn, nil
}

// sessionsDir is where named cookie jars persist across runs.
func sessionsDir() string {
	if d, err := os.UserHomeDir(); err == nil {
		return filepath.Join(d, ".webx", "sessions")
	}
	return filepath.Join(os.TempDir(), "webx-sessions")
}

type sessionEntry struct {
	Domain  string         `json:"domain"`
	Cookies []*http.Cookie `json:"cookies"`
}

// sessionJar is a minimal persistent http.CookieJar — enough to keep a
// cf_clearance or login cookie alive between runs. Not RFC-complete: domain
// matching is suffix-based, secure/httpOnly flags are advisory.
type sessionJar struct {
	path string
	mu   sync.Mutex
	jars map[string][]*http.Cookie
}

func openSessionJar(name string) *sessionJar {
	j := &sessionJar{
		path: filepath.Join(sessionsDir(), name+".json"),
		jars: map[string][]*http.Cookie{},
	}
	if data, err := os.ReadFile(j.path); err == nil {
		var entries []sessionEntry
		if json.Unmarshal(data, &entries) == nil {
			for _, e := range entries {
				j.jars[e.Domain] = e.Cookies
			}
		}
	}
	return j
}

func (j *sessionJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	host := u.Hostname()
	for _, c := range cookies {
		dom := host
		if c.Domain != "" {
			dom = strings.TrimPrefix(c.Domain, ".")
		}
		replaced := false
		for i, old := range j.jars[dom] {
			if old.Name == c.Name {
				j.jars[dom][i] = c
				replaced = true
				break
			}
		}
		if !replaced {
			j.jars[dom] = append(j.jars[dom], c)
		}
	}
	j.save()
}

func (j *sessionJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	host := u.Hostname()
	var out []*http.Cookie
	for dom, cookies := range j.jars {
		if host == dom || strings.HasSuffix(host, "."+dom) {
			now := time.Now()
			for _, c := range cookies {
				if c.Expires.IsZero() || c.Expires.After(now) {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

func (j *sessionJar) save() {
	entries := make([]sessionEntry, 0, len(j.jars))
	for dom, cookies := range j.jars {
		var live []*http.Cookie
		for _, c := range cookies {
			if c.Expires.IsZero() || c.Expires.After(time.Now()) {
				live = append(live, c)
			}
		}
		if len(live) > 0 {
			entries = append(entries, sessionEntry{Domain: dom, Cookies: live})
		}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(j.path), 0o700)
	os.WriteFile(j.path, data, 0o600)
}

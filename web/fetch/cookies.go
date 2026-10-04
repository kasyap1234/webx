package fetch

// cookies.go — Netscape cookie-file import. The `cookies.txt` export format
// (curl/browsers/extensions all emit it) is how agents carry an SSO'd
// session into a fetch without re-doing the login dance.
//
// Format per line, tab-separated:
//   domain  include_subdomains  path  secure  expires_epoch  name  value
// `#HttpOnly_`-prefixed lines are cookies too; other `#` lines are comments.

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// LoadCookieFile parses a Netscape cookies.txt into http cookies.
func LoadCookieFile(path string) ([]*http.Cookie, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseCookies(string(data), path)
}

// ParseCookies parses Netscape-format cookie text — also the remote-API
// shape (clients send content, never server-side paths).
func ParseCookies(text, source string) ([]*http.Cookie, error) {
	var out []*http.Cookie
	for ln, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(line, "#HttpOnly_") {
			line = line[len("#HttpOnly_"):]
		} else if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 7 {
			return nil, fmt.Errorf("cookies %s line %d: want 7 tab-separated fields, got %d", source, ln+1, len(f))
		}
		c := &http.Cookie{
			Name:   f[5],
			Value:  f[6],
			Path:   f[2],
			Domain: strings.TrimPrefix(f[0], "."),
			Secure: strings.EqualFold(f[3], "true"),
		}
		if exp, err := strconv.ParseInt(f[4], 10, 64); err == nil && exp > 0 {
			c.Expires = time.Unix(exp, 0)
		}
		out = append(out, c)
	}
	return out, nil
}

// cookiesFor filters a cookie set to those this URL would send — suffix
// domain match (cookie ".x.com" covers a.x.com), expiry-aware.
func cookiesFor(cookies []*http.Cookie, u *url.URL) []*http.Cookie {
	host := u.Hostname()
	var out []*http.Cookie
	now := time.Now()
	for _, c := range cookies {
		if !c.Expires.IsZero() && c.Expires.Before(now) {
			continue
		}
		d := strings.TrimPrefix(c.Domain, ".")
		if host == d || strings.HasSuffix(host, "."+d) {
			out = append(out, c)
		}
	}
	return out
}

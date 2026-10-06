package fetch

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// SameOrWWW reports whether l's host equals host ignoring a leading
// www. on either side — the includeSubdomains:false boundary for map.
func SameOrWWW(l, host string) bool {
	u, err := url.Parse(l)
	if err != nil {
		return false
	}
	h := strings.TrimPrefix(u.Hostname(), "www.")
	return h == strings.TrimPrefix(host, "www.")
}

// MapLinks discovers site URLs by fetching pages and harvesting same-host
// links — the no-sitemap discovery path (Firecrawl map ignoreSitemap).
// Two levels deep: the base page, then one level of discovered links.
// fetchFn is injected so callers with their own transport/stub keep it
// (serve's test seam); the CLI passes Fetch.
func MapLinks(ctx context.Context, base *url.URL, limit int, subdomains bool,
	fetchFn func(context.Context, FetchRequest) (*Document, error)) ([]string, error) {
	if limit <= 0 {
		limit = 300
	}
	seen := map[string]bool{base.String(): true}
	var out []string
	baseHost := strings.TrimPrefix(base.Hostname(), "www.")
	inScope := func(l string) bool {
		u, err := url.Parse(l)
		if err != nil || u.Hostname() == "" || !isPagePath(u.Path) {
			return false
		}
		h := strings.TrimPrefix(u.Hostname(), "www.")
		if subdomains {
			return h == baseHost || strings.HasSuffix(h, "."+baseHost)
		}
		return h == baseHost
	}
	frontier := []string{base.String()}
	// Fetch budget caps politeness: the base page plus up to 40 depth-1
	// pages — enough coverage for nav-heavy sites without crawling the
	// whole domain on a synchronous request.
	fetches := 0
	const maxFetches = 40
	for depth := 0; depth < 2 && len(out) < limit && len(frontier) > 0 && fetches < maxFetches; depth++ {
		var next []string
		for _, u := range frontier {
			if len(out) >= limit || fetches >= maxFetches {
				break
			}
			fetches++
			doc, err := fetchFn(ctx, FetchRequest{URL: u, WantLinks: true, AutoRender: true})
			if err != nil {
				continue
			}
			for _, l := range doc.Links {
				l = strings.SplitN(l, "#", 2)[0]
				if seen[l] || !inScope(l) {
					continue
				}
				seen[l] = true
				out = append(out, l)
				next = append(next, l)
			}
		}
		frontier = next
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no same-host links discovered from %s", base.String())
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// assetExt matches URL paths that are resources, not pages — images, fonts,
// scripts, stylesheets, media, binaries. Documents (pdf/xml/json/rss) stay:
// maps legitimately list papers, feeds, and data files.
var assetExt = map[string]bool{
	".svg": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".ico": true, ".avif": true,
	".css": true, ".js": true, ".mjs": true, ".map": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
	".mp3": true, ".mp4": true, ".webm": true, ".mov": true, ".avi": true,
	".wasm": true, ".dmg": true, ".exe": true, ".apk": true,
	".pkg": true, ".msi": true, ".deb": true, ".rpm": true,
}

// isPagePath reports whether a URL path looks like a document rather than an
// asset — no extension, or an explicit page-ish one (.html, .md, …).
func isPagePath(p string) bool {
	for i := len(p) - 1; i >= 0 && i > len(p)-12; i-- {
		if p[i] == '.' {
			return !assetExt[p[i:]]
		}
		if p[i] == '/' {
			return true
		}
	}
	return true
}

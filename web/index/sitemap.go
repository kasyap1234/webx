package index

import (
	"compress/gzip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var sitemapClient = &http.Client{Timeout: 20 * time.Second}

// sitemapSource finds sitemap locations for a host: robots.txt Sitemap:
// lines first, then the /sitemap.xml convention. Reports whether the map
// was declared (robots) vs guessed so callers can phrase errors precisely.
// sitemapUA is a descriptive bot UA — several sites (wikipedia among them)
// 403 the default Go-http-client UA on robots.txt, which used to read as
// "no sitemap declared" when really the fetch was refused.
const sitemapUA = "webx/0.1 (+https://github.com/kasyap1234/webx)"

func sitemapURLs(ctx context.Context, base *url.URL) (found []string, declared bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.Scheme+"://"+base.Host+"/robots.txt", nil)
	if err == nil {
		req.Header.Set("User-Agent", sitemapUA)
		if robots, rerr := sitemapClient.Do(req); rerr == nil {
			defer robots.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(robots.Body, 1<<20))
			for line := range strings.SplitSeq(string(body), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(strings.ToLower(line), "sitemap:") {
					if loc := strings.TrimSpace(line[len("sitemap:"):]); loc != "" {
						found = append(found, loc)
					}
				}
			}
		}
	}
	if len(found) == 0 {
		return []string{base.Scheme + "://" + base.Host + "/sitemap.xml"}, false
	}
	return found, true
}

// locset matches both <urlset><url><loc> and <sitemapindex><sitemap><loc>.
type locset struct {
	XMLName  xml.Name
	Sitemaps []struct {
		Loc string `xml:"loc"`
	} `xml:"sitemap"`
	URLs []struct {
		Loc string `xml:"loc"`
	} `xml:"url"`
}

const maxSitemaps = 200

// fetchSitemap pulls one sitemap document, gunzipping .xml.gz responses —
// plenty of sites only ship the compressed form and a silent skip used to
// read as "site has no sitemap".
func fetchSitemap(ctx context.Context, sm string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sm, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", sitemapUA)
	resp, err := sitemapClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var r io.Reader = resp.Body
	if strings.HasSuffix(sm, ".gz") || resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close()
		r = gz
	}
	return io.ReadAll(io.LimitReader(r, 32<<20))
}

// DiscoverURLs resolves a domain's sitemaps (nested sitemap indexes
// included) into a list of page URLs, capped at limit.
func DiscoverURLs(ctx context.Context, base *url.URL, limit int) ([]string, error) {
	queue, declared := sitemapURLs(ctx, base)
	var out []string
	seen := map[string]bool{}
	var fetchErrs []string // what went wrong, for the error report
	var sawXML bool        // at least one document parsed as sitemap XML

	for len(queue) > 0 && len(out) < limit && len(seen) < maxSitemaps {
		sm := queue[0]
		queue = queue[1:]
		if seen[sm] {
			continue
		}
		seen[sm] = true

		body, err := fetchSitemap(ctx, sm)
		if err != nil {
			if len(fetchErrs) < 3 {
				fetchErrs = append(fetchErrs, fmt.Sprintf("%s: %v", sm, err))
			}
			continue
		}
		var ls locset
		if err := xml.Unmarshal(body, &ls); err != nil {
			if len(fetchErrs) < 3 {
				fetchErrs = append(fetchErrs, fmt.Sprintf("%s: not sitemap XML", sm))
			}
			continue
		}
		sawXML = true
		for _, s := range ls.Sitemaps {
			if strings.TrimSpace(s.Loc) != "" {
				queue = append(queue, s.Loc)
			}
		}
		for _, u := range ls.URLs {
			loc := strings.TrimSpace(u.Loc)
			pu, err := url.Parse(loc)
			if err != nil || pu.Host != base.Host {
				continue // same-host only
			}
			out = append(out, loc)
			if len(out) >= limit {
				break
			}
		}
	}
	if len(out) == 0 {
		// Distinguish the three ways this ends: nothing declared anywhere,
		// declared but unfetchable/unparseable, or XML that held no same-host
		// URLs — the fix differs for each.
		switch {
		case declared && len(fetchErrs) > 0:
			return nil, fmt.Errorf("%s declares a sitemap in robots.txt but it could not be used — %s", base.Host, strings.Join(fetchErrs, "; "))
		case !declared && len(fetchErrs) > 0:
			return nil, fmt.Errorf("no sitemap found for %s (robots.txt has no Sitemap: line; /sitemap.xml failed — %s)", base.Host, strings.Join(fetchErrs, "; "))
		case sawXML:
			return nil, fmt.Errorf("sitemap(s) for %s parsed but contained no same-host URLs", base.Host)
		default:
			return nil, fmt.Errorf("no sitemap found for %s (robots.txt has no Sitemap: line and /sitemap.xml is absent)", base.Host)
		}
	}
	return out, nil
}

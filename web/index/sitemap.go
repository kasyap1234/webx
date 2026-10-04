package index

import (
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
// lines first, then the /sitemap.xml convention.
func sitemapURLs(ctx context.Context, base *url.URL) []string {
	var found []string
	robots, err := sitemapClient.Get(base.Scheme + "://" + base.Host + "/robots.txt")
	if err == nil {
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
	if len(found) == 0 {
		found = []string{base.Scheme + "://" + base.Host + "/sitemap.xml"}
	}
	return found
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

// DiscoverURLs resolves a domain's sitemaps (nested sitemap indexes
// included) into a list of page URLs, capped at limit.
func DiscoverURLs(ctx context.Context, base *url.URL, limit int) ([]string, error) {
	queue := sitemapURLs(ctx, base)
	var out []string
	seen := map[string]bool{}

	for len(queue) > 0 && len(out) < limit && len(seen) < maxSitemaps {
		sm := queue[0]
		queue = queue[1:]
		if seen[sm] || strings.HasSuffix(sm, ".xml.gz") { // skip compressed for now
			continue
		}
		seen[sm] = true

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, sm, nil)
		if err != nil {
			continue
		}
		resp, err := sitemapClient.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			continue
		}
		var ls locset
		if err := xml.Unmarshal(body, &ls); err != nil {
			continue
		}
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
		return nil, fmt.Errorf("no sitemap URLs found for %s (tried robots.txt and /sitemap.xml)", base.Host)
	}
	return out, nil
}

package search

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// reddit uses search.rss — the Atom feed is the last unauthenticated search
// surface Reddit still serves (search.json 403s every client since the 2023
// API lockdown; old.reddit.com redirects to login). Opt-in via --providers
// reddit rather than a default — it's still rate-limited per-IP.
type reddit struct{}

func (r *reddit) Name() string { return "reddit" }

// redditFeed is the Atom shape search.rss returns. Entries carry the post
// title, permalink, HTML selftext, subreddit (category term) and date.
type redditFeed struct {
	Entries []struct {
		Title string `xml:"title"`
		Link  struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Content  string `xml:"content"`
		Category struct {
			Term  string `xml:"term,attr"`
			Label string `xml:"label,attr"`
		} `xml:"category"`
		Updated string `xml:"updated"`
	} `xml:"entry"`
}

func (r *reddit) Search(ctx context.Context, req Request) ([]Result, error) {
	q := req.Query
	if req.Site != "" && !hostMatches("https://www.reddit.com", req.Site) {
		return nil, nil
	}
	num := req.Num
	if num <= 0 {
		num = defaultNum
	}
	u := fmt.Sprintf("https://www.reddit.com/search.rss?q=%s&limit=%d&sort=relevance",
		url.QueryEscape(q), num)

	var resp *http.Response
	for attempt := range 3 {
		hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		// Reddit rate-limits by UA class — a descriptive bot UA fares better
		// than either a bare client or a spoofed browser string here.
		hreq.Header.Set("User-Agent", "webx/"+versionString()+" search toolkit")
		resp, err = httpClient.Do(hreq)
		if err != nil {
			return nil, fmt.Errorf("reddit: %w", err)
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 2 {
			// x-ratelimit-reset is seconds until the window opens — worth a
			// short wait rather than reporting failure on a cold window.
			wait := 2 * time.Second
			if rs := resp.Header.Get("x-ratelimit-reset"); rs != "" {
				if sec, err := strconv.Atoi(strings.Split(rs, ".")[0]); err == nil && sec > 0 {
					wait = time.Duration(sec+1) * time.Second
					if wait > 8*time.Second {
						wait = 8 * time.Second
					}
				}
			}
			resp.Body.Close()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		break
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reddit: status %d (per-IP rate limit — retry shortly or drop this provider)", resp.StatusCode)
	}
	var feed redditFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("reddit: feed parse: %w", err)
	}

	results := make([]Result, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		link := e.Link.Href
		if link == "" || !strings.Contains(link, "/comments/") {
			continue // subreddit/wiki entries aren't posts
		}
		sub := strings.TrimSpace(strings.TrimPrefix(e.Category.Label, "r/"))
		if sub == "" {
			sub = e.Category.Term
		}
		snippet := strings.Join(strings.Fields(textContent(e.Content)), " ")
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		var published time.Time
		if t, err := time.Parse(time.RFC3339, e.Updated); err == nil {
			published = t
		}
		results = append(results, Result{
			Title:     e.Title,
			URL:       link,
			Snippet:   snippet,
			Sources:   []string{r.Name()},
			Published: published,
		})
	}
	return results, nil
}

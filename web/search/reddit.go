package search

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// reddit uses search.json — unauthenticated and rate-limited, so it's opt-in
// (--providers reddit) rather than a default. Addresses the common complaint
// that SERP APIs miss forum/community discussions.
type reddit struct{}

func (r *reddit) Name() string { return "reddit" }

type redditResponse struct {
	Data struct {
		Children []struct {
			Data struct {
				Title       string  `json:"title"`
				Permalink   string  `json:"permalink"`
				Selftext    string  `json:"selftext"`
				Subreddit   string  `json:"subreddit"`
				Score       int     `json:"score"`
				NumComments int     `json:"num_comments"`
				CreatedUTC  float64 `json:"created_utc"`
			} `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

func (r *reddit) Search(ctx context.Context, req Request) ([]Result, error) {
	q := req.Query
	if req.Site != "" && !hostMatches("https://www.reddit.com", req.Site) {
		return nil, nil
	}
	u := fmt.Sprintf("https://www.reddit.com/search.json?q=%s&limit=%d&sort=relevance&type=link",
		url.QueryEscape(q), req.Num)
	var out redditResponse
	if err := getJSON(ctx, u, nil, &out); err != nil {
		return nil, fmt.Errorf("reddit: %w", err)
	}

	results := make([]Result, 0, len(out.Data.Children))
	for _, c := range out.Data.Children {
		d := c.Data
		snippet := strings.Join(strings.Fields(d.Selftext), " ")
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		if snippet == "" {
			snippet = fmt.Sprintf("r/%s · %d points · %d comments", d.Subreddit, d.Score, d.NumComments)
		}
		results = append(results, Result{
			Title:     d.Title,
			URL:       "https://www.reddit.com" + d.Permalink,
			Snippet:   snippet,
			Sources:   []string{r.Name()},
			Published: time.Unix(int64(d.CreatedUTC), 0),
		})
	}
	return results, nil
}

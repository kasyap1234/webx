package search

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// hackernews queries the free Algolia-powered HN API — excellent signal for
// dev queries, no key required.
type hackernews struct{}

func (h *hackernews) Name() string { return "hn" }

type hnResponse struct {
	Hits []struct {
		ObjectID    string `json:"objectID"`
		Title       string `json:"title"`
		URL         string `json:"url"`
		Points      int    `json:"points"`
		NumComments int    `json:"num_comments"`
		CreatedAtI  int64  `json:"created_at_i"`
	} `json:"hits"`
}

func (h *hackernews) Search(ctx context.Context, req Request) ([]Result, error) {
	u := fmt.Sprintf("https://hn.algolia.com/api/v1/search?query=%s&tags=story&hitsPerPage=%d",
		url.QueryEscape(req.Query), req.Num*2)
	var out hnResponse
	if err := getJSON(ctx, u, nil, &out); err != nil {
		return nil, fmt.Errorf("hn: %w", err)
	}

	results := make([]Result, 0, len(out.Hits))
	for _, hit := range out.Hits {
		link := hit.URL
		if link == "" { // Ask HN / job posts
			link = "https://news.ycombinator.com/item?id=" + hit.ObjectID
		}
		results = append(results, Result{
			Title:     hit.Title,
			URL:       link,
			Snippet:   fmt.Sprintf("%d points · %d comments on Hacker News", hit.Points, hit.NumComments),
			Sources:   []string{h.Name()},
			Published: time.Unix(hit.CreatedAtI, 0),
		})
	}
	return results, nil
}

package search

import (
	"context"
	"fmt"
	"net/url"
)

// brave uses the Brave Search API; enabled only when WEBX_BRAVE_API_KEY or
// BRAVE_API_KEY is set. Free tier covers ~2k queries/month.
type brave struct {
	apiKey string
}

func (b *brave) Name() string { return "brave" }

type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
			Age         string `json:"age"`
		} `json:"results"`
	} `json:"web"`
}

func (b *brave) Search(ctx context.Context, req Request) ([]Result, error) {
	if b.apiKey == "" {
		return nil, fmt.Errorf("brave: no API key (set WEBX_BRAVE_API_KEY)")
	}
	q := req.Query
	if req.Site != "" {
		q += " site:" + req.Site
	}
	u := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		url.QueryEscape(q), req.Num)
	var out braveResponse
	if err := getJSON(ctx, u, map[string]string{
		"Accept":               "application/json",
		"Accept-Encoding":      "gzip",
		"X-Subscription-Token": b.apiKey,
	}, &out); err != nil {
		return nil, fmt.Errorf("brave: %w", err)
	}

	results := make([]Result, 0, len(out.Web.Results))
	for _, r := range out.Web.Results {
		results = append(results, Result{
			Title:   textContent(r.Title),
			URL:     r.URL,
			Snippet: textContent(r.Description),
			Sources: []string{b.Name()},
		})
	}
	return results, nil
}

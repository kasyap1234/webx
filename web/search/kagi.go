package search

import (
	"context"
	"fmt"
	"net/url"
)

// kagi uses the Kagi Search API — a paid, ad-free index popular with
// researchers and the agents-* communities. Enabled only when
// WEBX_KAGI_API_TOKEN or KAGI_API_TOKEN is set (API tokens are bought
// separately from Kagi subscriptions).
type kagi struct {
	token string
}

func (k *kagi) Name() string { return "kagi" }

// https://kagi.com/api/v0/search — data[] mixes result objects (t:0) with
// related-questions (t:1); we keep the t:0 hits.
type kagiResponse struct {
	Data []struct {
		T       int    `json:"t"`
		URL     string `json:"url"`
		Title   string `json:"title"`
		Snippet string `json:"snippet"`
	} `json:"data"`
}

func (k *kagi) Search(ctx context.Context, req Request) ([]Result, error) {
	if k.token == "" {
		return nil, fmt.Errorf("kagi: no API token (set WEBX_KAGI_API_TOKEN)")
	}
	q := req.Query
	if req.Site != "" {
		q += " site:" + req.Site
	}
	u := fmt.Sprintf("https://kagi.com/api/v0/search?q=%s&limit=%d",
		url.QueryEscape(q), req.Num)
	var out kagiResponse
	if err := getJSON(ctx, u, map[string]string{
		"Authorization": "Bot " + k.token,
	}, &out); err != nil {
		return nil, fmt.Errorf("kagi: %w", err)
	}
	results := make([]Result, 0, len(out.Data))
	for _, r := range out.Data {
		if r.T != 0 || r.URL == "" {
			continue // t:0 = web result; t:1 = related questions etc.
		}
		results = append(results, Result{
			Title:   textContent(r.Title),
			URL:     r.URL,
			Snippet: textContent(r.Snippet),
			Sources: []string{k.Name()},
		})
	}
	return results, nil
}

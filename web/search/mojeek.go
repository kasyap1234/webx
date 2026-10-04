package search

import (
	"context"
	"fmt"
	"net/url"
)

// mojeek uses the Mojeek Search API — a genuinely independent index
// (not Google/Bing syndication), which matters when the majors throttle.
// Enabled only when WEBX_MOJEEK_API_KEY or MOJEEK_API_KEY is set.
type mojeek struct {
	apiKey string
}

func (m *mojeek) Name() string { return "mojeek" }

// https://api.mojeek.com/search?fmt=json&q=… — response nests results
// under response.results.standard.results.
type mojeekResponse struct {
	Response struct {
		Results struct {
			Standard struct {
				Results []struct {
					URL   string `json:"url"`
					Title string `json:"title"`
					Desc  string `json:"s"` // plain snippet; 'desc' carries html on some tiers
				} `json:"results"`
			} `json:"standard"`
		} `json:"results"`
	} `json:"response"`
}

func (m *mojeek) Search(ctx context.Context, req Request) ([]Result, error) {
	if m.apiKey == "" {
		return nil, fmt.Errorf("mojeek: no API key (set WEBX_MOJEEK_API_KEY)")
	}
	q := req.Query
	if req.Site != "" {
		q += " host:" + req.Site // mojeek's site: operator is host:
	}
	u := fmt.Sprintf("https://api.mojeek.com/search?fmt=json&api_key=%s&q=%s&t=%d",
		url.QueryEscape(m.apiKey), url.QueryEscape(q), req.Num)
	var out mojeekResponse
	if err := getJSON(ctx, u, nil, &out); err != nil {
		return nil, fmt.Errorf("mojeek: %w", err)
	}
	results := make([]Result, 0, len(out.Response.Results.Standard.Results))
	for _, r := range out.Response.Results.Standard.Results {
		results = append(results, Result{
			Title:   textContent(r.Title),
			URL:     r.URL,
			Snippet: textContent(r.Desc),
			Sources: []string{m.Name()},
		})
	}
	return results, nil
}

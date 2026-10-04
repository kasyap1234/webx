package search

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"
)

// grepapp searches inside ~1M public GitHub repositories via grep.app's free
// JSON API — code search, not page search: for identifier-shaped queries
// ("mux.HandleFunc", "cf_clearance") this is the highest-precision provider
// we have. Opt-in: Vercel's checkpoint sometimes challenges API clients.
type grepapp struct{}

func (g *grepapp) Name() string { return "grep" }

type grepResponse struct {
	Hits struct {
		Total int `json:"total"`
		Hits  []struct {
			Repo struct {
				Raw string `json:"raw"`
			} `json:"repo"`
			Path struct {
				Raw string `json:"raw"`
			} `json:"path"`
			Branch struct {
				Raw string `json:"raw"`
			} `json:"branch"`
			Content struct {
				Snippet string `json:"snippet"`
			} `json:"content"`
		} `json:"hits"`
	} `json:"hits"`
}

func (g *grepapp) Search(ctx context.Context, req Request) ([]Result, error) {
	q := req.Query
	u := "https://grep.app/api/search?q=" + url.QueryEscape(q)
	var out grepResponse
	if err := getJSON(ctx, u, nil, &out); err != nil {
		return nil, fmt.Errorf("grep: %w (grep.app may be checkpoint-blocking)", err)
	}

	limit := req.Num
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	results := make([]Result, 0, limit)
	for _, h := range out.Hits.Hits {
		if len(results) >= limit {
			break
		}
		repo := h.Repo.Raw
		path := h.Path.Raw
		branch := h.Branch.Raw
		if branch == "" {
			branch = "HEAD"
		}
		// snippet is HTML with <mark> highlights — strip tags, keep text.
		snippet := strings.TrimSpace(textContent(h.Content.Snippet))
		snippet = html.UnescapeString(snippet)
		results = append(results, Result{
			Title:   fmt.Sprintf("%s — %s", repo, path),
			URL:     fmt.Sprintf("https://github.com/%s/blob/%s/%s", repo, branch, path),
			Snippet: snippet,
			Sources: []string{g.Name()},
		})
	}
	return results, nil
}

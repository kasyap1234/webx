package search

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// github searches repositories via the GitHub Search API — unauthenticated
// is ~10 req/min; set GITHUB_TOKEN or WEBX_GITHUB_TOKEN for more. Opt-in.
type github struct{ token string }

func (g *github) Name() string { return "gh" }

type ghResponse struct {
	Items []struct {
		FullName    string `json:"full_name"`
		HTMLURL     string `json:"html_url"`
		Description string `json:"description"`
		Stars       int    `json:"stargazers_count"`
		Language    string `json:"language"`
		UpdatedAt   string `json:"updated_at"`
	} `json:"items"`
}

func githubToken() string {
	if t := os.Getenv("WEBX_GITHUB_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GITHUB_TOKEN")
}

func (g *github) Search(ctx context.Context, req Request) ([]Result, error) {
	u := fmt.Sprintf("https://api.github.com/search/repositories?q=%s&per_page=%d&sort=updated",
		url.QueryEscape(req.Query), req.Num)
	headers := map[string]string{"Accept": "application/vnd.github+json"}
	if g.token != "" {
		headers["Authorization"] = "Bearer " + g.token
	}
	var out ghResponse
	if err := getJSON(ctx, u, headers, &out); err != nil {
		return nil, fmt.Errorf("gh: %w", err)
	}

	results := make([]Result, 0, len(out.Items))
	for _, it := range out.Items {
		snippet := strings.Join(strings.Fields(it.Description), " ")
		if snippet == "" {
			snippet = it.FullName
		}
		snippet += fmt.Sprintf(" · ★%d", it.Stars)
		if it.Language != "" {
			snippet += " · " + it.Language
		}
		r := Result{
			Title:   it.FullName,
			URL:     it.HTMLURL,
			Snippet: snippet,
			Sources: []string{g.Name()},
		}
		if t, err := time.Parse(time.RFC3339, it.UpdatedAt); err == nil {
			r.Published = t
		}
		results = append(results, r)
	}
	return results, nil
}

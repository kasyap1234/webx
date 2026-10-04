package search

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// wikipedia uses the MediaWiki search API — free, no key, good coverage for
// concepts, protocols, and tools.
type wikipedia struct{}

func (w *wikipedia) Name() string { return "wiki" }

type wikiResponse struct {
	Query struct {
		Search []struct {
			Title     string `json:"title"`
			Snippet   string `json:"snippet"`
			PageID    int64  `json:"pageid"`
			Timestamp string `json:"timestamp"`
		} `json:"search"`
	} `json:"query"`
}

func (w *wikipedia) Search(ctx context.Context, req Request) ([]Result, error) {
	lang := req.Lang
	if lang == "" {
		lang = "en"
	}
	u := fmt.Sprintf("https://%s.wikipedia.org/w/api.php?action=query&list=search&srsearch=%s&srlimit=%d&format=json&utf8=1",
		url.QueryEscape(lang), url.QueryEscape(req.Query), req.Num)
	var out wikiResponse
	if err := getJSON(ctx, u, nil, &out); err != nil {
		return nil, fmt.Errorf("wiki: %w", err)
	}

	results := make([]Result, 0, len(out.Query.Search))
	for _, it := range out.Query.Search {
		r := Result{
			Title:   it.Title,
			URL:     "https://" + lang + ".wikipedia.org/wiki/" + strings.ReplaceAll(it.Title, " ", "_"),
			Snippet: textContent(it.Snippet),
			Sources: []string{w.Name()},
		}
		if t, err := time.Parse(time.RFC3339, it.Timestamp); err == nil {
			r.Published = t
		}
		results = append(results, r)
	}
	return results, nil
}

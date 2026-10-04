package search

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"time"
)

// stackoverflow queries the free Stack Exchange excerpts API — ~300 req/day
// per IP, no key. Best for error-message-style queries.
type stackoverflow struct{}

func (s *stackoverflow) Name() string { return "so" }

type seResponse struct {
	Items []struct {
		Title        string `json:"title"`
		Excerpt      string `json:"excerpt"`
		ItemType     string `json:"item_type"`
		QuestionID   int    `json:"question_id"`
		AnswerID     int    `json:"answer_id"`
		CreationDate int64  `json:"creation_date"`
		Score        int    `json:"score"`
		IsAnswered   bool   `json:"is_answered"`
	} `json:"items"`
}

func (s *stackoverflow) Search(ctx context.Context, req Request) ([]Result, error) {
	q := req.Query
	if req.Site != "" && !hostMatches("https://stackoverflow.com", req.Site) {
		return nil, nil // SO can only return stackoverflow.com results
	}
	u := fmt.Sprintf("https://api.stackexchange.com/2.3/search/excerpts?order=desc&sort=relevance&q=%s&site=stackoverflow&pagesize=%d",
		url.QueryEscape(q), req.Num)
	var out seResponse
	if err := getJSON(ctx, u, nil, &out); err != nil {
		return nil, fmt.Errorf("so: %w", err)
	}

	results := make([]Result, 0, len(out.Items))
	for _, it := range out.Items {
		link := fmt.Sprintf("https://stackoverflow.com/questions/%d", it.QuestionID)
		if it.ItemType == "answer" && it.AnswerID != 0 {
			link = fmt.Sprintf("https://stackoverflow.com/a/%d", it.AnswerID)
		}
		snippet := textContent(it.Excerpt)
		if it.IsAnswered {
			snippet = "✓ answered · " + snippet
		}
		results = append(results, Result{
			Title:     html.UnescapeString(it.Title),
			URL:       link,
			Snippet:   snippet,
			Sources:   []string{s.Name()},
			Published: time.Unix(it.CreationDate, 0),
		})
	}
	return results, nil
}

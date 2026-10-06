package search

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// stackoverflow queries the free Stack Exchange excerpts API — ~300 req/day
// per IP, no key. Best for error-message-style queries.
//
// Two SE quirks handled here: the API reports throttling as HTTP 400 with a
// JSON envelope ({error_id, error_name, error_message}) — a bare "status 400"
// hides that — and responses carry a `backoff` field asking clients to pause.
// We surface both and honor backoff process-wide so bursts (eval, doctor,
// watch) don't escalate a polite delay into a Cloudflare block.
type stackoverflow struct{}

func (s *stackoverflow) Name() string { return "so" }

// soBackoffUntil is the process-wide "don't call again before" stamp the SE
// API requests via its backoff field. Honored before every request — it's the
// difference between 300 req/day and a multi-hour Cloudflare 1015.
var soBackoffUntil struct {
	sync.Mutex
	t time.Time
}

func (s *stackoverflow) backoffWait() error {
	soBackoffUntil.Lock()
	defer soBackoffUntil.Unlock()
	if d := time.Until(soBackoffUntil.t); d > 0 {
		return fmt.Errorf("throttled — stack exchange asked for backoff until %s", soBackoffUntil.t.Format("15:04:05"))
	}
	return nil
}

func (s *stackoverflow) noteBackoff(seconds int) {
	soBackoffUntil.Lock()
	soBackoffUntil.t = time.Now().Add(time.Duration(seconds) * time.Second)
	soBackoffUntil.Unlock()
}

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
	Backoff int `json:"backoff"`
}

// seError is the Stack Exchange fault envelope — returned on non-200 with a
// real reason (throttle_violation, bad_parameter, quota exceeded).
type seError struct {
	ErrorID      int    `json:"error_id"`
	ErrorName    string `json:"error_name"`
	ErrorMessage string `json:"error_message"`
}

func (s *stackoverflow) Search(ctx context.Context, req Request) ([]Result, error) {
	q := req.Query
	if req.Site != "" && !hostMatches("https://stackoverflow.com", req.Site) {
		return nil, nil // SO can only return stackoverflow.com results
	}
	if err := s.backoffWait(); err != nil {
		return nil, err
	}
	num := req.Num
	if num <= 0 {
		num = defaultNum
	}
	if num > 100 {
		num = 100 // SE hard cap — over it the API answers 400
	}
	u := fmt.Sprintf("https://api.stackexchange.com/2.3/search/excerpts?order=desc&sort=relevance&q=%s&site=stackoverflow&pagesize=%d",
		url.QueryEscape(q), num)

	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("User-Agent", searchUserAgent)
	resp, err := httpClient.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("so: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("so: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var e seError
		if json.Unmarshal(body, &e) == nil && e.ErrorMessage != "" {
			return nil, fmt.Errorf("so: %s (%s)", e.ErrorMessage, e.ErrorName)
		}
		return nil, fmt.Errorf("so: status %d", resp.StatusCode)
	}
	var out seResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("so: %w", err)
	}
	if out.Backoff > 0 {
		s.noteBackoff(out.Backoff)
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

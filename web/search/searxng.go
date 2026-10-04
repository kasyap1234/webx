package search

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// searxng queries a self-hosted or public SearXNG instance — one provider
// that fans out to 70+ engines server-side. Enabled when WEBX_SEARXNG_URL
// is set; JSON output must be enabled on the instance (format=json).
// category selects the SearXNG vertical: "" / "general" for web, "news"
// and "images" for the typed sources.
type searxng struct {
	bases    []string
	category string // "" | "news" | "images" ("general" treated as "")
}

func (s *searxng) Name() string {
	if s.category != "" {
		return "searxng_" + s.category
	}
	return "searxng"
}

type searxngResponse struct {
	Results []struct {
		URL           string   `json:"url"`
		Title         string   `json:"title"`
		Content       string   `json:"content"`
		Engine        string   `json:"engine"`
		Engines       []string `json:"engines"`
		PublishedDate string   `json:"publishedDate"`
		ImgSrc        string   `json:"img_src"`       // images category: full image
		ThumbnailSrc  string   `json:"thumbnail_src"` // images category: thumbnail
	} `json:"results"`
}

// searxngBases parses WEBX_SEARXNG_URL as a comma-separated instance list —
// public instances rate-limit unpredictably, so we rotate on failure.
func searxngBases() []string {
	raw := os.Getenv("WEBX_SEARXNG_URL")
	var out []string
	for _, b := range strings.Split(raw, ",") {
		if b = strings.TrimRight(strings.TrimSpace(b), "/"); b != "" {
			out = append(out, b)
		}
	}
	return out
}

func (s *searxng) Search(ctx context.Context, req Request) ([]Result, error) {
	q := req.Query
	if req.Site != "" {
		q += " site:" + req.Site
	}
	var out searxngResponse
	var lastErr error
	bases := s.bases
	if len(bases) == 0 {
		bases = searxngBases()
	}
	if len(bases) == 0 {
		return nil, fmt.Errorf("searxng: WEBX_SEARXNG_URL not set")
	}
	cat := "general"
	if req.Topic == "news" {
		cat = "news"
	}
	if s.category != "" {
		cat = s.category // explicit vertical wins over the topic hint
	}
	for _, base := range bases {
		u := fmt.Sprintf("%s/search?q=%s&format=json&categories=%s",
			base, url.QueryEscape(q), cat)
		if req.Lang != "" {
			u += "&language=" + url.QueryEscape(req.Lang)
		}
		if tr := timeRange(req.After); tr != "" {
			u += "&time_range=" + tr
		}
		if err := getJSON(ctx, u, nil, &out); err != nil {
			lastErr = err
			continue
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		return nil, fmt.Errorf("searxng: %w (is format=json enabled on the instance?)", lastErr)
	}

	results := make([]Result, 0, len(out.Results))
	for _, r := range out.Results {
		res := Result{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content,
			Sources: []string{s.Name()},
		}
		// Typed verticals tag their results — images carry the full asset
		// URL (page URL stays in URL so dedup still works on the host).
		switch s.category {
		case "news":
			res.Type = "news"
		case "images":
			res.Type = "images"
			res.ImageURL = r.ImgSrc
			res.Thumbnail = r.ThumbnailSrc
		}
		if r.PublishedDate != "" {
			if t, err := time.Parse("2006-01-02T15:04:05", r.PublishedDate); err == nil {
				res.Published = t
			} else if t, err := time.Parse("2006-01-02", r.PublishedDate); err == nil {
				res.Published = t
			}
		}
		results = append(results, res)
	}
	return results, nil
}

// timeRange maps an After cutoff onto SearXNG's coarse buckets — the
// post-fusion date filter still does exact trimming, this narrows upstream.
func timeRange(after time.Time) string {
	if after.IsZero() {
		return ""
	}
	d := time.Since(after)
	switch {
	case d <= 24*time.Hour:
		return "day"
	case d <= 30*24*time.Hour:
		return "month"
	case d <= 365*24*time.Hour:
		return "year"
	}
	return ""
}

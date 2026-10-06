package search

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/kasyap1234/webx/web/fetch"
	"github.com/kasyap1234/webx/web/render"
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
			Repo    grepField `json:"repo"`
			Path    grepField `json:"path"`
			Branch  grepField `json:"branch"`
			Content struct {
				Snippet string `json:"snippet"`
			} `json:"content"`
		} `json:"hits"`
	} `json:"hits"`
}

// grepField is grep.app's polymorphic string-or-object field — the API has
// shipped both "owner/repo" and {"raw":"owner/repo"} depending on version.
type grepField struct{ raw string }

func (f *grepField) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		f.raw = s
		return nil
	}
	var obj struct {
		Raw string `json:"raw"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	f.raw = obj.Raw
	return nil
}

func (g *grepapp) Search(ctx context.Context, req Request) ([]Result, error) {
	q := req.Query
	u := "https://grep.app/api/search?q=" + url.QueryEscape(q)
	var out grepResponse
	err := getJSON(ctx, u, nil, &out)
	if err != nil {
		// Vercel's checkpoint keys on TLS fingerprint — the plain Go client
		// trips it on sight. Retry through the Chrome-fingerprint transport;
		// a solved challenge also drops cookies into the session jar.
		bt := fetch.Client(true, req.Session, 15*time.Second, "")
		err2 := getJSONWith(ctx, bt, u, map[string]string{
			"Accept":  "application/json",
			"Referer": "https://grep.app/",
		}, &out)
		if err2 != nil {
			// Still blocked → it's the JS challenge tier, which only a real
			// browser passes. Render the API URL when an engine is around;
			// the JSON lands in the page's <pre>.
			if rerr := g.viaRender(ctx, u, &out); rerr != nil {
				return nil, fmt.Errorf("grep: %w (browser retry: %v; render: %v)", err2, err, rerr)
			}
		}
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
		repo := h.Repo.raw
		path := h.Path.raw
		branch := h.Branch.raw
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

// viaRender fetches the API URL through the real browser — the only thing
// that passes Vercel's JS-challenge tier. The API's JSON response renders
// as a <pre> in the DOM; strip tags and unmarshal it directly.
func (g *grepapp) viaRender(ctx context.Context, u string, out *grepResponse) error {
	if !render.Available() {
		return fmt.Errorf("no render engine (install Chrome or set WEBX_RENDER_URL) — try the sg provider for code search")
	}
	res, err := render.Render(ctx, render.Request{URL: u, BlockMedia: true})
	if err != nil {
		return err
	}
	body := textContent(string(res.HTML))
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), out); err != nil {
		return fmt.Errorf("rendered response not JSON: %w", err)
	}
	return nil
}

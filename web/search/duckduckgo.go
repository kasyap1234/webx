package search

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kasyap1234/webx/web/fetch"
	"golang.org/x/net/html"
)

// duckduckgo scrapes the free html.duckduckgo.com endpoint — no key, but
// rate-limited, so failures are tolerated by the merger.
type duckduckgo struct{}

func (d *duckduckgo) Name() string { return "ddg" }

func (d *duckduckgo) Search(ctx context.Context, req Request) ([]Result, error) {
	q := req.Query
	if req.Site != "" {
		q += " site:" + req.Site
	}
	form := url.Values{"q": {q}, "kl": {"us-en"}}

	body, err := postForm(ctx, "https://html.duckduckgo.com/html/", form, nil)
	if err != nil {
		// DDG's html endpoint 202-challenges datacenter clients under load —
		// back off once, then try the lite endpoint which is less guarded.
		time.Sleep(1500 * time.Millisecond)
		body, err = postForm(ctx, "https://html.duckduckgo.com/html/", form, nil)
	}
	if err != nil {
		var lerr error
		if body, lerr = postForm(ctx, "https://lite.duckduckgo.com/lite/", form, nil); lerr != nil {
			// Last resort: real Chrome fingerprint + persistent session —
			// the 202 is a JA3-challenge, and this is what actually answers it.
			browser := fetch.Client(true, "ddg", 20*time.Second, "")
			if body, err = postFormWith(ctx, browser, "https://html.duckduckgo.com/html/", form, nil); err != nil {
				return nil, fmt.Errorf("ddg: %w (lite: %v)", err, lerr)
			}
		}
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("ddg: parse: %w", err)
	}

	var links, snippets []*html.Node
	walk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		// html endpoint uses result__a/result__snippet; lite uses
		// result-link/result-snippet — accept both class sets.
		if n.Data == "a" && (hasClass(n, "result__a") || hasClass(n, "result-link")) {
			links = append(links, n)
		} else if hasClass(n, "result__snippet") || hasClass(n, "result-snippet") {
			snippets = append(snippets, n)
		}
	})

	results := make([]Result, 0, len(links))
	for i, a := range links {
		rawURL := decodeDDGLink(attr(a, "href"))
		if rawURL == "" {
			continue
		}
		r := Result{
			Title:   strings.Join(strings.Fields(textOf(a)), " "),
			URL:     rawURL,
			Sources: []string{d.Name()},
		}
		if i < len(snippets) {
			r.Snippet = strings.Join(strings.Fields(textOf(snippets[i])), " ")
		}
		results = append(results, r)
	}
	return results, nil
}

func textOf(n *html.Node) string {
	var b strings.Builder
	walk(n, func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	})
	return b.String()
}

// decodeDDGLink unwraps duckduckgo.com/l/?uddg=<target> redirect links.
func decodeDDGLink(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if uddg := u.Query().Get("uddg"); uddg != "" {
		return uddg
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return href
	}
	return ""
}

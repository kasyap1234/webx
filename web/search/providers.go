package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const searchUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 webx/0.1"

// versionString reports the running module version for UAs and logs —
// stamped builds get their ldflags version via the caller; source builds
// fall back to runtime build info so "go install" binaries still identify
// honestly instead of "dev".
func versionString() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "0.1.0"
}

func getJSON(ctx context.Context, u string, headers map[string]string, v any) error {
	return getJSONWith(ctx, httpClient, u, headers, v)
}

// getJSONWith is getJSON over a specific client — providers retrying through
// the browser transport (uTLS Chrome fingerprint) use it when the plain
// client trips a checkpoint.
func getJSONWith(ctx context.Context, client *http.Client, u string, headers map[string]string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", searchUserAgent)
	for k, h := range headers {
		req.Header.Set(k, h)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

func postForm(ctx context.Context, u string, form url.Values, headers map[string]string) ([]byte, error) {
	return postFormWith(ctx, httpClient, u, form, headers)
}

// postFormWith is postForm over a specific client — providers retrying
// through the browser transport (uTLS + cookie jar) use this.
func postFormWith(ctx context.Context, client *http.Client, u string, form url.Values, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", searchUserAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, h := range headers {
		req.Header.Set(k, h)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// textContent collects the text of an HTML fragment (e.g. SO excerpts).
func textContent(fragment string) string {
	ctx := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), ctx)
	if err != nil {
		return fragment
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func hasClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, c := range strings.Fields(a.Val) {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func walk(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

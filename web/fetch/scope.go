package fetch

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// applySelectors scopes an HTML document before extraction:
//   - include: keep only elements matching any include selector (Jina's
//     X-Target-Selector / Firecrawl's includeTags) — "just the API table".
//   - exclude: delete elements matching any exclude selector (X-Remove-Selector
//     / excludeTags) — strip nav, cookie bars, footers readability kept.
func applySelectors(body []byte, include, exclude []string) ([]byte, error) {
	if len(include) == 0 && len(exclude) == 0 {
		return body, nil
	}
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("scope html: %w", err)
	}

	if len(include) > 0 {
		var kept []string
		for _, sel := range include {
			matcher, err := cascadia.Compile(sel)
			if err != nil {
				return nil, fmt.Errorf("include selector %q: %w", sel, err)
			}
			for _, n := range matcher.MatchAll(root) {
				var b strings.Builder
				if html.Render(&b, n) == nil {
					kept = append(kept, b.String())
				}
			}
		}
		if len(kept) == 0 {
			return nil, fmt.Errorf("include selectors matched nothing — page structure may differ (selectors: %s)",
				strings.Join(include, ", "))
		}
		return []byte("<html><body>" + strings.Join(kept, "\n") + "</body></html>"), nil
	}

	for _, sel := range exclude {
		matcher, err := cascadia.Compile(sel)
		if err != nil {
			return nil, fmt.Errorf("exclude selector %q: %w", sel, err)
		}
		for _, n := range matcher.MatchAll(root) {
			if n.Parent != nil {
				n.Parent.RemoveChild(n)
			}
		}
	}
	var out strings.Builder
	if err := html.Render(&out, root); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

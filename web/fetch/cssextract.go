package fetch

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// ExtractCSS runs a CSS-selector schema against raw HTML — Crawl4AI's
// JsonCssExtractionStrategy: structured JSON, zero LLM cost, deterministic.
//
// Schema shapes:
//
//	{"title": "h1"}                              field  → text of first match
//	{"url":   "a.main@href"}                     @attr  → attribute value
//	{"tags":  "a.tag[]"}                         []     → array of text
//	{"hrefs": "a.nav[]@href"}                    []@attr→ array of attributes
//	{"meta":  {"selector": ".card",              object → fields applied to
//	           "fields": {"a":"h2"}}}                    the first match's subtree
//	{"rows":  {"selector": "tr.item[]",          object[]→ fields per match
//	           "fields": {"n":".name","u":"a@href"}}}
//
// A "fields" value may itself be a nested object schema for deeper shapes.
func ExtractCSS(htmlBody []byte, schema map[string]any) (map[string]any, error) {
	root, err := html.Parse(bytes.NewReader(htmlBody))
	if err != nil {
		return nil, fmt.Errorf("css extract: parse: %w", err)
	}
	out, err := evalSchema(root, schema)
	if err != nil {
		return nil, err
	}
	m, _ := out.(map[string]any)
	return m, nil
}

func evalSchema(node *html.Node, schema map[string]any) (any, error) {
	out := map[string]any{}
	for field, spec := range schema {
		v, err := evalField(node, spec)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", field, err)
		}
		out[field] = v
	}
	return out, nil
}

func evalField(node *html.Node, spec any) (any, error) {
	switch s := spec.(type) {
	case string:
		sel, attr, multi := parseSelector(s)
		return evalSelector(node, sel, attr, multi)
	case map[string]any:
		selStr, _ := s["selector"].(string)
		fields, _ := s["fields"].(map[string]any)
		if selStr == "" || len(fields) == 0 {
			return nil, fmt.Errorf("object spec needs selector + fields")
		}
		sel, _, multi := parseSelector(selStr)
		matcher, err := cascadia.Compile(sel)
		if err != nil {
			return nil, err
		}
		matches := matcher.MatchAll(node)
		if multi {
			arr := make([]any, 0, len(matches))
			for _, m := range matches {
				o, err := evalSchema(m, fields)
				if err != nil {
					return nil, err
				}
				arr = append(arr, o)
			}
			return arr, nil
		}
		if len(matches) == 0 {
			return nil, nil
		}
		return evalSchema(matches[0], fields)
	default:
		return nil, fmt.Errorf("unsupported spec type %T", spec)
	}
}

// parseSelector splits "sel[]@attr" into selector, attribute, multiplicity.
func parseSelector(s string) (sel, attr string, multi bool) {
	if i := strings.LastIndex(s, "@"); i >= 0 {
		attr = s[i+1:]
		s = s[:i]
	}
	if strings.HasSuffix(s, "[]") {
		multi = true
		s = s[:len(s)-2]
	}
	return strings.TrimSpace(s), attr, multi
}

func evalSelector(node *html.Node, sel, attr string, multi bool) (any, error) {
	matcher, err := cascadia.Compile(sel)
	if err != nil {
		return nil, err
	}
	matches := matcher.MatchAll(node)
	val := func(n *html.Node) string {
		if attr == "html" {
			var b strings.Builder
			_ = html.Render(&b, n)
			return b.String()
		}
		if attr != "" {
			for _, a := range n.Attr {
				if a.Key == attr {
					return a.Val
				}
			}
			return ""
		}
		return strings.TrimSpace(nodeText(n))
	}
	if multi {
		arr := make([]string, 0, len(matches))
		for _, m := range matches {
			arr = append(arr, val(m))
		}
		return arr, nil
	}
	if len(matches) == 0 {
		return nil, nil
	}
	return val(matches[0]), nil
}

// nodeText concatenates descendant text — trim-heavy but simple.
func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
			b.WriteByte(' ')
			return
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

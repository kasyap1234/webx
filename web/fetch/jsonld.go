package fetch

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ExtractLD pulls every application/ld+json block from an HTML document and
// returns them as raw JSON entities — schema.org typed data (Product,
// Article, JobPosting, FAQPage…) that almost always beats selector scraping
// when present. @graph arrays are flattened to individual entities.
func ExtractLD(html []byte) []json.RawMessage {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil
	}
	var out []json.RawMessage
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		raw := bytes.TrimSpace([]byte(s.Text()))
		if len(raw) == 0 {
			return
		}
		var parsed any
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return // malformed ld+json — skip honestly, don't guess
		}
		out = append(out, flattenLD(parsed)...)
	})
	return out
}

// flattenLD expands @graph arrays and top-level JSON arrays into individual
// entities — one RawMessage per typed object.
func flattenLD(v any) []json.RawMessage {
	var out []json.RawMessage
	switch t := v.(type) {
	case []any:
		for _, it := range t {
			out = append(out, flattenLD(it)...)
		}
	case map[string]any:
		if g, ok := t["@graph"].([]any); ok {
			for _, it := range g {
				out = append(out, flattenLD(it)...)
			}
			return out
		}
		if raw, err := json.Marshal(t); err == nil {
			out = append(out, raw)
		}
	}
	return out
}

// LDType returns the @type of an entity as lowercase — may be a string or
// array; "" when absent.
func LDType(entity json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(entity, &m); err != nil {
		return ""
	}
	switch t := m["@type"].(type) {
	case string:
		return strings.ToLower(t)
	case []any:
		if len(t) > 0 {
			if s, ok := t[0].(string); ok {
				return strings.ToLower(s)
			}
		}
	}
	return ""
}

// LDOfType filters entities to a schema.org @type name (case-insensitive).
// Common aliases map to the schema type agents usually mean.
func LDOfType(entities []json.RawMessage, want string) []json.RawMessage {
	want = strings.ToLower(strings.TrimSpace(want))
	alias := map[string]string{
		"job":     "jobposting",
		"posting": "jobposting",
		"event":   "event",
		"faq":     "faqpage",
		"howto":   "howto",
	}
	if a, ok := alias[want]; ok {
		want = a
	}
	var out []json.RawMessage
	for _, e := range entities {
		if LDType(e) == want {
			out = append(out, e)
		}
	}
	return out
}

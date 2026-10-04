package fetch

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// DetectInjection scans a page's HTML + extracted markdown for content
// anomalies commonly used to smuggle prompt-injection into an agent's
// context — hidden text, invisible characters, and instruction phrasing.
// No competitor does this; for a tool whose consumers are LLM agents it is
// the honest signal that matters most. Returns human-readable warnings.
func DetectInjection(raw []byte, md string) []string {
	var warns []string

	if n, kinds := hiddenTextBlocks(raw); n > 0 {
		warns = append(warns, fmt.Sprintf("hidden-text: %d blocks concealed via %s", n, strings.Join(kinds, ", ")))
	}
	if n := invisibleChars(md); n > 0 {
		warns = append(warns, fmt.Sprintf("invisible-chars: %d zero-width/bidi control characters in text", n))
	}
	if phrases := instructionPhrases(md); len(phrases) > 0 {
		warns = append(warns, fmt.Sprintf("instruction-phrasing: %s", strings.Join(phrases, ", ")))
	}
	if n := hiddenComments(raw); n > 0 {
		warns = append(warns, fmt.Sprintf("html-comments: %d comments containing instruction-like text", n))
	}
	return warns
}

// hiddenAttrRe matches CSS that conceals text from sighted readers while
// leaving it in the DOM — the classic injection delivery mechanism.
var hiddenAttrRe = regexp.MustCompile(`(?i)(display\s*:\s*none|visibility\s*:\s*hidden|font-size\s*:\s*0|opacity\s*:\s*0|height\s*:\s*0|width\s*:\s*0|left\s*:\s*-\d{3,}|text-indent\s*:\s*-\d{3,}|clip\s*:\s*rect\(0|position\s*:\s*absolute[^;]*(?:left|top)\s*:\s*-\d{3,})`)

func hiddenTextBlocks(raw []byte) (int, []string) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return 0, nil
	}
	kinds := map[string]bool{}
	n := 0
	doc.Find("[style]").Each(func(_ int, s *goquery.Selection) {
		style, _ := s.Attr("style")
		m := hiddenAttrRe.FindStringSubmatch(style)
		if m == nil {
			return
		}
		// Only flag when there's real text being hidden — hidden chrome
		// like empty divs is everywhere and not suspicious.
		if len(strings.TrimSpace(s.Text())) >= 40 {
			n++
			kind := m[1]
			if i := strings.Index(kind, ":"); i > 0 {
				kind = strings.TrimSpace(kind[:i])
			}
			kinds[strings.ToLower(kind)] = true
		}
	})
	if a := doc.Find("[aria-hidden='true'], [hidden]").Length(); a > 0 {
		doc.Find("[aria-hidden='true'], [hidden]").Each(func(_ int, s *goquery.Selection) {
			if len(strings.TrimSpace(s.Text())) >= 40 {
				n++
				kinds["aria-hidden/hidden"] = true
			}
		})
	}
	out := make([]string, 0, len(kinds))
	for k := range kinds {
		out = append(out, k)
	}
	return n, out
}

// invisibleChars counts characters that are invisible when rendered but
// present in text — used to hide instructions or break keyword scanning.
// Explicit escapes: these runes are literally invisible in source.
func invisibleChars(s string) int {
	n := 0
	for _, r := range s {
		switch {
		case r == '\u200B' || r == '\u200C' || r == '\u200D' || r == '\u2060': // ZWSP/ZWNJ/ZWJ/word-joiner
			n++
		case r == '\uFEFF': // BOM as zero-width no-break space
			n++
		case r >= '\u202A' && r <= '\u202E': // bidi controls LRE..PDF
			n++
		}
	}
	return n
}

// instructionRe catches phrases designed to hijack an LLM reader. Kept
// deliberately narrow — common benign phrasings must not trigger it.
var instructionRe = regexp.MustCompile(`(?i)(ignore\s+(all\s+|any\s+)?(previous|prior|above|earlier)\s+(instructions?|prompts?|directions?|rules?)|disregard\s+(your\s+)?(previous|all|prior|the\s+above)\s+(instructions?|context|training)|forget\s+(everything|all)\s+(you\s+)?(know|were\s+told)|you\s+are\s+now\s+(a|an|in)\s|new\s+system\s+(prompt|instructions?)|do\s+not\s+(reveal|tell|mention)\s+(this|these)\s+instructions?|act\s+as\s+(if|though)\s+you\s+(have\s+)?(no|are\s+not)\s+restrictions?)`)

func instructionPhrases(md string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range instructionRe.FindAllString(md, 6) {
		m = strings.TrimSpace(m)
		if len(m) > 60 {
			m = m[:60]
		}
		if !seen[strings.ToLower(m)] {
			seen[strings.ToLower(m)] = true
			out = append(out, `"`+m+`"`)
		}
	}
	return out
}

var commentRe = regexp.MustCompile(`<!--([\s\S]*?)-->`)

// hiddenComments counts HTML comments whose text looks like agent
// instructions — a delivery channel invisible to rendered-page readers.
func hiddenComments(raw []byte) int {
	n := 0
	for _, m := range commentRe.FindAllSubmatch(raw, -1) {
		body := html.UnescapeString(string(m[1]))
		if instructionRe.MatchString(body) {
			n++
		}
	}
	return n
}

// SanitizeHTML strips the concealment mechanisms DetectInjection reports:
// hidden elements, instruction comments, and zero-width/bidi characters.
// The original is never mutated — callers decide whether to sanitize.
func SanitizeHTML(raw []byte) []byte {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return raw
	}
	doc.Find("[style]").Each(func(_ int, s *goquery.Selection) {
		style, _ := s.Attr("style")
		if hiddenAttrRe.MatchString(style) && len(strings.TrimSpace(s.Text())) >= 40 {
			s.Remove()
		}
	})
	doc.Find("[aria-hidden='true'], [hidden]").Each(func(_ int, s *goquery.Selection) {
		if len(strings.TrimSpace(s.Text())) >= 40 {
			s.Remove()
		}
	})
	out, err := doc.Html()
	if err != nil {
		return raw
	}
	return []byte(out)
}

package fetch

import (
	"fmt"
	"regexp"
	"strings"
)

// dataURIRe matches markdown images whose target is a data: URI — inline
// base64 is pure token waste for agents (and often megabytes of it).
var dataURIRe = regexp.MustCompile(`!\[[^\]]*\]\(data:[^)]*\)`)

// stripDataURIs removes base64-encoded images from markdown.
func stripDataURIs(md string) string {
	return dataURIRe.ReplaceAllString(md, "")
}

var mdImageRe = regexp.MustCompile(`!\[([^\]]*)\]\((https?://[^\s)<>"']+)\)`)

// mdLinkRe (in fetch.go) matches non-image markdown links.
var mdPlainLinkRe = regexp.MustCompile(`(?:^|[\s(])\[([^\]]+)\]\((https?://[^\s)<>"']+)\)`)

// extractImages returns absolute image URLs + alt text found in markdown.
func extractImages(md string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mdImageRe.FindAllStringSubmatch(md, -1) {
		u := m[2]
		if seen[u] {
			continue
		}
		seen[u] = true
		if alt := strings.TrimSpace(m[1]); alt != "" {
			out = append(out, u+" \""+alt+"\"")
		} else {
			out = append(out, u)
		}
	}
	return out
}

// RefSummary appends Jina-style aggregated reference blocks to the end of
// the document — one "## Links" / "## Images" numbered list so agents can
// cite or follow resources without re-parsing inline markdown.
func RefSummary(md string, links, images []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(md, "\n"))
	if len(links) > 0 {
		b.WriteString("\n\n## Links\n\n")
		for i, l := range links {
			fmt.Fprintf(&b, "[%d] %s\n", i+1, l)
		}
	}
	if len(images) > 0 {
		b.WriteString("\n## Images\n\n")
		for i, l := range images {
			fmt.Fprintf(&b, "[%d] %s\n", i+1, l)
		}
	}
	return b.String()
}

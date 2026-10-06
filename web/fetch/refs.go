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

// stripHTMLComments removes <!-- --> comments outside fenced code blocks.
// Extractors leak template markers (<!--THE END-->, CMS guards) into
// markdown where they poison snippets, diffs and fit output; inside fences
// they may be genuine code samples and stay.
func stripHTMLComments(md string) string {
	var b strings.Builder
	b.Grow(len(md))
	inFence, inComment := false, false
	for line := range strings.Lines(md) {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			b.WriteString(line + "\n")
			continue
		}
		if inFence {
			b.WriteString(line + "\n")
			continue
		}
		for {
			if inComment {
				i := strings.Index(line, "-->")
				if i < 0 {
					line = ""
					break
				}
				line, inComment = line[i+3:], false
				continue
			}
			i := strings.Index(line, "<!--")
			if i < 0 {
				break
			}
			line, inComment = line[:i]+line[i+4:], true
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// footnoteRe matches [N]-style citation markers — [\[15\]](…), [\[edit\]](…),
// [\[citation needed\]](…). Converters escape the inner brackets as \[ \],
// so backslashes are optional in the pattern. Anchors wrapped in literal
// brackets are wiki/CMS footnote chrome, never prose.
var footnoteRe = regexp.MustCompile(`\[\\?\[[^\]]{1,20}\\?\]\]\([^)]*\)`)

// mdInlineLinkRe matches a single markdown link (not an image); [^)]* covers
// optional "title" attributes inside the parens.
var mdInlineLinkRe = regexp.MustCompile(`!?\[[^\]]*\]\([^)]*\)`)

// stripLinkClusters removes navigation/table-of-contents boilerplate that
// survives HTML→markdown conversion as "link soup" — blocks where link
// syntax crowds out any real prose. Two rules, both fence-aware:
//  1. footnote/citation markers [\[15\]](…) are stripped wherever they appear
//  2. non-list blocks that are ≥4 links and ≥85% link syntax are dropped
//     (infoboxes, inline nav menus); bullet lists are preserved because
//     docs index/TOC sections legitimately look like link lists
func stripLinkClusters(md string) string {
	var b strings.Builder
	b.Grow(len(md))
	inFence := false
	blanks := 0
	for line := range strings.Lines(md) {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			blanks = 0
			b.WriteString(line + "\n")
			continue
		}
		if inFence {
			b.WriteString(line + "\n")
			continue
		}
		line = footnoteRe.ReplaceAllString(line, "")
		trim := strings.TrimSpace(line)
		if trim != "" && isLinkSoup(trim) {
			continue
		}
		if trim == "" && blanks > 0 {
			continue // collapse blank runs left by removed blocks
		}
		if trim == "" {
			blanks++
		} else {
			blanks = 0
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// isLinkSoup reports whether a line is ≥4 markdown links covering ≥85% of
// its text — the signature of nav menus and infobox blobs. List items and
// headings never qualify: leading -, *, digits, # are real structure.
func isLinkSoup(line string) bool {
	if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") ||
		strings.HasPrefix(line, "#") || line[0] >= '0' && line[0] <= '9' {
		return false
	}
	links := mdInlineLinkRe.FindAllString(line, -1)
	if len(links) < 4 {
		return false
	}
	linkLen := 0
	for _, l := range links {
		linkLen += len(l)
	}
	// whitespace doesn't count against density — nav lines pad with spaces
	dense := len(line) - strings.Count(line, " ") - strings.Count(line, "\t")
	return dense > 0 && float64(linkLen)/float64(dense) >= 0.85
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

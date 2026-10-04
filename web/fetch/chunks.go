package fetch

// chunks.go — RAG-ready output: markdown split on heading boundaries into
// breadcrumb-annotated chunks with token estimates (the Exa `context` /
// Jina-segment idea, deterministic and free).
import (
	"regexp"
	"strings"
)

// Chunk is one addressable piece of a document.
type Chunk struct {
	HeadingPath string `json:"heading_path,omitempty"` // "Guide > Install > Linux"
	Text        string `json:"text"`
	EstTokens   int    `json:"est_tokens"`
}

// estTokens is the honest cheap estimator: ~4 chars/token for English
// prose. Reported as an estimate, never billed as exact.
func estTokens(s string) int { return (len(s) + 3) / 4 }

var mdHeadingRe = regexp.MustCompile(`(?m)^(#{1,6})\s+(.*\S)\s*$`)

const chunkTargetChars = 1400 // ~350 tokens — RAG-friendly size

// ChunkMarkdown splits markdown at heading boundaries, carrying a
// breadcrumb ("A > B > C") for each chunk; oversized sections split
// further at blank lines. Order is document order.
func ChunkMarkdown(md string) []Chunk {
	var chunks []Chunk
	var path []string // path[level] = last heading at that depth
	var buf strings.Builder

	flush := func() {
		text := strings.TrimSpace(buf.String())
		buf.Reset()
		if text == "" {
			return
		}
		for len(text) > chunkTargetChars*2 {
			// Split oversized sections at a paragraph break near the target.
			cut := strings.LastIndex(text[:chunkTargetChars*2], "\n\n")
			if cut < chunkTargetChars/2 {
				break
			}
			chunks = append(chunks, Chunk{HeadingPath: strings.Join(path, " > "),
				Text: text[:cut], EstTokens: estTokens(text[:cut])})
			text = strings.TrimSpace(text[cut:])
		}
		if text != "" {
			chunks = append(chunks, Chunk{HeadingPath: strings.Join(path, " > "),
				Text: text, EstTokens: estTokens(text)})
		}
	}

	for _, line := range strings.Split(md, "\n") {
		if m := mdHeadingRe.FindStringSubmatch(line); m != nil {
			flush()
			level := len(m[1])
			if len(path) >= level {
				path = path[:level-1]
			}
			for len(path) < level-1 {
				path = append(path, "")
			}
			path = append(path, m[2])
			buf.WriteString(line + "\n")
			continue
		}
		buf.WriteString(line + "\n")
	}
	flush()
	return chunks
}

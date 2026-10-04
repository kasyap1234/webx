package fetch

import (
	"bytes"
	"strings"

	"github.com/ledongthuc/pdf"
)

// isPDF detects PDFs by content-type or a .pdf path — covers servers that
// mislabel octet-stream, the common case for spec/RFC downloads.
func isPDF(contentType, rawURL string) bool {
	if strings.Contains(contentType, "application/pdf") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(rawURL), ".pdf")
}

// pdfText extracts readable text page-by-page. Text-layer PDFs only —
// scanned images return little; that's an honest limitation, not a bug.
func pdfText(body []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String()), nil
}

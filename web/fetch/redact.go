package fetch

// redact.go — PII redaction as a markdown format (formats:["redact_pii"]).
// Deterministic regex passes over the extracted markdown — emails, phone
// numbers, card-number runs, SSNs. Deliberately conservative: it masks
// things that LOOK like PII rather than trying to be a classifier, so
// false positives (a 10-digit ISBN) are possible and false negatives
// (an SSN written "123 45 6789") aren't promised away.
import "regexp"

var (
	// user@domain.tld — allow the usual +/%/=. local-part set.
	piiEmail = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	// SSN — the canonical xxx-xx-xxxx shape (not loose digit runs).
	piiSSN = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	// Card numbers — 13–19 digits, optionally separated by single spaces
	// or dashes between 4-digit groups.
	piiCard = regexp.MustCompile(`\b(?:\d{4}[ -]?){3}\d{1,4}(?:\d{3})?\b`)
	// Phone — international +N or (NNN) NNN-NNNN shapes; requires 10+
	// digits so a bare "555-1212" or year doesn't redact.
	piiPhone = regexp.MustCompile(`(?:\+\d{1,3}[ .-]?)?(?:\(\d{3}\)|\d{3})[ .-]\d{3}[ .-]\d{4}\b`)
	// IP addresses are PII under GDPR/CCPA when tied to a person —
	// and harmless to mask for agents anyway.
	piiIPv4 = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
)

// RedactPII masks common PII patterns in text — the same output shape
// Jina/Firecrawl sell as a compliance feature, done deterministically
// so agents can rely on it in a data-pipeline step.
func RedactPII(s string) string {
	s = piiEmail.ReplaceAllString(s, "[email]")
	s = piiSSN.ReplaceAllString(s, "[ssn]")
	s = piiCard.ReplaceAllString(s, "[card]")
	s = piiPhone.ReplaceAllString(s, "[phone]")
	s = piiIPv4.ReplaceAllString(s, "[ip]")
	return s
}

// Redact masks PII across a Document's text surfaces — markdown, the
// BM25-fit variant, and the raw HTML. Transcript text is markdown-bound
// already. Titles are left alone (they're metadata, not body content).
func (d *Document) Redact() {
	d.Markdown = RedactPII(d.Markdown)
	d.FitMarkdown = RedactPII(d.FitMarkdown)
	d.HTML = RedactPII(d.HTML)
	if d.Transcript != nil {
		d.Transcript.Text = RedactPII(d.Transcript.Text)
	}
}

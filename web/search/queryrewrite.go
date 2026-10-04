package search

import (
	"regexp"
	"strings"
)

// rewriter is implemented by providers that do better on keyword queries
// than natural language — Q&A/code APIs ("what is a goroutine" → "goroutine").
// Called per-provider before Search; providers without it get the raw query.
type rewriter interface {
	Rewrite(req Request) Request
}

var questionPrefixRe = regexp.MustCompile(`(?i)^(what is a|what is an|what is|what's|what are|how do i|how do you|how to|how can i|how does|why is|why does|explain|tell me about|show me|example of|difference between)\s+`)

// rewriteKeywords strips conversational filler — keyword-style providers get
// "concurrent map writes golang" rather than "how do I fix concurrent map
// writes in golang".
func rewriteKeywords(req Request) Request {
	q := questionPrefixRe.ReplaceAllString(req.Query, "")
	q = strings.TrimSuffix(strings.TrimSpace(q), "?")
	if q == "" {
		return req // never send an empty query
	}
	req.Query = q
	return req
}

// Providers below prefer keywords over natural language.
func (s *stackoverflow) Rewrite(req Request) Request { return rewriteKeywords(req) }
func (w *wikipedia) Rewrite(req Request) Request     { return rewriteKeywords(req) }
func (g *grepapp) Rewrite(req Request) Request       { return rewriteKeywords(req) }
func (s *sourcegraph) Rewrite(req Request) Request   { return rewriteKeywords(req) }
func (n *npmjs) Rewrite(req Request) Request         { return rewriteKeywords(req) }
func (c *cratesio) Rewrite(req Request) Request      { return rewriteKeywords(req) }

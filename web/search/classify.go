package search

import (
	"regexp"
	"strings"
)

// QueryKind is a lightweight classification of a search query's shape —
// the "autoprompt" equivalent that routes weight toward the providers most
// likely to answer this kind of question.
type QueryKind int

const (
	KindConcept    QueryKind = iota // prose questions, concepts, comparisons
	KindErrorish                    // error messages, stack traces, symptoms
	KindIdentifier                  // code identifiers: mux.HandleFunc, os::path
	KindHowTo                       // "how do I", "how to", "example of"
	KindFresh                       // releases, changelogs, "latest" — freshness matters
)

// String names the kind for eval breakdowns and debugging.
func (k QueryKind) String() string {
	switch k {
	case KindErrorish:
		return "errorish"
	case KindIdentifier:
		return "identifier"
	case KindHowTo:
		return "howto"
	case KindFresh:
		return "fresh"
	default:
		return "concept"
	}
}

var (
	errorishRe = regexp.MustCompile(`(?i)\b(error|err|exception|traceback|fatal|panic|segfault|stacktrace|errno|exit code|cannot |can't |failed to|not defined|undefined|denied|refused|timed? ?out)\b|File "[^"]+"|\w+\.(go|py|rs|ts|js|java|c|cpp|rb):\d+|panic\(|TypeError|ValueError|NullPointer`)
	// errorCodeRe is deliberately case-sensitive — E-codes and Java-style
	// Exception names are only meaningful when actually capitalized.
	errorCodeRe = regexp.MustCompile(`\bE[A-Z_]{4,}\b|\b[A-Z]\w+Exception\b|error TS\d+`)
	identRe     = regexp.MustCompile(`\b\w+(\.\w+)+(\(\)?|\))|\w+::\w+|\b[a-z]+[A-Z]\w*\(|\bget[A-Z]\w*|/\w+/\w+\b`)
	camelRe     = regexp.MustCompile(`\b[a-z]+[A-Z]\w*`)
	howtoRe     = regexp.MustCompile(`(?i)^(how (do|to|can)|what is|what's|example of|difference between|vs\.? |versus )\b`)
	freshRe     = regexp.MustCompile(`(?i)\b(release|released|changelog|latest|announced|announcement|news|v\d+\.\d+|20\d\d)\b`)
)

// Classify inspects the query shape.
func Classify(query string) QueryKind {
	q := strings.TrimSpace(query)
	switch {
	case freshRe.MatchString(q):
		return KindFresh
	case errorishRe.MatchString(q) || errorCodeRe.MatchString(q):
		return KindErrorish
	case isIdentifierish(q):
		return KindIdentifier
	case howtoRe.MatchString(q):
		return KindHowTo
	default:
		return KindConcept
	}
}

// isIdentifierish: code-identifier shape — dotted/scoped/camel token present
// and the query is short enough to be about that identifier ("useEffect
// react", "express.Router node", "Vec::with_capacity rust").
func isIdentifierish(q string) bool {
	if len(strings.Fields(q)) > 3 {
		return false
	}
	return identRe.MatchString(q) || camelRe.MatchString(q) ||
		strings.Contains(q, ".") || strings.Contains(q, "::")
}

// baseProviderWeights biases each provider's RRF contribution; queryKind
// then adjusts. Values are multipliers on 1/(k+rank).
var baseProviderWeights = map[string]float64{
	"index":   1.5, // own corpus, full-text — wins ties
	"searxng": 1.2, // fused upstream, broad coverage
	"brave":   1.2,
	"ddg":     1.0,
	"hn":      0.9, // narrow but high-signal
	"so":      1.1,
	"wiki":    0.7, // background context, rarely the direct answer
	"gh":      1.1,
	"reddit":  0.8,
	"sg":      1.0, // code search — precision for identifiers
	"grep":    1.0,
	"npm":     1.0,
	"crates":  1.0,
}

// weightsFor adjusts base weights for the query kind — this is where "search
// tuned for coding agents" lives.
func weightsFor(kind QueryKind) map[string]float64 {
	w := make(map[string]float64, len(baseProviderWeights))
	for k, v := range baseProviderWeights {
		w[k] = v
	}
	switch kind {
	case KindErrorish:
		w["so"] *= 2.2
		w["gh"] *= 1.6
		w["reddit"] *= 1.5 // community threads often carry the fix
		w["wiki"] *= 0.4
	case KindIdentifier:
		w["index"] *= 2.0
		w["gh"] *= 1.8
		w["sg"] *= 1.8 // literal code search
		w["grep"] *= 1.8
		w["npm"] *= 1.5 // package page IS the docs entry point
		w["crates"] *= 1.5
		w["so"] *= 1.4
		w["ddg"] *= 1.1
	case KindHowTo:
		w["index"] *= 1.6
		w["so"] *= 1.4
		w["hn"] *= 1.1
	case KindConcept:
		w["hn"] *= 1.3
		w["wiki"] *= 1.5
	case KindFresh:
		w["hn"] *= 1.8 // release/news discussion
		w["gh"] *= 1.5 // releases live in repos
		w["reddit"] *= 1.4
		w["searxng"] *= 1.3 // carries publishedDate
		w["so"] *= 0.5
		w["wiki"] *= 0.6
	}
	return w
}

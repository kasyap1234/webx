package search

import (
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// rrfK is the reciprocal-rank-fusion constant; k=60 is the standard value
// used by Elasticsearch/OpenSearch. Higher values flatten rank influence.
const rrfK = 60.0

// fuse merges provider result lists via weighted reciprocal rank fusion,
// dedupes by normalized URL, applies domain priors, then filters and sorts.
func fuse(lists map[string][]Result, req Request) []Result {
	type merged struct {
		Result
		score float64
	}
	byKey := map[string]*merged{}
	kind := Classify(req.Query)
	weights := weightsFor(kind)

	// bm25BlendW scales provider-native scores (index bm25) when blended into
	// the RRF sum — ~0.3 of a rank-1 vote so strong full-text matches matter
	// without letting one provider's score scale dominate rank evidence.
	const bm25BlendW = 0.3

	for provider, results := range lists {
		pw := weights[provider]
		if pw == 0 {
			pw = 1.0
		}
		var maxRaw float64
		for _, r := range results {
			if r.RawScore > maxRaw {
				maxRaw = r.RawScore
			}
		}
		for i, r := range results {
			key := normalizeURL(r.URL)
			if key == "" {
				continue
			}
			contribution := pw / (rrfK + float64(i) + 1.0)
			if maxRaw > 0 && r.RawScore > 0 {
				contribution += bm25BlendW * pw * r.RawScore / maxRaw
			}
			m, ok := byKey[key]
			if !ok {
				m = &merged{Result: r}
				m.URL = key
				m.Sources = []string{provider}
				byKey[key] = m
			} else {
				dup := false
				for _, s := range m.Sources {
					if s == provider {
						dup = true
						break
					}
				}
				if !dup {
					m.Sources = append(m.Sources, provider)
				}
				if len(r.Title) > len(m.Title) {
					m.Title = r.Title
				}
				if len(r.Snippet) > len(m.Snippet) {
					m.Snippet = r.Snippet
				}
				if m.Published.IsZero() {
					m.Published = r.Published
				}
			}
			m.score += contribution
		}
	}

	qTerms := queryTerms(req.Query)
	out := make([]Result, 0, len(byKey))
	for _, m := range byKey {
		r := m.Result
		score := m.score * domainBoost(keyURLHost(r.URL))
		score *= titleMatchBoost(r.Title, qTerms)
		if kind == KindFresh || req.Topic == "news" {
			score *= freshnessFactor(r.Published)
		}
		r.Score = score
		if req.Site != "" && !hostMatches(r.URL, req.Site) {
			continue
		}
		if len(req.Domains) > 0 && !hostMatchesAny(r.URL, req.Domains) {
			continue
		}
		if len(req.ExcludeDomains) > 0 && hostMatchesAny(r.URL, req.ExcludeDomains) {
			continue
		}
		if !req.After.IsZero() && !r.Published.IsZero() && r.Published.Before(req.After) {
			continue
		}
		if !req.Before.IsZero() && !r.Published.IsZero() && r.Published.After(req.Before) {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > req.Num {
		out = out[:req.Num]
	}
	return out
}

// trackingParams are dropped before dedup so the same article shared with
// different campaign tags collapses to one result.
var trackingParams = map[string]bool{
	"fbclid": true, "gclid": true, "dclid": true, "msclkid": true,
	"ref": true, "source": true, "mc_cid": true, "mc_eid": true,
	"igshid": true, "si": true,
}

func normalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	u.Host = strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	u.Fragment = ""
	u.RawFragment = ""
	if u.Path != "/" {
		u.Path = strings.TrimSuffix(u.Path, "/")
	}
	q := u.Query()
	for k := range q {
		if trackingParams[k] || strings.HasPrefix(k, "utm_") {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	if c := canonicalURL(u); c != nil {
		u = c
	}
	return u.String()
}

var (
	// Stack Overflow / Stack Exchange: /questions/{id}/{slug} -> /questions/{id}
	seQuestionRe = regexp.MustCompile(`^/(?:questions|q)/(\d+)`)
	seAnswerRe   = regexp.MustCompile(`^/a/(\d+)`)
	// Reddit: /r/{sub}/comments/{id}/{slug} -> /r/{sub}/comments/{id}
	redditPostRe = regexp.MustCompile(`^(/r/[^/]+/comments/[a-z0-9]+)`)
)

// canonicalURL rewrites links on sites whose URL paths carry cosmetic
// slugs, so the same question/post found via different URLs dedupes.
func canonicalURL(u *url.URL) *url.URL {
	host := u.Host
	isSE := strings.HasSuffix(host, "stackexchange.com") ||
		host == "stackoverflow.com" || host == "superuser.com" ||
		host == "serverfault.com" || host == "askubuntu.com"
	if isSE {
		if m := seQuestionRe.FindStringSubmatch(u.Path); m != nil {
			return &url.URL{Scheme: u.Scheme, Host: host, Path: "/questions/" + m[1]}
		}
		if m := seAnswerRe.FindStringSubmatch(u.Path); m != nil {
			return &url.URL{Scheme: u.Scheme, Host: host, Path: "/a/" + m[1]}
		}
	}
	if strings.HasSuffix(host, "reddit.com") {
		if m := redditPostRe.FindStringSubmatch(u.Path); m != nil {
			return &url.URL{Scheme: u.Scheme, Host: host, Path: m[1]}
		}
	}
	return nil
}

func keyURLHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Host
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return strings.ToLower(host)
}

func hostMatches(raw, site string) bool {
	host := keyURLHost(raw)
	site = strings.ToLower(strings.TrimSpace(site))
	return host == site || strings.HasSuffix(host, "."+site)
}

func hostMatchesAny(raw string, sites []string) bool {
	for _, s := range sites {
		if hostMatches(raw, s) {
			return true
		}
	}
	return false
}

// domainBoost rewards sources that are authoritative for coding-agent
// queries: official docs, the language/ecosystem hubs, primary Q&A sites.
// Deliberately a small, hand-tuned table — this is the "search for agents"
// differentiator and should stay legible.
var domainBoostTable = map[string]float64{
	"developer.mozilla.org": 1.6,
	"docs.python.org":       1.6,
	"pkg.go.dev":            1.6,
	"go.dev":                1.5,
	"learn.microsoft.com":   1.5,
	"doc.rust-lang.org":     1.5,
	"react.dev":             1.5,
	"kubernetes.io":         1.4,
	"docs.rs":               1.5,
	"developer.android.com": 1.5,
	"man7.org":              1.4,
	"devdocs.io":            1.3,
	"github.com":            1.35,
	"stackoverflow.com":     1.3,
	"stackexchange.com":     1.25,
	"news.ycombinator.com":  1.1,
	"arxiv.org":             1.25,
	"en.wikipedia.org":      1.1,
	"geeksforgeeks.org":     0.9,
	"medium.com":            0.85,
	"dev.to":                0.95,
	"readthedocs.io":        1.2,
	"gitbook.io":            1.15,
	"docs.github.com":       1.5,
	"docs.aws.amazon.com":   1.5,
	"cloud.google.com":      1.4,
	"learnk8s.io":           1.2,
	"web.dev":               1.3,
	"developer.chrome.com":  1.3,
	"sourcegraph.com":       1.1,
	"htmx.org":              1.3,
	"sqlite.org":            1.3,
	"postgresql.org":        1.4,
	"redis.io":              1.3,
	"nodejs.org":            1.4,
	"npmjs.com":             1.3,
	"pypi.org":              1.3,
	"crates.io":             1.3,
}

// titleMatchBoost rewards results whose title contains the query terms —
// a cheap proxy for topicality that rank position alone misses.
func titleMatchBoost(title string, terms []string) float64 {
	if len(terms) == 0 || title == "" {
		return 1.0
	}
	t := strings.ToLower(title)
	var hits int
	for _, w := range terms {
		if strings.Contains(t, w) {
			hits++
		}
	}
	if hits == 0 {
		return 1.0
	}
	frac := float64(hits) / float64(len(terms))
	return 1.0 + 0.3*frac // up to +30% when every term is in the title
}

// freshnessFactor applies recency decay for freshness-shaped queries —
// half-life ~120 days, bounded to [0.6, 1.0] so old-but-correct pages aren't
// destroyed, and unknown dates stay neutral-ish.
func freshnessFactor(p time.Time) float64 {
	if p.IsZero() {
		return 0.9
	}
	days := time.Since(p).Hours() / 24
	if days < 0 {
		days = 0
	}
	f := math.Exp(-days / 172) // ln(2)/120 ≈ 1/172.8
	return 0.6 + 0.4*f
}

var boostedPrefixes = []string{"docs.", "developer.", "dev.", "learn.", "wiki.", "reference.", "api."}

func domainBoost(host string) float64 {
	if b, ok := domainBoostTable[host]; ok {
		return b
	}
	for h, b := range domainBoostTable {
		if strings.HasSuffix(host, "."+h) {
			return b
		}
	}
	for _, p := range boostedPrefixes {
		if strings.HasPrefix(host, p) {
			return 1.2
		}
	}
	return 1.0
}

package fetch

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

// RobotsChecker caches per-host robots.txt rules — fetched once per host
// per run, missing/unreachable robots = allowed (standard interpretation).
type RobotsChecker struct {
	mu    sync.Mutex
	rules map[string]*robotRules // host -> parsed rules
}

func NewRobotsChecker() *RobotsChecker {
	return &RobotsChecker{rules: map[string]*robotRules{}}
}

type robotRule struct {
	pattern *regexp.Regexp
	patLen  int
	allow   bool
}

type robotRules struct {
	rules []robotRule
}

// Allowed reports whether robots.txt permits fetching u — the caller's
// obligation to honor --respect-robots.
func (rc *RobotsChecker) Allowed(ctx context.Context, u *url.URL) bool {
	host := strings.ToLower(u.Host)
	rc.mu.Lock()
	rr, ok := rc.rules[host]
	rc.mu.Unlock()
	if !ok {
		rr = fetchRobots(ctx, u)
		rc.mu.Lock()
		rc.rules[host] = rr
		rc.mu.Unlock()
	}
	return rr.allowed(u.Path + u.RawQuery)
}

var robotsClient = &http.Client{Timeout: 10 * time.Second}

// fetchRobots downloads and parses robots.txt for u's host.
// Rules for User-agent "webx" beat "*"; absent = allow all.
func fetchRobots(ctx context.Context, u *url.URL) *robotRules {
	rr := &robotRules{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		u.Scheme+"://"+u.Host+"/robots.txt", nil)
	if err != nil {
		return rr
	}
	resp, err := robotsClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return rr // unreachable/missing robots → allowed by convention
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))

	var ours, star []robotRule
	var curAllow, curDisallow []robotRule
	var curIsOurs, curIsStar, inGroup, sawRules bool
	flush := func() {
		if curIsOurs {
			ours = append(ours, curAllow...)
			ours = append(ours, curDisallow...)
		}
		if curIsStar {
			star = append(star, curAllow...)
			star = append(star, curDisallow...)
		}
		curAllow, curDisallow = nil, nil
		curIsOurs, curIsStar, sawRules = false, false, false
	}
	sc := bufio.NewScanner(strings.NewReader(string(body)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		field, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		field, val = strings.ToLower(strings.TrimSpace(field)), strings.TrimSpace(val)
		switch field {
		case "user-agent":
			// A user-agent line after rules have appeared starts a new group.
			if sawRules {
				flush()
			}
			inGroup = true
			ua := strings.ToLower(val)
			if ua == "webx" {
				curIsOurs = true
			}
			if ua == "*" {
				curIsStar = true
			}
		case "disallow", "allow":
			if !inGroup || val == "" {
				continue
			}
			sawRules = true
			re, err := ruleRegexp(val)
			if err != nil {
				continue
			}
			rule := robotRule{pattern: re, patLen: len(val), allow: field == "allow"}
			if field == "allow" {
				curAllow = append(curAllow, rule)
			} else {
				curDisallow = append(curDisallow, rule)
			}
		}
	}
	flush()

	if len(ours) > 0 {
		rr.rules = ours
	} else {
		rr.rules = star
	}
	return rr
}

// ruleRegexp compiles a robots pattern — `*` wildcards and `$` end-anchor.
func ruleRegexp(p string) (*regexp.Regexp, error) {
	end := strings.HasSuffix(p, "$")
	p = strings.TrimSuffix(p, "$")
	var b strings.Builder
	b.WriteString("^")
	for _, r := range p {
		switch r {
		case '*':
			b.WriteString(".*")
		default:
			fmt.Fprintf(&b, "%s", regexp.QuoteMeta(string(r)))
		}
	}
	if end {
		b.WriteString("$")
	}
	return regexp.Compile(b.String())
}

// allowed applies longest-match-wins, Allow beating Disallow on ties.
func (rr *robotRules) allowed(urlPath string) bool {
	if len(rr.rules) == 0 {
		return true
	}
	urlPath = path.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	best := -1
	allow := true
	for _, r := range rr.rules {
		if !r.pattern.MatchString(urlPath) {
			continue
		}
		if r.patLen > best || (r.patLen == best && r.allow) {
			best, allow = r.patLen, r.allow
		}
	}
	return allow
}

package eval

// Corpus generation — 30 hand-picked queries prove nothing at ±18% CI. This
// builds a 1000+ query set from real data where the expected answer is
// grounded, not guessed:
//
//   ORCAS   — 18.8M real Bing query→clicked-URL pairs (Microsoft). We stream
//             the head of the public gzip; the clicked host IS the qrel —
//             it's what a real user actually wanted.
//   Stack Exchange — top-voted question titles across network sites; the
//             question's own site is the expected host.
//   GitHub  — top-starred repo descriptions as "find the tool" queries;
//             github.com is the expected host.
//
// Queries are deduped; expected hosts come from real clicks/known sources, so
// hit-rate measures "did we surface what users actually wanted".

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// GenOpts controls corpus composition.
type GenOpts struct {
	ORCAS  int            // pairs to pull from the ORCAS stream head
	SE     map[string]int // SE site → question count
	GitHub int            // repo-description queries
	Out    string         // write the generated set here (empty → skip)
}

// DefaultGen targets ~1200 queries.
func DefaultGen() GenOpts {
	return GenOpts{
		ORCAS: 700,
		SE: map[string]int{
			"stackoverflow": 150,
			"superuser":     80,
			"serverfault":   80,
			"askubuntu":     80,
			"unix":          60,
			"apple":         50,
		},
		GitHub: 150,
	}
}

const orcasURL = "https://msmarco.z22.web.core.windows.net/msmarcoranking/orcas.tsv.gz"

var (
	genHTTP   = &http.Client{Timeout: 60 * time.Second}
	orcasHTTP = &http.Client{Timeout: 10 * time.Minute} // 330MB stream
)

// Generate builds the corpus. Source failures degrade the set size, not the
// run — a partial corpus with an honest count beats a dead one.
func Generate(ctx context.Context, o GenOpts, logf func(string, ...any)) (Set, error) {
	var s Set
	seen := map[string]bool{}
	add := func(q string, expect []string) bool {
		q = strings.Join(strings.Fields(q), " ")
		if q == "" || len(q) < 8 || seen[strings.ToLower(q)] {
			return false
		}
		seen[strings.ToLower(q)] = true
		s.Queries = append(s.Queries, Case{Query: q, Expect: expect})
		return true
	}

	if o.ORCAS > 0 {
		n := genORCAS(ctx, o.ORCAS, add, logf)
		if logf != nil {
			logf("orcas: %d queries", n)
		}
	}
	for site, want := range o.SE {
		n := genStackExchange(ctx, site, want, add)
		if logf != nil {
			logf("stackexchange/%s: %d queries", site, n)
		}
		time.Sleep(300 * time.Millisecond) // anonymous quota: ~300 req/day
	}
	if o.GitHub > 0 {
		n := genGitHub(ctx, o.GitHub, add)
		if logf != nil {
			logf("github: %d queries", n)
		}
	}
	return s, nil
}

// genORCAS reservoir-samples the whole ORCAS stream — the file is sorted, so
// reading the head alone gives an early-alphabet-biased corpus. Algorithm R
// gives a uniform random sample in one pass over ~330MB.
func genORCAS(ctx context.Context, want int, add func(string, []string) bool, logf func(string, ...any)) int {
	req, err := http.NewRequestWithContext(ctx, "GET", orcasURL, nil)
	if err != nil {
		return 0
	}
	resp, err := orcasHTTP.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if logf != nil {
			logf("orcas: fetch failed (%v) — skipping", err)
		}
		if resp != nil {
			resp.Body.Close()
		}
		return 0
	}
	defer resp.Body.Close()
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return 0
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	type pair struct{ q, host string }
	reservoir := make([]pair, 0, want)
	sc := bufio.NewScanner(gz)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	seenLine := 0
	for sc.Scan() {
		f := strings.SplitN(sc.Text(), "\t", 4)
		if len(f) < 4 {
			continue
		}
		query, raw := f[1], f[3]
		// Skip pure navigational queries (single word / URL-shaped / mostly
		// punctuation) — they measure nothing about ranking quality.
		if strings.Count(strings.TrimSpace(query), " ") < 1 || strings.Contains(query, "http") ||
			len(alphaOnly(query)) < 4 {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			continue
		}
		h := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
		if h == "" {
			continue
		}
		seenLine++
		if len(reservoir) < want {
			reservoir = append(reservoir, pair{query, h})
		} else if j := rng.Intn(seenLine); j < want {
			reservoir[j] = pair{query, h}
		}
		if ctx.Err() != nil {
			break
		}
	}
	if logf != nil {
		logf("orcas: scanned %d eligible pairs for %d-sample", seenLine, want)
	}
	n := 0
	for _, p := range reservoir {
		if add(p.q, []string{p.host}) {
			n++
		}
	}
	return n
}

func alphaOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// genStackExchange pulls top-voted question titles — real things developers
// typed — with the site itself as the expected host.
func genStackExchange(ctx context.Context, site string, want int, add func(string, []string) bool) int {
	domain := site
	if !strings.Contains(site, ".") {
		domain = site + ".stackexchange.com"
	}
	if site == "stackoverflow" {
		domain = "stackoverflow.com"
	} else if site == "superuser" {
		domain = "superuser.com"
	} else if site == "serverfault" {
		domain = "serverfault.com"
	} else if site == "askubuntu" {
		domain = "askubuntu.com"
	}
	per := want
	if per > 100 {
		per = 100
	}
	got := 0
	for page := 1; got < want && page <= 3; page++ {
		u := fmt.Sprintf("https://api.stackexchange.com/2.3/questions?order=desc&sort=votes&site=%s&pagesize=%d&page=%d&filter=withbody",
			site, per, page)
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return got
		}
		resp, err := genHTTP.Do(req)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			return got
		}
		var payload struct {
			Items []struct {
				Title string `json:"title"`
			} `json:"items"`
		}
		err = json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if err != nil {
			return got
		}
		for _, it := range payload.Items {
			title := html.UnescapeString(it.Title)
			if got >= want {
				break
			}
			if len(title) < 12 {
				continue
			}
			if add(title, []string{domain}) {
				got++
			}
		}
		if len(payload.Items) < per {
			break
		}
	}
	return got
}

// genGitHub turns top-starred repo descriptions into "find the tool" queries
// across languages — the navigational intent is real (users search these).
func genGitHub(ctx context.Context, want int, add func(string, []string) bool) int {
	langs := []string{"go", "python", "typescript", "rust"}
	per := want / len(langs)
	if per < 1 {
		per = 1
	}
	got := 0
	for _, lang := range langs {
		u := fmt.Sprintf("https://api.github.com/search/repositories?q=language:%s+stars:>2000&sort=stars&order=desc&per_page=%d",
			lang, per)
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return got
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := genHTTP.Do(req)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			return got
		}
		var payload struct {
			Items []struct {
				Name string `json:"name"`
				Desc string `json:"description"`
			} `json:"items"`
		}
		err = json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if err != nil {
			return got
		}
		for _, it := range payload.Items {
			if got >= want {
				break
			}
			q := it.Desc
			if q == "" || len(q) < 20 || len(q) > 140 {
				continue
			}
			if add(q+" "+it.Name, []string{"github.com"}) {
				got++
			}
		}
		time.Sleep(2 * time.Second) // anonymous search rate limit
	}
	return got
}

// Write serializes the set as YAML for reuse with --set.
func (s Set) Write(path string) error {
	var b strings.Builder
	b.WriteString("# generated eval corpus — see webx eval --gen\nqueries:\n")
	for _, c := range s.Queries {
		fmt.Fprintf(&b, "  - q: %s\n    expect: [%s]\n",
			yamlQuote(c.Query), strings.Join(c.Expect, ", "))
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func yamlQuote(s string) string {
	e := strings.NewReplacer("\\", "\\\\", "\"", "\\\"")
	return "\"" + e.Replace(s) + "\""
}

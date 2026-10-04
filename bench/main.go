// webx-bench — comparative scrape benchmark: webx vs external providers.
//
// Runs a fixed URL corpus through each configured provider and reports
// latency (p50/p95), success rate, and extracted-content volume. Providers
// needing keys are skipped unless their env var is set:
//
//	WEBX_BENCH_BASE   webx server   (default http://localhost:8080)
//	FIRECRAWL_API_KEY api.firecrawl.dev/v2
//	TAVILY_API_KEY    api.tavily.com
//	JINA_API_KEY      r.jina.ai (optional — anonymous tier works)
//
//	webx and jina+raw need no keys, so `go run ./bench` works out of the box.
//
//	go run ./bench -n 2 -warm          # median of 2, plus a warm-cache pass
//	go run ./bench -out results.json   # machine-readable report
//
// Honest benchmarking rules baked in: every provider sees the same corpus in
// the same order; failures are recorded not hidden; a raw-HTTP baseline shows
// the floor so render/extraction overhead is visible, not laundered.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Case is one corpus line: "url | substring-that-must-appear" (substring optional).
type Case struct {
	URL    string
	Expect string
}

// Result is one provider × one URL.
type Result struct {
	URL      string `json:"url"`
	MS       int64  `json:"ms"`
	Bytes    int    `json:"bytes"` // extracted-content size (markdown/text), not raw HTML
	OK       bool   `json:"ok"`
	ExpectOK bool   `json:"expect_ok"` // content check passed (or no expectation)
	Error    string `json:"error,omitempty"`
	Tier     string `json:"tier,omitempty"` // webx render tier when reported
}

// Provider is a scraper we can benchmark. Extract returns content text.
type Provider struct {
	Name    string
	EnvKey  string // required env var ("" = always on)
	Extract func(ctx context.Context, url string) (content string, tier string, err error)
}

func (p Provider) Available() bool {
	return p.EnvKey == "" || os.Getenv(p.EnvKey) != ""
}

var httpc = &http.Client{Timeout: 90 * time.Second}

func doJSON(ctx context.Context, method, url string, hdr map[string]string,
	body any, out any) (raw []byte, status int, err error) {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if out != nil && resp.StatusCode < 400 {
		_ = json.Unmarshal(raw, out)
	}
	return raw, resp.StatusCode, err
}

// ── providers ────────────────────────────────────────────────────────────────

func webxProvider() Provider {
	base := os.Getenv("WEBX_BENCH_BASE")
	if base == "" {
		base = "http://localhost:8080"
	}
	key := os.Getenv("WEBX_API_KEY")
	return Provider{Name: "webx", Extract: func(ctx context.Context, url string) (string, string, error) {
		var out struct {
			Data struct {
				Markdown string `json:"markdown"`
				Title    string `json:"title"`
				TierUsed string `json:"tier_used"`
			} `json:"data"`
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		hdr := map[string]string{}
		if key != "" {
			hdr["Authorization"] = "Bearer " + key
		}
		raw, status, err := doJSON(ctx, "POST", base+"/scrape", hdr,
			map[string]any{"url": url, "formats": []string{"markdown"},
				// Fair comparison: jina/firecrawl render JS server-side by
				// default — webx escalates only when the page demands it.
				"auto_render": true}, &out)
		if err != nil {
			return "", "", err
		}
		if status >= 400 {
			var e struct{ Error, Code string }
			_ = json.Unmarshal(raw, &e)
			return "", "", fmt.Errorf("%d %s %s", status, e.Code, e.Error)
		}
		// Jina/Firecrawl fold the page title into their markdown; webx
		// keeps it in metadata. Concatenate so byte/content comparisons
		// measure the same agent-usable payload.
		return out.Data.Title + "\n" + out.Data.Markdown, out.Data.TierUsed, nil
	}}
}

func rawProvider() Provider {
	return Provider{Name: "raw-http", Extract: func(ctx context.Context, url string) (string, string, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		req.Header.Set("User-Agent", "webx-bench/1.0")
		resp, err := httpc.Do(req)
		if err != nil {
			return "", "", err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if resp.StatusCode >= 400 {
			return "", "", fmt.Errorf("http %d", resp.StatusCode)
		}
		return string(raw), "raw", err
	}}
}

func jinaProvider() Provider {
	return Provider{Name: "jina-reader", EnvKey: "", Extract: func(ctx context.Context, url string) (string, string, error) {
		hdr := map[string]string{"X-Return-Format": "markdown"}
		if k := os.Getenv("JINA_API_KEY"); k != "" {
			hdr["Authorization"] = "Bearer " + k
		}
		var discard any
		raw, status, err := doJSON(ctx, "GET", "https://r.jina.ai/"+url, hdr, nil, &discard)
		if err != nil {
			return "", "", err
		}
		if status >= 400 {
			return "", "", fmt.Errorf("http %d: %.120s", status, raw)
		}
		return string(raw), "cloud", nil
	}}
}

func firecrawlProvider() Provider {
	return Provider{Name: "firecrawl", EnvKey: "FIRECRAWL_API_KEY",
		Extract: func(ctx context.Context, url string) (string, string, error) {
			var out struct {
				Data    struct{ Markdown string } `json:"data"`
				Success bool                      `json:"success"`
				Error   string                    `json:"error"`
			}
			raw, status, err := doJSON(ctx, "POST",
				"https://api.firecrawl.dev/v2/scrape",
				map[string]string{"Authorization": "Bearer " + os.Getenv("FIRECRAWL_API_KEY")},
				map[string]any{"url": url, "formats": []string{"markdown"}}, &out)
			if err != nil {
				return "", "", err
			}
			if status >= 400 {
				return "", "", fmt.Errorf("http %d: %.120s", status, raw)
			}
			if !out.Success {
				return "", "", fmt.Errorf("failed: %s", out.Error)
			}
			return out.Data.Markdown, "cloud", nil
		}}
}

func tavilyProvider() Provider {
	return Provider{Name: "tavily-extract", EnvKey: "TAVILY_API_KEY",
		Extract: func(ctx context.Context, url string) (string, string, error) {
			var out struct {
				Results []struct {
					URL        string `json:"url"`
					RawContent string `json:"raw_content"`
				} `json:"results"`
				FailedResults []struct{ Error string } `json:"failed_results"`
			}
			raw, status, err := doJSON(ctx, "POST", "https://api.tavily.com/extract",
				map[string]string{
					"Authorization": "Bearer " + os.Getenv("TAVILY_API_KEY")},
				map[string]any{"urls": []string{url}}, &out)
			if err != nil {
				return "", "", err
			}
			if status >= 400 {
				return "", "", fmt.Errorf("http %d: %.120s", status, raw)
			}
			if len(out.Results) == 0 {
				if len(out.FailedResults) > 0 {
					return "", "", fmt.Errorf("failed: %s", out.FailedResults[0].Error)
				}
				return "", "", fmt.Errorf("no results")
			}
			return out.Results[0].RawContent, "cloud", nil
		}}
}

// ── runner ──────────────────────────────────────────────────────────────────

func runCase(ctx context.Context, p Provider, c Case) Result {
	start := time.Now()
	content, tier, err := p.Extract(ctx, c.URL)
	ms := time.Since(start).Milliseconds()
	r := Result{URL: c.URL, MS: ms, Bytes: len(content), Tier: tier,
		OK: err == nil, ExpectOK: true}
	if err != nil {
		r.OK = false
		r.Error = fmt.Sprintf("%.140s", err)
	}
	if c.Expect != "" {
		r.ExpectOK = strings.Contains(strings.ToLower(content),
			strings.ToLower(c.Expect))
	}
	return r
}

func summarize(rs []Result) (p50, p95, mean int64, okRate, expectRate float64, totBytes int) {
	if len(rs) == 0 {
		return
	}
	var ok, exp int
	var lat []int64
	var sum int64
	for _, r := range rs {
		lat = append(lat, r.MS)
		sum += r.MS
		if r.OK {
			ok++
			totBytes += r.Bytes
		}
		if r.ExpectOK {
			exp++
		}
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	p50 = lat[len(lat)/2]
	p95 = lat[min(len(lat)-1, int(float64(len(lat))*0.95))]
	mean = sum / int64(len(rs))
	okRate = float64(ok) / float64(len(rs))
	expectRate = float64(exp) / float64(len(rs))
	return
}

func loadCorpus(path string) ([]Case, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		c := Case{URL: strings.TrimSpace(parts[0])}
		if len(parts) == 2 {
			c.Expect = strings.TrimSpace(parts[1])
		}
		cases = append(cases, c)
	}
	return cases, nil
}

func main() {
	corpusPath := flag.String("corpus", "bench/corpus.txt", "URL corpus file")
	outPath := flag.String("out", "", "write JSON report here")
	only := flag.String("providers", "", "comma filter (webx,jina-reader,raw-http,firecrawl,tavily-extract)")
	n := flag.Int("n", 1, "repetitions per provider (median taken)")
	warm := flag.Bool("warm", false, "second webx pass to measure cache warmth")
	flag.Parse()

	cases, err := loadCorpus(*corpusPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "corpus:", err)
		os.Exit(1)
	}
	fmt.Printf("corpus: %d URLs from %s\n\n", len(cases), *corpusPath)

	providers := []Provider{
		webxProvider(), rawProvider(), jinaProvider(),
		firecrawlProvider(), tavilyProvider(),
	}
	if *only != "" {
		want := map[string]bool{}
		for _, s := range strings.Split(*only, ",") {
			want[strings.TrimSpace(s)] = true
		}
		var keep []Provider
		for _, p := range providers {
			if want[p.Name] {
				keep = append(keep, p)
			}
		}
		providers = keep
	}

	report := map[string][]Result{}
	type row struct {
		name            string
		p50, p95, mean  int64
		okRate, expRate float64
		bytes           int
		skipped         string
	}
	var rows []row

	for _, p := range providers {
		if !p.Available() {
			rows = append(rows, row{name: p.Name, skipped: p.EnvKey + " unset"})
			fmt.Printf("── %-14s skipped (%s unset)\n", p.Name, p.EnvKey)
			continue
		}
		fmt.Printf("── %-14s\n", p.Name)
		var all []Result
		for _, c := range cases {
			var best Result
			for i := 0; i < *n; i++ {
				r := runCase(context.Background(), p, c)
				if i == 0 || r.MS < best.MS {
					best = r
				}
			}
			mark := "ok"
			if !best.OK {
				mark = "FAIL " + best.Error
			} else if !best.ExpectOK {
				mark = "MISS-content"
			}
			fmt.Printf("   %6dms  %6dB  %s  %s\n", best.MS, best.Bytes, mark, best.URL)
			all = append(all, best)
			time.Sleep(300 * time.Millisecond) // politeness gap
		}
		report[p.Name] = all
		p50, p95, mean, okR, expR, tb := summarize(all)
		rows = append(rows, row{p.Name, p50, p95, mean, okR, expR, tb, ""})
	}

	if *warm {
		fmt.Println("── webx (warm cache)")
		var all []Result
		for _, p := range providers {
			if p.Name != "webx" || !p.Available() {
				continue
			}
			for _, c := range cases {
				r := runCase(context.Background(), p, c)
				all = append(all, r)
				fmt.Printf("   %6dms  %s\n", r.MS, r.URL)
			}
			report["webx-warm"] = all
			p50, p95, mean, okR, expR, tb := summarize(all)
			rows = append(rows, row{"webx (warm)", p50, p95, mean, okR, expR, tb, ""})
		}
	}

	fmt.Println("\n| provider | p50 | p95 | mean | success | content-check | md bytes |")
	fmt.Println("|---|---|---|---|---|---|---|")
	for _, r := range rows {
		if r.skipped != "" {
			fmt.Printf("| %s | — | — | — | — | — | %s |\n", r.name, r.skipped)
			continue
		}
		fmt.Printf("| %s | %dms | %dms | %dms | %.0f%% | %.0f%% | %d |\n",
			r.name, r.p50, r.p95, r.mean, r.okRate*100, r.expRate*100, r.bytes)
	}

	if *outPath != "" {
		f, _ := os.Create(*outPath)
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"generated": time.Now().UTC().Format(time.RFC3339),
			"corpus":    *corpusPath,
			"results":   report,
		})
		f.Close()
		fmt.Println("\nwrote", *outPath)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

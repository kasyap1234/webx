package serve

// reader.go — Jina r.jina.ai-style reader endpoint: GET /r/<url> returns
// the page as markdown over plain HTTP. The whole path after /r/ (plus any
// ?query) IS the target URL — no JSON body, no flags, curl-friendly.
//
// Options ride headers so the URL shape stays pure (Jina's convention):
//
//	X-Engine: chrome|light   render engine for JS-heavy pages
//	X-Render: 1|true         force the render tier
//	X-Token-Budget: N        cap output at ~N tokens
//	X-Target-Selector: a,b   keep only matching elements
//	X-Remove-Selector: a,b   drop matching elements
//	X-Wait-Ms: N             wait N ms after render — delayed JS injects
//
// Accept: application/json returns the full Document JSON instead of text.
//
// Note the Go ServeMux wrinkle: it canonicalizes // inside paths, so
// /r/https://x redirects to /r/https:/x — the handler repairs the scheme.

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/kasyap1234/webx/web/fetch"
)

var schemeRe = regexp.MustCompile(`^(https?):/`)

func (s *Server) reader(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.PathValue("url"))
	if target == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "usage: GET /r/<url> — markdown for any page")
		fmt.Fprintln(w, "  curl http://<host>/r/https://example.com")
		fmt.Fprintln(w, "headers: X-Engine, X-Render, X-Token-Budget, X-Target-Selector, X-Remove-Selector, X-Wait-Ms")
		return
	}
	// ServeMux's clean-path redirect collapses https:// → https:/; rebuild
	// the scheme from the capture group either way.
	if m := schemeRe.FindStringSubmatch(target); m != nil {
		rest := strings.TrimPrefix(target, m[0])
		target = m[1] + "://" + strings.TrimPrefix(rest, "/")
	} else {
		target = "https://" + target
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	if err := s.guard.CheckURL(target); err != nil {
		http.Error(w, "target refused: "+err.Error(), http.StatusForbidden)
		return
	}

	maxChars := 0
	if n, err := strconv.Atoi(r.Header.Get("X-Token-Budget")); err == nil && n > 0 {
		maxChars = n * 4
	}

	freq := fetch.FetchRequest{
		URL:        target,
		AutoRender: true, // the reader should just work — escalate only on detection
		Render:     truthyHeader(r, "X-Render"),
	}
	if eng := r.Header.Get("X-Engine"); eng != "" {
		freq.RenderEngine = eng
		freq.Render = true
	}
	if sels := splitCSV(r.Header.Get("X-Target-Selector")); len(sels) > 0 {
		freq.IncludeSelectors = sels
	}
	if sels := splitCSV(r.Header.Get("X-Remove-Selector")); len(sels) > 0 {
		freq.ExcludeSelectors = sels
	}
	if n, err := strconv.Atoi(r.Header.Get("X-Wait-Ms")); err == nil && n > 0 {
		freq.WaitMs = n // delayed-JS sites: sleep before extraction
	}

	doc, err := fetchFn(r.Context(), freq)
	if err != nil {
		writeFetchErr(w, err)
		return
	}
	if maxChars > 0 && len(doc.Markdown) > maxChars {
		doc.Markdown = doc.Markdown[:maxChars]
		doc.Truncated = true
	}

	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, map[string]any{"success": true, "data": doc})
		return
	}

	// Jina's response shape — Title/URL Source/Published Time header block
	// then the body — so r.jina.ai users can switch with a host change.
	var b strings.Builder
	if doc.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n\n", doc.Title)
	}
	fmt.Fprintf(&b, "URL Source: %s\n\n", doc.FinalURL)
	if doc.Published != "" {
		fmt.Fprintf(&b, "Published Time: %s\n\n", doc.Published)
	}
	if doc.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n\n", doc.Summary)
	}
	if doc.NeedsRender {
		fmt.Fprintln(&b, "Warning: page looked JS-required; no render engine available — content may be thin")
		b.WriteString("\n")
	}
	b.WriteString("Markdown Content:\n")
	b.WriteString(doc.Markdown)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, b.String())
}

func truthyHeader(r *http.Request, name string) bool {
	switch strings.ToLower(r.Header.Get(name)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

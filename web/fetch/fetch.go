package fetch

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	trafilatura "github.com/markusmobius/go-trafilatura"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"github.com/kasyap1234/webx/web/render"
)

// FetchRequest configures a single page fetch.
type FetchRequest struct {
	URL              string
	Raw              bool          // skip extraction, convert the full page
	Timeout          time.Duration // 0 -> defaultTimeout
	UserAgent        string        // "" -> defaultUserAgent
	Browser          bool          // Chrome TLS fingerprint (uTLS) — passes first-line bot checks
	Session          string        // persistent cookie jar name under ~/.webx/sessions/
	Render           bool          // always render via Chrome/remote (JS pages)
	AutoRender       bool          // escalate to render only when JS-required is detected
	WaitFor          string        // CSS selector to wait for when rendering
	WaitMs           int           // ms to wait after load (delayed JS injects); implies render
	Scrolls          int           // scroll passes for infinite/virtual pages when rendering
	Actions          string        // render action spec: "click:.a | wait:.q | screenshot"
	Screenshot       bool          // capture an image while rendering (ScreenshotB64 out)
	ShotFullPage     *bool         // nil -> full-page; &false -> viewport only
	ShotQuality      int           // 1..100 -> JPEG at that quality (0 -> PNG)
	ViewportW        int           // explicit viewport width for render (0 -> default)
	ViewportH        int           // explicit viewport height for render
	Proxy            string        // http://proxy for both HTTP fetch and Chrome
	Stealth          bool          // patch webdriver/plugins/chrome fingerprints when rendering
	Profile          string        // persistent Chrome profile (~/.webx/profiles/<name>)
	BlockTrackers    bool          // drop tracker/ad requests when rendering (avoid_ads)
	BlockMedia       bool          // drop image/font/media/css when rendering (text_mode)
	Mobile           bool          // emulate phone viewport + UA when rendering
	Locale           string        // e.g. "fr-FR" — Accept-Language + navigator.language
	Timezone         string        // e.g. "Europe/Paris" — CDP timezone override
	NetworkCapture   bool          // record page XHR traffic → Document.Network
	ConsoleCapture   bool          // record console.* → Document.Console
	WantPDF          bool          // page→PDF capture when rendering (PDFB64 out)
	WantMHTML        bool          // single-file archive capture when rendering
	SkipTLS          bool          // ignore certificate errors (fetch + render)
	PageSession      string        // named live tab — persists across renders (multi-step flows)
	PierceDOM        bool          // flatten shadow roots + same-origin iframes when rendering
	ScrollSelector   string        // scroll this element instead of window (virtual lists)
	ScrollBy         float64       // px per scroll pass (0 -> viewport)
	Fit              string        // BM25-filter the markdown to this query ("fit markdown")
	IfModifiedSince  time.Time     // conditional GET — 304 → Document{NotModified:true}
	IncludeSelectors []string      // keep only matching elements before extraction
	ExcludeSelectors []string      // drop matching elements before extraction
	WantHTML         bool          // populate Document.HTML with the page source
	WantLinks        bool          // populate Document.Links from the markdown
	WantImages       bool          // populate Document.Images from the markdown
	LinkSummary      bool          // append aggregated ## Links/## Images refs to the markdown (Jina-style)
	WantBrand        bool          // site identity (logo/theme-color/icons) → Document.Brand
	Summary          bool          // LLM-summarize the page (WEBX_LLM_* — best effort)
	WantChunks       bool          // RAG-ready heading-path chunks → Document.Chunks
	WantAgentReady   bool          // host agent-friendliness probe → Document.AgentReady
	WantA11y         bool          // accessibility snapshot (implies render) → Document.A11y
	RenderEngine     string        // "light" → Lightpanda over CDP (default chrome)
	CookieFile       string        // Netscape cookies.txt to load (browser export → SSO'd docs)
	CookieText       string        // inline Netscape-format cookies (remote API — content, not a path)
	RetryAfter       bool          // honor Retry-After once on 429/503 (bounded by maxRetryWait)
	Transcript       bool          // YouTube-only fast path: InnerTube captions → Document.Transcript/Markdown
	TranscriptLang   string        // preferred caption language ("en", "es", …)
	RedactPII        bool          // mask emails/phones/SSN-ish strings in the output
}

// Document is the extracted result of fetching a page.
type Document struct {
	URL           string              `json:"url"`
	FinalURL      string              `json:"final_url"`
	StatusCode    int                 `json:"status_code"`
	Title         string              `json:"title,omitempty"`
	Byline        string              `json:"byline,omitempty"`
	Excerpt       string              `json:"excerpt,omitempty"`
	SiteName      string              `json:"site_name,omitempty"`
	Language      string              `json:"language,omitempty"`
	Published     string              `json:"published,omitempty"`
	Extracted     bool                `json:"extracted"`              // main content was isolated
	Extractor     string              `json:"extractor,omitempty"`    // readability|trafilatura|raw
	LLMSTxt       bool                `json:"llms_txt,omitempty"`     // host publishes /llms.txt
	TierUsed      string              `json:"tier_used"`              // http|tls|chrome|cdp|lightpanda — which engine produced this
	NeedsRender   bool                `json:"needs_render,omitempty"` // page looks JS-required; retry with --render
	TextLength    int                 `json:"text_length"`
	Markdown      string              `json:"markdown"`
	Truncated     bool                `json:"truncated,omitempty"`
	ScreenshotB64 string              `json:"screenshot_b64,omitempty"` // PNG, when rendered+requested
	FitMarkdown   string              `json:"fit_markdown,omitempty"`   // BM25-filtered content for --fit queries
	NotModified   bool                `json:"not_modified,omitempty"`   // 304 on a conditional request
	HTML          string              `json:"html,omitempty"`           // page source when requested (WantHTML)
	Links         []string            `json:"links,omitempty"`          // absolute links on the page (WantLinks)
	Images        []string            `json:"images,omitempty"`         // absolute image URLs + alt text (WantImages)
	JSONLD        []json.RawMessage   `json:"jsonld,omitempty"`         // schema.org entities found in ld+json
	Summary       string              `json:"summary,omitempty"`        // LLM page summary (best effort)
	Warnings      []string            `json:"warnings,omitempty"`       // content anomalies (prompt-injection signals etc.)
	PDFB64        string              `json:"pdf_b64,omitempty"`        // page→PDF when rendered+requested
	MHTML         string              `json:"mhtml,omitempty"`          // single-file archive when rendered+requested
	Network       []render.NetRequest `json:"network,omitempty"`        // page XHR/fetch traffic (render)
	Brand         *Brand              `json:"branding,omitempty"`       // site identity (logo/colors/icons)
	Console       []string            `json:"console,omitempty"`        // console.* messages (render)
	ActionReturns []any               `json:"action_returns,omitempty"` // eval action return values
	Chunks        []Chunk             `json:"chunks,omitempty"`         // RAG-ready segments (WantChunks)
	EstTokens     int                 `json:"est_tokens,omitempty"`     // ~chars/4 of markdown
	AgentReady    *AgentReady         `json:"agent_ready,omitempty"`    // host agent-friendliness probe
	A11y          string              `json:"a11y,omitempty"`           // accessibility tree snapshot (render)
	Transcript    *VideoTranscript    `json:"transcript,omitempty"`     // YouTube captions (Transcript req)
	DurationMs    int64               `json:"duration_ms,omitempty"`    // wall time — honest perf for agents budgeting
}

const maxBodyBytes = 10 << 20 // 10MiB

const (
	defaultTimeout   = 30 * time.Second
	defaultUserAgent = "webx/0.1 (+https://github.com/kasyap1234/webx)"
)

// Fetch retrieves a URL and returns clean markdown plus metadata.
func Fetch(ctx context.Context, freq FetchRequest) (*Document, error) {
	started := time.Now()
	fetchURL, rawCode := rewriteSiteURL(freq.URL)

	// Transcript is a YouTube-only shortcut — captions come from the
	// InnerTube API, not the watch page, so the whole fetch pipeline
	// is skipped. A non-YouTube URL is an honest error, not a fallback.
	if freq.Transcript {
		vid := YouTubeID(fetchURL)
		if vid == "" {
			return nil, fmt.Errorf("transcript: %s is not a YouTube video URL", freq.URL)
		}
		hc := Client(freq.Browser, freq.Session, freq.Timeout, freq.Proxy)
		tr, err := YouTubeTranscript(ctx, vid, freq.TranscriptLang, hc)
		if err != nil {
			return nil, err
		}
		doc := &Document{
			URL: freq.URL, FinalURL: fetchURL, StatusCode: 200,
			Title: tr.Title, Markdown: tr.Text, TextLength: len(tr.Text),
			Transcript: tr, TierUsed: "transcript",
			DurationMs: time.Since(started).Milliseconds(),
		}
		if freq.RedactPII {
			doc.Redact()
		}
		return doc, nil
	}

	// a11y snapshots only exist in a browser — honor the documented
	// "implies render" instead of silently dropping the request.
	if freq.WantA11y {
		freq.Render = true
	}
	// A numeric wait implies render too: it becomes a leading sleep action
	// so delayed JS injection lands before extraction (FC waitFor parity).
	if freq.WaitMs > 0 {
		if freq.Actions != "" {
			freq.Actions = " | " + freq.Actions
		}
		freq.Actions = "sleep:" + strconv.Itoa(freq.WaitMs) + freq.Actions
		freq.Render = true
	}

	// Session-only request: act on an already-open named tab, no HTTP fetch.
	if fetchURL == "" && freq.PageSession != "" && freq.Render {
		rdoc, rerr := render.Render(ctx, req2render(freq, nil))
		if rerr != nil {
			return nil, fmt.Errorf("page session %q: %w", freq.PageSession, rerr)
		}
		doc := docFromRendered(&Document{URL: freq.URL}, rdoc, freq)
		if freq.RedactPII {
			doc.Redact()
		}
		return doc, nil
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", freq.URL, err)
	}
	ua := freq.UserAgent
	if ua == "" {
		ua = defaultUserAgent
		if freq.Browser {
			// Chrome fingerprint + bot UA is self-defeating — match the wire.
			ua = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
		}
	}
	httpReq.Header.Set("User-Agent", ua)
	// Prefer native markdown — Cloudflare/Vercel edge zones serve
	// LLM-ready text on negotiation (80%+ token savings); HTML fallback
	// keeps universal coverage.
	httpReq.Header.Set("Accept", "text/markdown;q=1.0, text/html;q=0.9, application/xhtml+xml;q=0.8, */*;q=0.5")
	if !freq.IfModifiedSince.IsZero() {
		httpReq.Header.Set("If-Modified-Since", freq.IfModifiedSince.UTC().Format(http.TimeFormat))
	}
	signBotRequest(httpReq) // Web Bot Auth — honest crypto identity when configured

	// Imported cookies ride the request directly — a browser-exported
	// cookies.txt carries an SSO session without a persistent jar.
	if freq.CookieFile != "" || freq.CookieText != "" {
		var loaded []*http.Cookie
		var cerr error
		if freq.CookieText != "" {
			loaded, cerr = ParseCookies(freq.CookieText, "request")
		} else {
			loaded, cerr = LoadCookieFile(freq.CookieFile)
		}
		if cerr != nil {
			return nil, ferr(CodeInvalidURL, "cookies", cerr)
		}
		for _, c := range cookiesFor(loaded, httpReq.URL) {
			httpReq.AddCookie(c)
		}
	}

	timeout := freq.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	client := ClientTLS(freq.Browser, freq.Session, timeout, freq.Proxy, freq.SkipTLS)
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, ferr(CodeUpStream, fmt.Sprintf("fetch %s", freq.URL), err)
	}
	defer resp.Body.Close()

	// One polite retry when the server names a wait (429/503 + Retry-After).
	// Capped at maxRetryWait — beyond that, rate_limited says it plainly.
	if freq.RetryAfter && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) {
		ra := retryAfterHint(resp.Header.Get("Retry-After"))
		if ra > 0 && ra <= maxRetryWait {
			resp.Body.Close()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(ra):
			}
			if resp, err = client.Do(httpReq.Clone(ctx)); err != nil {
				return nil, ferr(CodeUpStream, fmt.Sprintf("fetch %s (retry)", freq.URL), err)
			}
		} else if ra > maxRetryWait {
			resp.Body.Close()
			return nil, &FetchError{Code: CodeRateLimited,
				Message: fmt.Sprintf("fetch %s: rate limited — server wants %s wait (>%s cap)", freq.URL, ra, maxRetryWait)}
		}
	}

	if resp.StatusCode == http.StatusNotModified {
		return &Document{URL: freq.URL, FinalURL: fetchURL, StatusCode: 304, NotModified: true, TierUsed: "http"}, nil
	}
	if resp.StatusCode == http.StatusPaymentRequired {
		// x402/AP2 payment gates are real now (Cloudflare pay-per-crawl) —
		// report the ask honestly so an agent's wallet layer can decide.
		return nil, &FetchError{Code: CodePaymentRequired,
			Message: fmt.Sprintf("fetch %s: payment required", freq.URL),
			Payment: paymentGate(resp.Header)}
	}
	if resp.StatusCode != http.StatusOK {
		code := CodeHTTPStatus
		if resp.StatusCode == http.StatusTooManyRequests {
			code = CodeRateLimited
		}
		// Bot-wall statuses (403 Forbidden, 429, 503 challenge pages) are
		// exactly what --auto-render exists for: a real Chrome often walks
		// through where a plain HTTP client gets bounced.
		if freq.AutoRender &&
			(resp.StatusCode == http.StatusForbidden ||
				resp.StatusCode == http.StatusTooManyRequests ||
				resp.StatusCode == http.StatusServiceUnavailable) {
			var jar *sessionJar
			if freq.Session != "" {
				jar = openSessionJar(freq.Session)
			}
			if rdoc, rerr := renderPage(ctx, req2render(freq, jar)); rerr == nil && len(rdoc.HTML) > 0 {
				out := docFromRendered(&Document{URL: freq.URL}, rdoc, freq)
				out.TierUsed = "chrome"
				if rdoc.Engine != "" {
					out.TierUsed = rdoc.Engine
				}
				out.Warnings = append(out.Warnings,
					fmt.Sprintf("http tier got %d; %s render recovered", resp.StatusCode, out.TierUsed))
				out.DurationMs = time.Since(started).Milliseconds()
				return out, nil
			}
		}
		return nil, &FetchError{Code: code, Message: fmt.Sprintf("fetch %s: status %d", freq.URL, resp.StatusCode)}
	}

	contentType := resp.Header.Get("Content-Type")
	generic := isGenericType(contentType)
	if !rawCode && !generic && !isHTML(contentType) && !isPDF(contentType, fetchURL) && !isMarkdown(contentType) && !isTextPlain(contentType) {
		return nil, &FetchError{Code: CodeUnsupportedType,
			Message: fmt.Sprintf("fetch %s: unsupported content type %q", freq.URL, contentType)}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: read body: %w", freq.URL, err)
	}
	if int64(len(body)) > maxBodyBytes {
		return nil, fmt.Errorf("fetch %s: body exceeds %d bytes", freq.URL, maxBodyBytes)
	}

	finalURL := resp.Request.URL

	// Misconfigured origins ship HTML with octet-stream / no type — a real
	// webx failure mode. Sniff the body; only give up when it's not a
	// content type we can extract anything from.
	var sniffedFrom string
	var didSniff bool
	if generic && !rawCode {
		sniffed := sniffContentType(body)
		if sniffed == "" {
			return nil, &FetchError{Code: CodeUnsupportedType,
				Message: fmt.Sprintf("fetch %s: unsupported content type %q", freq.URL, contentType)}
		}
		sniffedFrom, contentType, didSniff = contentType, sniffed, true
	}

	// PDFs skip the whole HTML cascade — extract text directly.
	if isPDF(contentType, finalURL.String()) {
		text, perr := pdfText(body)
		if perr != nil {
			return nil, fmt.Errorf("fetch %s: pdf: %w", freq.URL, perr)
		}
		return &Document{
			URL: freq.URL, FinalURL: finalURL.String(), StatusCode: resp.StatusCode,
			Title: path.Base(finalURL.Path), Extractor: "pdf", TierUsed: "http",
			Markdown: text, TextLength: len(text), Extracted: true,
			DurationMs: time.Since(started).Milliseconds(),
		}, nil
	}

	decoded, err := decodeCharset(body, contentType)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: decode charset: %w", freq.URL, err)
	}

	doc := &Document{
		URL:        freq.URL,
		FinalURL:   finalURL.String(),
		StatusCode: resp.StatusCode,
	}
	if didSniff {
		doc.Warnings = append(doc.Warnings,
			fmt.Sprintf("origin served %q — body sniffed as %s", sniffedFrom, contentType))
	}
	doc.LLMSTxt = hasLLMSTxt(ctx, finalURL)

	if rawCode {
		// github blob -> raw file: wrap as a fenced code block
		doc.Title = path.Base(finalURL.Path)
		doc.Extractor = "raw"
		doc.Markdown = "```" + langHint(finalURL.Path) + "\n" + string(decoded) + "\n```"
		doc.TextLength = len(doc.Markdown)
		doc.TierUsed = "http"
		return doc, nil
	}

	doc.TierUsed = "http"
	if freq.Browser {
		doc.TierUsed = "tls"
	}
	var jar *sessionJar
	if freq.Session != "" {
		jar = openSessionJar(freq.Session)
	}
	switch {
	case isMarkdown(contentType) || isTextPlain(contentType):
		// Edge markdown / plain text — the origin volunteered ready text:
		// text/markdown via content negotiation (Cloudflare "Markdown for
		// Agents"), or text/plain (Vercel serves negotiated markdown that
		// way — react.dev — and .txt files are already content). Skip the
		// whole extract cascade; the server did our job.
		doc.Extracted = true
		doc.Markdown = strings.TrimSpace(string(decoded))
		doc.Title = markdownTitle(doc.Markdown)
		doc.TextLength = len(doc.Markdown)
		if isMarkdown(contentType) {
			doc.Extractor = "edge-markdown"
			doc.AgentReady = &AgentReady{MarkdownNative: true}
		} else {
			doc.Extractor = "text-plain"
		}
	case freq.Render:
		if rendered, rerr := renderPage(ctx, req2render(freq, jar)); rerr == nil {
			if rdoc := docFromRendered(doc, rendered, freq); len(rdoc.Markdown) > len(doc.Markdown) {
				doc = rdoc
			}
			doc.TierUsed = "chrome"
			if rendered.Engine != "" {
				doc.TierUsed = rendered.Engine
			}
		} else {
			return nil, fmt.Errorf("fetch %s: render: %w", freq.URL, rerr)
		}
	default:
		scoped, serr := applySelectors(decoded, freq.IncludeSelectors, freq.ExcludeSelectors)
		if serr != nil {
			return nil, fmt.Errorf("fetch %s: %w", freq.URL, serr)
		}
		decoded = scoped
		domain := finalURL.Scheme + "://" + finalURL.Host
		if _, eerr := extractFromHTML(doc, decoded, finalURL, domain, freq.Raw, freq.WantBrand); eerr != nil {
			return nil, eerr
		}
		if freq.AutoRender && NeedsRender(decoded, len(doc.Markdown)) {
			if rendered, rerr := renderPage(ctx, req2render(freq, jar)); rerr == nil {
				rdoc := docFromRendered(doc, rendered, freq)
				if len(rdoc.Markdown) > len(doc.Markdown) {
					doc = rdoc
					doc.TierUsed = "chrome"
					if rendered.Engine != "" {
						doc.TierUsed = rendered.Engine
					}
				} else {
					doc.NeedsRender = true
				}
			} else {
				doc.NeedsRender = true
			}
		} else {
			doc.NeedsRender = NeedsRender(decoded, len(doc.Markdown))
		}
	}
	if freq.Fit != "" && doc.Markdown != "" {
		doc.FitMarkdown = FitMarkdown(doc.Markdown, freq.Fit)
	}
	if freq.WantLinks || freq.LinkSummary {
		doc.Links = extractLinks(doc.Markdown, doc.FinalURL)
	}
	if freq.WantImages || freq.LinkSummary {
		doc.Images = extractImages(doc.Markdown)
	}
	if freq.LinkSummary {
		doc.Markdown = RefSummary(doc.Markdown, doc.Links, doc.Images)
	}
	if freq.WantHTML {
		doc.HTML = string(decoded)
	}
	if freq.Summary && doc.Markdown != "" {
		doc.Summary, _ = Summarize(ctx, doc.Markdown) // best effort
	}
	doc.EstTokens = estTokens(doc.Markdown)
	if freq.WantChunks {
		doc.Chunks = ChunkMarkdown(doc.Markdown)
	}
	if freq.WantAgentReady {
		probeAgentReady(ctx, finalURL, doc, resp.Header)
	}
	if doc.TierUsed == "http" || doc.TierUsed == "tls" {
		if ig := ignoredRenderOpts(freq); len(ig) > 0 {
			doc.Warnings = append(doc.Warnings,
				"render options ignored — no browser ran (set render/auto_render): "+strings.Join(ig, ", "))
		}
	}
	if freq.RedactPII {
		doc.Redact()
	}
	doc.DurationMs = time.Since(started).Milliseconds()
	return doc, nil
}

// ignoredRenderOpts lists the render-path options the caller set that
// never ran because no browser was involved — silent ignores are the
// worst kind of wrong answer for an agent.
func ignoredRenderOpts(freq FetchRequest) []string {
	var ig []string
	flags := []struct {
		name string
		set  bool
	}{
		{"wait_for", freq.WaitFor != ""},
		{"actions", freq.Actions != ""},
		{"scrolls", freq.Scrolls > 0},
		{"screenshot", freq.Screenshot},
		{"stealth", freq.Stealth},
		{"profile", freq.Profile != ""},
		{"block_ads", freq.BlockTrackers},
		{"text_mode", freq.BlockMedia},
		{"mobile", freq.Mobile},
		{"locale", freq.Locale != ""},
		{"timezone", freq.Timezone != ""},
		{"capture_network", freq.NetworkCapture},
		{"capture_console", freq.ConsoleCapture},
		{"pdf", freq.WantPDF},
		{"mhtml", freq.WantMHTML},
		{"page_session", freq.PageSession != ""},
		{"pierce_dom", freq.PierceDOM},
		{"scroll_selector", freq.ScrollSelector != ""},
		{"a11y", freq.WantA11y},
		{"engine", freq.RenderEngine != ""},
	}
	for _, f := range flags {
		if f.set {
			ig = append(ig, f.name)
		}
	}
	return ig
}

// req2render maps a FetchRequest onto a render.Request, pulling session
// cookies so a warmed jar (cf_clearance, login) rides into the browser.
func req2render(freq FetchRequest, jar *sessionJar) render.Request {
	req := render.Request{
		URL:            freq.URL,
		WaitFor:        freq.WaitFor,
		Scrolls:        freq.Scrolls,
		Timeout:        freq.Timeout,
		Proxy:          freq.Proxy,
		Screenshot:     freq.Screenshot,
		ShotFullPage:   freq.ShotFullPage,
		ShotQuality:    freq.ShotQuality,
		ViewportW:      freq.ViewportW,
		ViewportH:      freq.ViewportH,
		Stealth:        freq.Stealth,
		Profile:        freq.Profile,
		BlockTrackers:  freq.BlockTrackers,
		BlockMedia:     freq.BlockMedia,
		Mobile:         freq.Mobile,
		Locale:         freq.Locale,
		Timezone:       freq.Timezone,
		NetworkCapture: freq.NetworkCapture,
		ConsoleCapture: freq.ConsoleCapture,
		PDF:            freq.WantPDF,
		MHTML:          freq.WantMHTML,
		SkipTLS:        freq.SkipTLS,
		PageSession:    freq.PageSession,
		PierceDOM:      freq.PierceDOM,
		ScrollSelector: freq.ScrollSelector,
		ScrollBy:       freq.ScrollBy,
		WantA11y:       freq.WantA11y,
		WantAgentTools: freq.WantAgentReady, // WebMCP probe rides agent_ready
		Engine:         freq.RenderEngine,
		ResolveAct:     llmActResolver,
	}
	if freq.Actions != "" {
		req.Actions, _ = render.ParseActions(freq.Actions) // validated by caller
	}
	if jar != nil {
		if u, err := url.Parse(freq.URL); err == nil {
			req.Cookies = jar.Cookies(u)
		}
	}
	if freq.CookieFile != "" || freq.CookieText != "" {
		var loaded []*http.Cookie
		var err error
		if freq.CookieText != "" {
			loaded, err = ParseCookies(freq.CookieText, "request")
		} else {
			loaded, err = LoadCookieFile(freq.CookieFile)
		}
		if err == nil {
			if u, uerr := url.Parse(freq.URL); uerr == nil {
				req.Cookies = append(req.Cookies, cookiesFor(loaded, u)...)
			}
		}
	}
	return req
}

// renderPage is a var so tests can stub the browser — auto-render
// escalation logic is verifiable without Chrome.
var renderPage = func(ctx context.Context, req render.Request) (*render.Result, error) {
	if req.Timeout <= 0 {
		req.Timeout = defaultTimeout
	}
	return render.Render(ctx, req)
}

var actIndexRe = regexp.MustCompile(`\d+`)

// llmActResolver answers "which element index?" for act: actions — the
// model only sees the numbered candidate list, never raw HTML.
func llmActResolver(ctx context.Context, candidates, instruction string) (int, error) {
	if llmBase() == "" {
		return 0, fmt.Errorf("act needs an LLM — set WEBX_LLM_BASE (+WEBX_LLM_KEY)")
	}
	out, err := LLMChat(ctx,
		"You resolve browser instructions to element indices. Reply with ONLY the integer index of the best matching element, nothing else.",
		"Interactive elements (idx tag role \"text\"):\n"+candidates+"\n\nInstruction: "+instruction,
		false)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(actIndexRe.FindString(strings.TrimSpace(out)))
	if err != nil {
		return 0, fmt.Errorf("act: LLM returned %q — not an index", out)
	}
	return n, nil
}

// docFromRendered re-runs the extraction cascade on rendered HTML and
// bridges harvested cookies back into the session jar.
func docFromRendered(prev *Document, res *render.Result, freq FetchRequest) *Document {
	finalURL, err := url.Parse(res.FinalURL)
	if err != nil || finalURL == nil {
		finalURL, _ = url.Parse(res.URL)
	}
	decoded, err := decodeCharset(res.HTML, "text/html")
	if err != nil {
		return prev
	}
	if scoped, serr := applySelectors(decoded, freq.IncludeSelectors, freq.ExcludeSelectors); serr == nil {
		decoded = scoped
	}
	doc := &Document{URL: prev.URL, FinalURL: res.FinalURL, StatusCode: prev.StatusCode}
	doc.LLMSTxt = prev.LLMSTxt
	if len(res.Screenshot) > 0 {
		doc.ScreenshotB64 = base64.StdEncoding.EncodeToString(res.Screenshot)
	}
	if len(res.PDF) > 0 {
		doc.PDFB64 = base64.StdEncoding.EncodeToString(res.PDF)
	}
	if len(res.MHTML) > 0 {
		doc.MHTML = string(res.MHTML)
	}
	doc.Network = res.NetworkRequests
	doc.Console = res.Console
	doc.ActionReturns = res.ActionReturns
	doc.A11y = res.A11y
	doc.AgentReady = prev.AgentReady
	if res.WebMCP || len(res.AgentTools) > 0 {
		if doc.AgentReady == nil {
			doc.AgentReady = &AgentReady{}
		}
		doc.AgentReady.WebMCP = res.WebMCP
		doc.AgentReady.AgentTools = res.AgentTools
	}
	if freq.WantHTML {
		doc.HTML = string(res.HTML)
	}
	out, err := extractFromHTML(doc, decoded, finalURL, finalURL.Scheme+"://"+finalURL.Host, freq.Raw, freq.WantBrand)
	if err != nil {
		return prev
	}
	out.Warnings = append(out.Warnings, res.Warnings...)
	if freq.Session != "" {
		if u, uerr := url.Parse(res.FinalURL); uerr == nil {
			openSessionJar(freq.Session).SetCookies(u, render.HarvestCookies(res))
		}
	}
	return out
}

// extractFromHTML runs the readability→trafilatura→full-page cascade on an
// already-downloaded, charset-decoded HTML body — shared by Fetch and the
// Common Crawl bootstrap path which hands us WARC bodies, not live URLs.
func extractFromHTML(doc *Document, decoded []byte, finalURL *url.URL, domain string, raw bool, wantBrand bool) (*Document, error) {
	if !raw {
		// Pass 1: readability — best fidelity on articles.
		if article, aerr := readability.FromReader(bytes.NewReader(decoded), finalURL); aerr == nil && article.Node != nil {
			if md, cerr := htmltomarkdown.ConvertNode(article.Node, converter.WithDomain(domain)); cerr == nil && strings.TrimSpace(string(md)) != "" {
				doc.Markdown = string(md)
				doc.Extracted = true
				doc.Extractor = "readability"
			}
			doc.Title = article.Title()
			doc.Byline = article.Byline()
			doc.Excerpt = article.Excerpt()
			doc.SiteName = article.SiteName()
			doc.Language = article.Language()
		}
		// Pass 2: trafilatura — broader coverage on docs/technical pages,
		// EnableFallback runs readability+dom-distiller internally too.
		// Trigger when readability returned little OR returned low-quality
		// boilerplate (link/nav-heavy markdown reads long but isn't content).
		if len(strings.TrimSpace(doc.Markdown)) < 500 || mdQuality(doc.Markdown) < 0.45 {
			if tr, terr := trafilatura.Extract(bytes.NewReader(decoded), trafilatura.Options{
				OriginalURL:     finalURL,
				EnableFallback:  true,
				ExcludeComments: true,
			}); terr == nil && tr.ContentNode != nil {
				if md, cerr := htmltomarkdown.ConvertNode(tr.ContentNode, converter.WithDomain(domain)); cerr == nil && len(strings.TrimSpace(string(md))) > len(strings.TrimSpace(doc.Markdown)) {
					doc.Markdown = string(md)
					doc.Extracted = true
					doc.Extractor = "trafilatura"
				}
				if doc.Title == "" {
					doc.Title = tr.Metadata.Title
				}
				if doc.Byline == "" {
					doc.Byline = tr.Metadata.Author
				}
				if doc.Excerpt == "" {
					doc.Excerpt = tr.Metadata.Description
				}
				if doc.SiteName == "" {
					doc.SiteName = tr.Metadata.Sitename
				}
				if doc.Language == "" {
					doc.Language = tr.Metadata.Language
				}
				if doc.Published == "" && !tr.Metadata.Date.IsZero() {
					doc.Published = tr.Metadata.Date.Format("2006-01-02")
				}
			}
		}
	}

	if doc.Markdown == "" {
		md, err := htmltomarkdown.ConvertReader(bytes.NewReader(decoded), converter.WithDomain(domain))
		if err != nil {
			return nil, fmt.Errorf("extract %s: convert to markdown: %w", finalURL, err)
		}
		doc.Markdown = string(md)
	}

	doc.Markdown = stripDataURIs(doc.Markdown)     // base64 images are pure token waste
	doc.Markdown = stripHTMLComments(doc.Markdown) // <!-- --> is template junk, not content
	doc.Markdown = stripLinkClusters(doc.Markdown) // nav/infobox link soup + footnote chrome
	if doc.Title == "" {
		doc.Title = pageTitle(decoded)
	}
	if ents := ExtractLD(decoded); len(ents) > 0 {
		doc.JSONLD = ents
	}
	if wantBrand {
		doc.Brand = ExtractBrand(decoded, finalURL.String())
	}
	doc.Warnings = append(doc.Warnings, DetectInjection(decoded, doc.Markdown)...)
	doc.TextLength = len(doc.Markdown)
	return doc, nil
}

// mdQuality scores extracted markdown 0–1: the fraction of characters that
// are prose rather than link/image syntax or nav debris. Readability leaks
// boilerplate on tag/listing pages — those score low and get a second
// extraction attempt instead of shipping link salad as content.
var mdInlineRe = regexp.MustCompile(`!?\[[^\]]*\]\([^)]*\)|<[^>]+>`)

func mdQuality(md string) float64 {
	var total, prose float64
	for _, ln := range strings.Split(md, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		total += float64(len(ln))
		stripped := strings.TrimSpace(mdInlineRe.ReplaceAllString(ln, ""))
		prose += float64(len(stripped))
	}
	if total == 0 {
		return 0
	}
	return prose / total
}

// ExtractFromBody exposes the extraction cascade for callers that already
// hold an HTML body — Common Crawl WARC records, test fixtures, caches.
func ExtractFromBody(body []byte, contentType, finalURL string) (*Document, error) {
	u, err := url.Parse(finalURL)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeCharset(body, contentType)
	if err != nil {
		return nil, fmt.Errorf("decode charset: %w", err)
	}
	doc := &Document{URL: finalURL, FinalURL: finalURL}
	return extractFromHTML(doc, decoded, u, u.Scheme+"://"+u.Host, false, false)
}

// rewriteSiteURL maps known site URLs to raw content endpoints — GitHub blob
// pages are React UI chrome; the raw file is what agents actually want.
var ghBlobRe = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/blob/(.+)$`)

func rewriteSiteURL(u string) (string, bool) {
	if m := ghBlobRe.FindStringSubmatch(u); m != nil {
		return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", m[1], m[2], m[3]), true
	}
	return u, false
}

func langHint(p string) string {
	switch path.Ext(p) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".mjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".hpp", ".cc":
		return "cpp"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".sh", ".bash":
		return "bash"
	case ".sql":
		return "sql"
	default:
		return ""
	}
}

func isHTML(contentType string) bool {
	if contentType == "" {
		return true
	}
	return strings.Contains(contentType, "text/html") ||
		strings.Contains(contentType, "application/xhtml+xml")
}

// isMarkdown detects origin-served markdown (content-negotiation hits).
func isMarkdown(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "text/markdown") || strings.Contains(ct, "text/x-markdown")
}

// isTextPlain detects plain-text responses — ambiguous but always
// content-extractable: Vercel serves markdown-negotiated pages as
// text/plain (react.dev), and .txt files are already the answer.
func isTextPlain(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(contentType), "text/plain")
}

// isGenericType reports content-types that carry no real type signal —
// missing headers and octet-stream. Only these get body-sniffed; a
// declared-but-wrong type stays an honest unsupported_type error.
func isGenericType(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	return ct == "" ||
		strings.HasPrefix(ct, "application/octet-stream") ||
		strings.HasPrefix(ct, "binary/octet-stream") ||
		strings.HasPrefix(ct, "application/x-unknown") ||
		strings.HasPrefix(ct, "application/download")
}

// sniffContentType maps a body to an extractable type via stdlib
// content-sniffing. "" means nothing we can do anything with.
func sniffContentType(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	ct := http.DetectContentType(body)
	switch {
	case strings.HasPrefix(ct, "text/html"), strings.Contains(ct, "xhtml"):
		return "text/html"
	case strings.HasPrefix(ct, "text/plain"):
		return "text/plain"
	case strings.HasPrefix(ct, "application/pdf"):
		return "application/pdf"
	default:
		return ""
	}
}

// markdownTitle lifts the first ATX heading — edge-markdown pages carry
// no <title>.
func markdownTitle(md string) string {
	for _, line := range strings.SplitN(md, "\n", 20) {
		if t, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok {
			return strings.TrimSpace(t)
		}
	}
	return ""
}

func decodeCharset(body []byte, contentType string) ([]byte, error) {
	r, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func pageTitle(body []byte) string {
	node, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return ""
	}
	var title string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if title != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
			title = strings.TrimSpace(n.FirstChild.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(node)
	return title
}

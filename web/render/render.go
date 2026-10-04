// Package render drives a real browser (Chrome via go-rod/CDP) to render
// JavaScript pages the HTTP fast path can't see — the "chrome" tier behind
// the fetch pipeline's escalation model.
//
// Chrome is discovered via go-rod's launcher (system Chrome, or downloaded
// to ~/.cache/rod on first use). A lighter deployment can delegate to a
// remote render endpoint instead — set WEBX_RENDER_URL to any service that
// accepts {url, wait_ms} POST and returns rendered HTML (including our own
// `webx serve --with-render`).
package render

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// Action is one browser step — the pipe-separated mini-language agents can
// script: "click:.accept | wait:.quote | type:#q=golang | fill:@e3=me@x.com |
// get:@e1 | hover:.menu | press:enter | sleep:400 | scroll |
// eval:window.scrollTo(0,9999) | screenshot". Targets take @eN a11y refs
// (page sessions) or CSS selectors; clicks occlusion-check first.
type Action struct {
	Op   string `json:"op"`   // wait|click|type|fill|get|hover|press|sleep|scroll|eval|act|screenshot
	Arg  string `json:"arg"`  // selector / key / ms / js
	Arg2 string `json:"arg2"` // second operand (type's text)
}

// ParseActions compiles a " | "-separated action spec. Unknown ops error —
// silently skipping a "click" the caller intended would be worse.
// A literal pipe inside an argument escapes as \| (e.g. eval:x==='a|b').
func ParseActions(spec string) ([]Action, error) {
	var out []Action
	for _, part := range splitActions(spec) {
		part = unescapePipe(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		op, arg, _ := strings.Cut(part, ":")
		op = strings.ToLower(strings.TrimSpace(op))
		a := Action{Op: op}
		switch op {
		case "wait", "click", "press", "eval", "act", "get", "hover":
			a.Arg = strings.TrimSpace(arg)
			if a.Arg == "" {
				return nil, fmt.Errorf("action %q needs an argument", op)
			}
		case "type", "fill":
			sel, text, ok := strings.Cut(arg, "=")
			if !ok || strings.TrimSpace(sel) == "" {
				return nil, fmt.Errorf("%s needs \"sel=text\": %q", op, part)
			}
			a.Arg, a.Arg2 = strings.TrimSpace(sel), text
		case "sleep":
			ms, err := strconv.Atoi(strings.TrimSpace(arg))
			if err != nil || ms < 0 {
				return nil, fmt.Errorf("sleep needs ms: %q", arg)
			}
			a.Arg = strconv.Itoa(ms)
		case "scroll", "screenshot":
			// no args
		default:
			return nil, fmt.Errorf("unknown action %q (want wait|click|type|fill|get|hover|press|sleep|scroll|eval|act|screenshot)", op)
		}
		out = append(out, a)
	}
	return out, nil
}

// splitActions splits on unescaped '|' — \| inside an arg (JS in eval:,
// pipes in CSS selectors like [data-x="a|b"]) doesn't end the action.
func splitActions(spec string) []string {
	var parts []string
	var cur strings.Builder
	for i := 0; i < len(spec); i++ {
		if spec[i] == '\\' && i+1 < len(spec) && spec[i+1] == '|' {
			cur.WriteByte('\\')
			cur.WriteByte('|')
			i++
			continue
		}
		if spec[i] == '|' {
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(spec[i])
	}
	parts = append(parts, cur.String())
	return parts
}

func unescapePipe(s string) string {
	return strings.ReplaceAll(s, `\|`, `|`)
}

// Request configures one page render.
type Request struct {
	URL        string
	WaitFor    string        // CSS selector to wait for ("" -> wait for network-idle)
	Scrolls    int           // scroll-to-bottom passes for infinite/virtual pages
	Timeout    time.Duration // total render budget, 0 -> 30s
	Cookies    []*http.Cookie
	Actions    []Action // run after load+wait, before HTML capture
	Proxy      string   // http://user:pass@host:port — Chrome --proxy-server
	Screenshot bool     // capture an image into Result.Screenshot
	// ShotFullPage nil → full-page capture (webx's historic default);
	// &false → viewport-only (Firecrawl screenshot_options.full_page).
	ShotFullPage *bool
	// ShotQuality 1..100 switches the format to JPEG at that quality —
	// ~10× smaller than PNG for agents that only need to *see* the page.
	ShotQuality int
	// ViewportW/H emulate an exact viewport before rendering
	// (screenshot_options.viewport / responsive-layout checks). 0 →
	// default (or Mobile's 390×844 when Mobile is set).
	ViewportW      int
	ViewportH      int
	Stealth        bool    // patch webdriver/plugins/chrome fingerprints
	Profile        string  // persistent profile name under ~/.webx/profiles/ — logins+localStorage survive
	BlockTrackers  bool    // drop known tracker/ad requests (crawl4ai avoid_ads)
	BlockMedia     bool    // drop image/font/media/stylesheet requests (text_mode)
	Mobile         bool    // emulate a phone viewport + UA
	Locale         string  // e.g. "fr-FR" — Accept-Language + navigator.language
	Timezone       string  // e.g. "Europe/Paris" — CDP timezone override
	NetworkCapture bool    // record XHR/fetch traffic into Result.NetworkRequests
	ConsoleCapture bool    // record console.* messages into Result.Console
	PDF            bool    // capture the page as PDF (CDP print)
	MHTML          bool    // capture a single-file archive (Page.captureSnapshot)
	SkipTLS        bool    // ignore certificate errors
	CDPURL         string  // connect to a running Chrome via ws://…/devtools/browser/…
	PageSession    string  // named live tab — persists across renders for multi-step flows
	PierceDOM      bool    // flatten shadow roots + same-origin iframes into HTML
	ScrollSelector string  // scroll this element instead of the window (virtual lists)
	ScrollBy       float64 // px per scroll pass (0 -> viewport height)
	WantA11y       bool    // accessibility snapshot → Result.A11y (+ @eN refs for sessions)
	WantAgentTools bool    // probe navigator.modelContext (WebMCP) → Result.AgentTools
	Engine         string  // "light"/"lightpanda" → Lightpanda over CDP (default chrome)
	// ResolveAct turns a natural-language action into an element index
	// from the act-candidate dump (LLM-backed; nil = act: errors honestly).
	ResolveAct func(ctx context.Context, candidates, instruction string) (int, error)
}

// NetRequest is one observed page network request — the "find the real
// JSON API this SPA calls" output for agents.
type NetRequest struct {
	Method       string `json:"method"`
	URL          string `json:"url"`
	ResourceType string `json:"type,omitempty"`
	Status       int    `json:"status,omitempty"`
	MimeType     string `json:"mime,omitempty"`
}

// Result is the rendered DOM plus harvested cookies.
type Result struct {
	URL             string
	FinalURL        string
	HTML            []byte
	Cookies         []*http.Cookie
	Screenshot      []byte // PNG when requested via action or flag
	PDF             []byte // page→PDF when requested
	MHTML           []byte // single-file archive when requested
	ActionReturns   []any  // return values of eval actions, in order
	NetworkRequests []NetRequest
	Console         []string
	A11y            string   // accessibility tree snapshot (WantA11y)
	WebMCP          bool     // navigator.modelContext detected
	AgentTools      []string // WebMCP tool names when listable
	Engine          string   // backend used — chrome|cdp|lightpanda
	Warnings        []string // engine/fidelity caveats for the caller
}

// Render returns fully-rendered HTML. Order of backends:
//  1. WEBX_RENDER_URL — remote render service (keeps the binary Chrome-free)
//  2. local Chrome via go-rod (system Chrome or ~/.cache/rod download)
func Render(ctx context.Context, req Request) (*Result, error) {
	if base := os.Getenv("WEBX_RENDER_URL"); base != "" {
		return renderRemote(ctx, base, req)
	}
	return renderLocal(ctx, req)
}

// Available reports whether any render backend is configured.
func Available() bool {
	if os.Getenv("WEBX_RENDER_URL") != "" {
		return true
	}
	if os.Getenv("WEBX_CHROME_BIN") != "" {
		return true
	}
	if _, has := launcher.LookPath(); has {
		return true
	}
	if _, err := exec.LookPath("lightpanda"); err == nil {
		return true // engine=light available
	}
	return false // will trigger a browser download on first render
}

// ── local Chrome via go-rod ──────────────────────────────────────────────────

// The browser is pooled per (chrome-bin, proxy) — launching Chrome costs
// ~1-2s per render; a warm browser makes repeat renders dramatically cheaper.
// Leakless reaps Chrome when the process exits, so a pooled browser is safe
// for both the one-shot CLI and the long-lived server.
var (
	poolMu      sync.Mutex
	poolBrowser *rod.Browser
	poolKey     string
)

func getBrowser(req Request) (*rod.Browser, error) {
	poolMu.Lock()
	defer poolMu.Unlock()
	// External Chrome (WEBX_CDP_URL / Request.CDPURL) connects without
	// launching — pairs with a remote browser service or a manually
	// warmed headful Chrome.
	cdp := req.CDPURL
	if cdp == "" {
		cdp = os.Getenv("WEBX_CDP_URL")
	}
	if cdp != "" {
		key := "cdp:" + cdp
		if poolBrowser != nil && poolKey == key {
			if _, err := poolBrowser.Version(); err == nil {
				return poolBrowser, nil
			}
			poolBrowser = nil // external Chrome died — reconnect, don't close it
		}
		browser := rod.New().ControlURL(cdp)
		if err := browser.Connect(); err != nil {
			return nil, fmt.Errorf("cdp connect %s: %w", cdp, err)
		}
		poolBrowser, poolKey = browser, key
		return browser, nil
	}

	// The "light" engine — Lightpanda over CDP (lightpanda.go). An explicit
	// CDP attach above still wins; this only replaces the *launch* path.
	if engineName(req) == "lightpanda" {
		return lightBrowser()
	}

	key := os.Getenv("WEBX_CHROME_BIN") + "|" + req.Proxy + "|" + req.Profile +
		fmt.Sprint(req.Stealth, req.SkipTLS)
	if poolBrowser != nil && poolKey == key {
		if _, err := poolBrowser.Version(); err == nil {
			return poolBrowser, nil
		}
		poolBrowser.MustClose() // dead browser — relaunch below
		poolBrowser = nil
	}
	l := launcher.New().
		Headless(true).
		Leakless(true).
		Set("no-sandbox"). // containers/CI need it; harmless on desktop
		Set("disable-gpu").
		Set("disable-dev-shm-usage")
	if bin := os.Getenv("WEBX_CHROME_BIN"); bin != "" {
		l = l.Bin(bin)
	}
	if req.Proxy != "" {
		l = l.Proxy(req.Proxy)
	}
	if req.Profile != "" {
		// Persistent user-data dir — cookies, localStorage, and login
		// state survive across renders and process restarts.
		dir := filepath.Join(profileRoot(), sanitizeName(req.Profile))
		_ = os.MkdirAll(dir, 0o700)
		l = l.UserDataDir(dir)
	}
	if req.SkipTLS {
		l = l.Set("ignore-certificate-errors")
	}
	if req.Stealth {
		// Headless-markers that fingerprint checks key on.
		l = l.Set("disable-blink-features", "AutomationControlled")
	}
	controlURL, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("chrome launch: %w", err)
	}
	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("chrome connect: %w", err)
	}
	poolBrowser, poolKey = browser, key
	return browser, nil
}

func profileRoot() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".webx", "profiles")
	}
	return ".webx-profiles"
}

var badName = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func sanitizeName(s string) string {
	if s = badName.ReplaceAllString(s, "_"); s == "" {
		return "default"
	}
	return s
}

// CloseBrowser releases the pooled Chrome (server shutdown).
func CloseBrowser() {
	poolMu.Lock()
	defer poolMu.Unlock()
	if poolBrowser != nil {
		poolBrowser.MustClose()
		poolBrowser = nil
	}
}

// pageSessions are named live tabs keyed by (browser key, session name) —
// Steel/Browserbase session_id parity for multi-step agent flows: render
// with --page-session work to keep the tab (login, SPA state) alive, and
// re-render with actions only (URL may be empty) to act on the live page.
type pageSession struct {
	page     *rod.Page
	lastUsed time.Time
	refs     map[string]proto.DOMBackendNodeID // @eN refs from the last a11y snapshot
}

const maxPageSessions = 8

var pageSessions = map[string]*pageSession{}

func sessionPage(browser *rod.Browser, req Request) (*rod.Page, *pageSession, bool, error) {
	poolMu.Lock()
	key := poolKey + "::" + sanitizeName(req.PageSession)
	if s, ok := pageSessions[key]; ok {
		s.lastUsed = time.Now()
		poolMu.Unlock()
		// Health-check the tab — a crashed target must not poison the session.
		if _, err := s.page.Eval("() => 1"); err == nil {
			return s.page, s, true, nil
		}
		poolMu.Lock()
		delete(pageSessions, key)
	}
	// Evict the stalest session when at capacity.
	if len(pageSessions) >= maxPageSessions {
		var oldest string
		var old time.Time
		for k, s := range pageSessions {
			if oldest == "" || s.lastUsed.Before(old) {
				oldest, old = k, s.lastUsed
			}
		}
		if s := pageSessions[oldest]; s != nil {
			go s.page.Close()
			delete(pageSessions, oldest)
		}
	}
	poolMu.Unlock()
	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, nil, false, err
	}
	sess := &pageSession{page: page, lastUsed: time.Now()}
	poolMu.Lock()
	pageSessions[key] = sess
	poolMu.Unlock()
	return page, sess, false, nil
}

// ClosePageSession kills a named live tab (CLI/serve cleanup endpoint).
func ClosePageSession(name string) {
	poolMu.Lock()
	defer poolMu.Unlock()
	key := poolKey + "::" + sanitizeName(name)
	if s, ok := pageSessions[key]; ok {
		go s.page.Close()
		delete(pageSessions, key)
	}
}

func renderLocal(ctx context.Context, req Request) (*Result, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	browser, err := getBrowser(req)
	if err != nil {
		return nil, err
	}
	var page *rod.Page
	var sess *pageSession
	if req.PageSession != "" {
		page, sess, _, err = sessionPage(browser, req)
		if err != nil {
			return nil, err
		}
		// Session pages stay open — the session owns the tab.
	} else {
		page, err = browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
		if err != nil {
			return nil, err
		}
		defer page.Close()
	}

	res := &Result{URL: req.URL, Engine: engineName(req)}
	if res.Engine == "lightpanda" {
		res.Warnings = append(res.Warnings,
			"render_engine=lightpanda — fast+light, but reduced web-platform fidelity; some CDP features may be absent")
	}
	stopCollect := collectTelemetry(page, req, res)
	defer stopCollect()

	if req.Stealth {
		page.MustEvalOnNewDocument(stealthJS)
	}
	if req.Mobile {
		_ = proto.EmulationSetDeviceMetricsOverride{
			Width: 390, Height: 844, DeviceScaleFactor: 3, Mobile: true,
		}.Call(page)
		_ = proto.NetworkSetUserAgentOverride{
			UserAgent: mobileUA,
		}.Call(page)
	}
	// Explicit viewport wins over Mobile — an exact (w,h) request is the
	// caller saying "I know precisely what layout I want".
	if req.ViewportW > 0 && req.ViewportH > 0 {
		_ = proto.EmulationSetDeviceMetricsOverride{
			Width: req.ViewportW, Height: req.ViewportH,
			DeviceScaleFactor: 1, Mobile: req.Mobile,
		}.Call(page)
	}
	if req.Locale != "" {
		_ = proto.EmulationSetLocaleOverride{Locale: req.Locale}.Call(page)
		_, _ = page.SetExtraHeaders([]string{"Accept-Language", req.Locale})
	}
	if req.Timezone != "" {
		_ = proto.EmulationSetTimezoneOverride{TimezoneID: req.Timezone}.Call(page)
	}
	stopHijack := hijackResources(page, req)
	defer stopHijack()

	if len(req.Cookies) > 0 {
		params := make([]*proto.NetworkCookieParam, 0, len(req.Cookies))
		for _, c := range req.Cookies {
			params = append(params, &proto.NetworkCookieParam{
				Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
				Secure: c.Secure, HTTPOnly: c.HttpOnly,
			})
		}
		_ = page.SetCookies(params)
	}
	// Navigate only when a URL is given — a reused session page with URL ""
	// acts on the live tab as-is (multi-step flows).
	if req.URL != "" {
		if err := page.Navigate(req.URL); err != nil {
			return nil, fmt.Errorf("navigate %s: %w", req.URL, err)
		}
		if req.WaitFor != "" {
			if err := page.Timeout(timeout).WaitElementsMoreThan(req.WaitFor, 0); err != nil {
				return nil, fmt.Errorf("wait for %q: %w", req.WaitFor, err)
			}
		} else {
			// Network-idle + DOM-stable is the general-purpose "page is done".
			_ = page.Timeout(timeout).WaitStable(300 * time.Millisecond)
		}
	}
	for i := 0; i < req.Scrolls; i++ {
		if req.ScrollSelector != "" {
			_, _ = page.Eval("() => { const el = document.querySelector(" +
				strconv.Quote(req.ScrollSelector) + "); if (el) el.scrollTop += " +
				strconv.FormatFloat(scrollStep(req), 'f', 0, 64) + "; return el ? el.scrollTop : 0 }")
		} else {
			_ = page.Mouse.Scroll(0, scrollStep(req), 0)
		}
		page.Timeout(2 * time.Second).MustWaitStable()
	}
	png, err := runActions(ctx, page, req, sess, timeout, res)
	if err != nil {
		return nil, fmt.Errorf("actions: %w", err)
	}
	res.Screenshot = png
	if req.Screenshot && res.Screenshot == nil {
		full, params := shotParams(req)
		png, err := page.Screenshot(full, params)
		if err != nil {
			return nil, fmt.Errorf("screenshot: %w", err)
		}
		res.Screenshot = png
	}
	if req.PDF {
		if r, perr := page.PDF(&proto.PagePrintToPDF{}); perr == nil {
			res.PDF, _ = io.ReadAll(r)
		}
	}
	if req.MHTML {
		if snap, merr := (&proto.PageCaptureSnapshot{
			Format: proto.PageCaptureSnapshotFormatMhtml,
		}).Call(page); merr == nil {
			res.MHTML = []byte(snap.Data)
		}
	}
	if req.PierceDOM {
		// Flatten shadow roots and same-origin iframes into the light DOM —
		// crawl4ai's shadow-dom/iframe parity. Mutates the live page, so it
		// stays opt-in.
		_, _ = page.Eval("() => eval(" + strconv.Quote(pierceDOMJS) + ")")
	}
	info, err := page.Info()
	if err != nil {
		return nil, err
	}
	res.FinalURL = info.URL
	html, err := page.HTML()
	if err != nil {
		return nil, err
	}
	res.HTML = []byte(html)
	if req.WantA11y {
		if snap, aerr := captureA11y(page, sess); aerr == nil {
			res.A11y = snap
		} else {
			res.Warnings = append(res.Warnings, "a11y snapshot failed on this engine: "+aerr.Error())
		}
	}
	if req.WantAgentTools {
		if v, terr := page.Eval(agentToolsJS); terr == nil && !v.Value.Nil() {
			res.WebMCP = true
			for _, t := range v.Value.Get("tools").Arr() {
				res.AgentTools = append(res.AgentTools, t.Str())
			}
		}
	}
	if cs, err := page.Cookies(nil); err == nil {
		for _, c := range cs {
			res.Cookies = append(res.Cookies, &http.Cookie{
				Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
				Secure: c.Secure, HttpOnly: c.HTTPOnly,
			})
		}
	}
	return res, nil
}

// ── remote render service ────────────────────────────────────────────────────

var remoteClient = &http.Client{Timeout: 60 * time.Second}

// renderRemote POSTs {url,wait_ms,scrolls} to WEBX_RENDER_URL and expects
// {final_url, html} or a bare HTML body — our own serve's /render speaks the
// former; Browserless-style endpoints can be adapted by wrapping.
func renderRemote(ctx context.Context, base string, req Request) (*Result, error) {
	waitMs := int64(3000)
	if req.Timeout > 0 {
		waitMs = req.Timeout.Milliseconds()
	}
	payload, _ := json.Marshal(map[string]any{
		"url": req.URL, "wait_ms": waitMs, "scrolls": req.Scrolls, "wait_for": req.WaitFor,
		"actions": req.Actions, "screenshot": req.Screenshot,
		"stealth": req.Stealth, "profile": req.Profile,
		"block_ads": req.BlockTrackers, "text_mode": req.BlockMedia,
		"mobile": req.Mobile, "locale": req.Locale, "timezone": req.Timezone,
		"capture_network": req.NetworkCapture, "capture_console": req.ConsoleCapture,
		"pdf": req.PDF, "mhtml": req.MHTML, "skip_tls": req.SkipTLS,
		"page_session": req.PageSession, "pierce_dom": req.PierceDOM,
		"scroll_selector": req.ScrollSelector, "scroll_by": req.ScrollBy,
		"a11y": req.WantA11y, "agent_tools": req.WantAgentTools,
		"engine": req.Engine,
	})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/render", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := remoteClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("render service %s: %w", base, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("render service: status %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	var out struct {
		FinalURL      string       `json:"final_url"`
		HTML          string       `json:"html"`
		Screenshot    string       `json:"screenshot"` // base64 PNG
		PDFB64        string       `json:"pdf_b64"`
		MHTML         string       `json:"mhtml"`
		Network       []NetRequest `json:"network"`
		Console       []string     `json:"console"`
		ActionReturns []any        `json:"action_returns"`
		A11y          string       `json:"a11y"`
		WebMCP        bool         `json:"webmcp"`
		AgentTools    []string     `json:"agent_tools"`
		Engine        string       `json:"engine"`
		Warnings      []string     `json:"warnings"`
	}
	if err := json.Unmarshal(body, &out); err == nil && out.HTML != "" {
		fu := out.FinalURL
		if fu == "" {
			fu = req.URL
		}
		res := &Result{URL: req.URL, FinalURL: fu, HTML: []byte(out.HTML),
			NetworkRequests: out.Network, Console: out.Console, ActionReturns: out.ActionReturns,
			A11y: out.A11y, WebMCP: out.WebMCP, AgentTools: out.AgentTools,
			Engine: out.Engine, Warnings: out.Warnings}
		if out.Screenshot != "" {
			res.Screenshot, _ = base64.StdEncoding.DecodeString(out.Screenshot)
		}
		if out.PDFB64 != "" {
			res.PDF, _ = base64.StdEncoding.DecodeString(out.PDFB64)
		}
		res.MHTML = []byte(out.MHTML)
		return res, nil
	}
	return &Result{URL: req.URL, FinalURL: req.URL, HTML: body}, nil
}

// scrollStep is the px distance per scroll pass — viewport height default.
func scrollStep(req Request) float64 {
	if req.ScrollBy > 0 {
		return req.ScrollBy
	}
	return 1200
}

// pierceDOMJS is evaluated on the page to flatten shadow roots and
// same-origin iframes into light DOM before HTML capture. Shadow content
// lands in <div data-webx-shadow>, iframes in <div data-webx-iframe> —
// markers survive into the markdown so agents see the boundary.
const pierceDOMJS = `(() => {
  const expandShadow = (root, depth) => {
    if (depth > 4) return;
    root.querySelectorAll('*').forEach(el => {
      if (el.shadowRoot) {
        const div = document.createElement('div');
        div.setAttribute('data-webx-shadow', '1');
        div.innerHTML = el.shadowRoot.innerHTML;
        el.appendChild(div);
        expandShadow(el.shadowRoot, depth + 1);
      }
    });
  };
  expandShadow(document, 0);
  document.querySelectorAll('iframe').forEach(f => {
    try {
      const d = f.contentDocument;
      if (d && d.body && d.body.innerHTML.trim()) {
        const div = document.createElement('div');
        div.setAttribute('data-webx-iframe', f.src || 'about:blank');
        div.innerHTML = d.body.innerHTML;
        f.replaceWith(div);
      }
    } catch (e) { /* cross-origin — leave the frame as-is */ }
  });
  return true;
})()`

// runActions executes the action list on the page; returns a PNG if any
// screenshot action ran. Each step is best-effort bounded by the render
// timeout so a dead selector can't hang the whole render. eval values land
// in res.ActionReturns (Firecrawl javascriptReturns parity).
func runActions(ctx context.Context, page *rod.Page, req Request, sess *pageSession, budget time.Duration, res *Result) ([]byte, error) {
	var png []byte
	per := budget / 4 // one action gets at most a quarter of the render budget
	if per <= 0 {
		per = 5 * time.Second
	}
	for i, a := range req.Actions {
		p := page.Timeout(per)
		var err error
		switch a.Op {
		case "wait":
			err = p.WaitElementsMoreThan(a.Arg, 0)
		case "click":
			err = func() error {
				el, e := resolveElement(p, sess, a.Arg)
				if e != nil {
					return e
				}
				return clickElement(p, el, "click:"+a.Arg)
			}()
		case "act":
			err = act(ctx, p, req, a.Arg)
		case "type", "fill":
			err = func() error {
				el, e := resolveElement(p, sess, a.Arg)
				if e != nil {
					return e
				}
				return el.Input(a.Arg2)
			}()
		case "get":
			err = func() error {
				el, e := resolveElement(p, sess, a.Arg)
				if e != nil {
					return e
				}
				text, e := el.Text()
				if e == nil && res != nil {
					res.ActionReturns = append(res.ActionReturns, text)
				}
				return e
			}()
		case "hover":
			err = func() error {
				el, e := resolveElement(p, sess, a.Arg)
				if e != nil {
					return e
				}
				return el.Hover()
			}()
		case "press":
			if k := keyFor(a.Arg); k != nil {
				err = p.Keyboard.Press(*k)
			} else {
				err = fmt.Errorf("unknown key %q (enter|tab|esc|backspace|space|up|down)", a.Arg)
			}
		case "sleep":
			ms, _ := strconv.Atoi(a.Arg)
			time.Sleep(time.Duration(ms) * time.Millisecond)
		case "scroll":
			_ = p.Mouse.Scroll(0, 1200, 0)
			_ = p.WaitStable(300 * time.Millisecond)
		case "eval":
			// rod.Eval compiles js as a function to .apply() — a bare
			// expression like "document.title" isn't callable. eval() inside
			// a real function handles expressions AND statements.
			var v *proto.RuntimeRemoteObject
			v, err = p.Eval("() => eval(" + strconv.Quote(a.Arg) + ")")
			if err == nil && res != nil {
				res.ActionReturns = append(res.ActionReturns, v.Value.Val())
			}
		case "screenshot":
			full, params := shotParams(req)
			png, err = p.Screenshot(full, params)
		}
		if err != nil {
			return png, fmt.Errorf("step %d (%s %s): %w", i+1, a.Op, a.Arg, err)
		}
	}
	return png, nil
}

// shotParams resolves screenshot options into (fullPage, CDP params).
// Default is the historic full-page PNG; ShotFullPage=false gives the
// viewport-only shot, ShotQuality>0 switches to JPEG.
func shotParams(req Request) (bool, *proto.PageCaptureScreenshot) {
	full := true
	if req.ShotFullPage != nil {
		full = *req.ShotFullPage
	}
	p := &proto.PageCaptureScreenshot{}
	if req.ShotQuality > 0 {
		p.Format = proto.PageCaptureScreenshotFormatJpeg
		q := min(req.ShotQuality, 100)
		p.Quality = &q
	}
	return full, p
}

// keyFor maps friendly names to rod keys ("enter" → input.Enter).
func keyFor(name string) *input.Key {
	switch strings.ToLower(name) {
	case "enter", "return":
		return &input.Enter
	case "tab":
		return &input.Tab
	case "escape", "esc":
		return &input.Escape
	case "backspace":
		return &input.Backspace
	case "space":
		return &input.Space
	case "arrowdown", "down":
		return &input.ArrowDown
	case "arrowup", "up":
		return &input.ArrowUp
	}
	return nil
}

// ── stealth / emulation / interception ───────────────────────────────────────

// stealthJS neutralizes the DOM-level headless fingerprints bot checks
// read. Applied via EvalOnNewDocument so it runs before any page script.
// Kept tight — every patch is a detection surface of its own.
const stealthJS = `
Object.defineProperty(navigator, 'webdriver', {get: () => undefined});
if (!window.chrome) window.chrome = {runtime: {}, loadTimes: () => {}, csi: () => {}};
Object.defineProperty(navigator, 'plugins', {get: () => {
	const p = [{name:'PDF Viewer'},{name:'Chrome PDF Viewer'},{name:'Chromium PDF Viewer'},{name:'Microsoft Edge PDF Viewer'},{name:'WebKit built-in PDF'}];
	p.item = (i) => p[i]; p.namedItem = (n) => p.find(x => x.name === n) || null;
	p.refresh = () => {};
	return p;
}});
Object.defineProperty(navigator, 'languages', {get: () => ['en-US', 'en']});
if (navigator.permissions && navigator.permissions.query) {
	const orig = navigator.permissions.query.bind(navigator.permissions);
	navigator.permissions.query = (p) => p && p.name === 'notifications'
		? Promise.resolve({state: typeof Notification !== 'undefined' ? Notification.permission : 'default'})
		: orig(p);
}
// WebGL vendor/renderer — headless reports SwiftShader, real Chrome a GPU.
for (const proto of [WebGLRenderingContext.prototype, window.WebGL2RenderingContext && WebGL2RenderingContext.prototype]) {
	if (!proto) continue;
	const orig = proto.getParameter;
	proto.getParameter = function(p) {
		if (p === 37445) return 'Intel Inc.';
		if (p === 37446) return 'Intel Iris OpenGL Engine';
		return orig.call(this, p);
	};
}
// Chrome hairline feature-detect: real browsers expose connection.rtt.
if (navigator.connection && navigator.connection.rtt === undefined) {
	Object.defineProperty(navigator.connection, 'rtt', {get: () => 100});
}
`

const mobileUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) " +
	"AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"

// trackerHosts — the common analytics/ad domains crawl4ai's avoid_ads
// blocks. Content extraction never needs them; blocking speeds renders and
// strips tracker noise from network captures.
var trackerHosts = []string{
	"doubleclick.net", "googlesyndication.com", "google-analytics.com",
	"googletagmanager.com", "facebook.net", "connect.facebook.net",
	"hotjar.com", "clarity.ms", "fullstory.com", "segment.io", "segment.com",
	"mixpanel.com", "amplitude.com", "newrelic.com", "nr-data.net",
	"optimizely.com", "adnxs.com", "quantserve.com", "scorecardresearch.com",
	"outbrain.com", "taboola.com", "adroll.com", "criteo.com", "criteo.net",
	"ads-twitter.com", "analytics.twitter.com", "bat.bing.com",
	"pinimg.com", "ct.pinterest.com", "mc.yandex.ru", "mc.yandex.com",
	"chartbeat.com", "chartbeat.net", "parsely.com", "bounceexchange.com",
}

// collectTelemetry wires Network/Console event listeners when requested.
// go-rod's EachEvent returns the event-drain loop itself — it must run in a
// goroutine or undelivered events backpressure the page's CDP stream and hang
// navigation. The loop ends when the page closes; nothing else to stop.
func collectTelemetry(page *rod.Page, req Request, res *Result) func() {
	if !req.NetworkCapture && !req.ConsoleCapture {
		return func() {}
	}
	var mu sync.Mutex
	pending := map[string]*NetRequest{}
	go page.EachEvent(
		func(e *proto.NetworkRequestWillBeSent) {
			if !req.NetworkCapture {
				return
			}
			mu.Lock()
			pending[string(e.RequestID)] = &NetRequest{
				Method:       e.Request.Method,
				URL:          e.Request.URL,
				ResourceType: string(e.Type),
			}
			mu.Unlock()
		},
		func(e *proto.NetworkResponseReceived) {
			if !req.NetworkCapture {
				return
			}
			mu.Lock()
			if r, ok := pending[string(e.RequestID)]; ok {
				r.Status = e.Response.Status
				r.MimeType = e.Response.MIMEType
				res.NetworkRequests = append(res.NetworkRequests, *r)
				delete(pending, string(e.RequestID))
			}
			mu.Unlock()
		},
		func(e *proto.RuntimeConsoleAPICalled) {
			if !req.ConsoleCapture {
				return
			}
			var b strings.Builder
			for _, a := range e.Args {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(fmt.Sprint(a.Value.Val()))
			}
			if b.Len() == 0 {
				return
			}
			mu.Lock()
			if len(res.Console) < 200 { // bound: chatty pages shouldn't OOM a job
				res.Console = append(res.Console, string(e.Type)+" "+b.String())
			}
			mu.Unlock()
		},
	)()
	return func() {}
}

// hijackResources drops tracker/media requests when asked. rod's request
// router runs in-page so patterns stay per-render; the router must run for
// the page's lifetime — the returned stop func is deferred to page close.
func hijackResources(page *rod.Page, req Request) func() {
	if !req.BlockTrackers && !req.BlockMedia {
		return func() {}
	}
	router := page.HijackRequests()
	blocked := func(url string, rtype proto.NetworkResourceType) bool {
		if req.BlockMedia {
			switch rtype {
			case proto.NetworkResourceTypeImage,
				proto.NetworkResourceTypeMedia,
				proto.NetworkResourceTypeFont,
				proto.NetworkResourceTypeStylesheet:
				return true
			}
		}
		if req.BlockTrackers {
			for _, t := range trackerHosts {
				if strings.Contains(url, t) {
					return true
				}
			}
		}
		return false
	}
	router.MustAdd("*", func(ctx *rod.Hijack) {
		if blocked(ctx.Request.URL().String(), ctx.Request.Type()) {
			ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
			return
		}
		ctx.ContinueRequest(&proto.FetchContinueRequest{})
	})
	go router.Run()
	return func() { _ = router.Stop() }
}

// ── session cookie bridging ──────────────────────────────────────────────────

// HarvestCookies returns the cookies a rendered page set — callers drop
// them into a session jar so a cf_clearance solved in Chrome is reusable
// by the HTTP path.
func HarvestCookies(res *Result) []*http.Cookie {
	if res == nil {
		return nil
	}
	return res.Cookies
}

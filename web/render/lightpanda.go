package render

// lightpanda.go — the "light" render engine. Lightpanda (the Zig headless
// browser built for agents — ~16x lighter than Chrome, ~9x faster) speaks
// CDP, so the whole session/action pipeline rides it unchanged via ControlURL.
// Fidelity is lower — parts of CDP and the web platform are unimplemented —
// so it's opt-in (--engine=light / WEBX_RENDER_ENGINE=light) and every
// document carries a render-engine warning.
import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
)

var lightCmd *exec.Cmd // the `lightpanda serve` we spawned, if any

// engineName reports which backend a request will use — for Result.Engine
// and honest warnings (explicit CDP attach counts as its own engine).
func engineName(req Request) string {
	e := req.Engine
	if e == "" {
		e = os.Getenv("WEBX_RENDER_ENGINE")
	}
	if e == "light" || e == "lightpanda" {
		return "lightpanda"
	}
	if req.CDPURL != "" || os.Getenv("WEBX_CDP_URL") != "" {
		return "cdp"
	}
	return "chrome"
}

// lightBrowser attaches to a Lightpanda CDP server, pooled like the Chrome
// browser: explicit WEBX_LIGHTPANDA_URL, then a server already on :9222,
// then a spawned `lightpanda serve` on a free port.
func lightBrowser() (*rod.Browser, error) {
	const key = "lightpanda"
	if poolBrowser != nil && poolKey == key {
		if _, err := poolBrowser.Version(); err == nil {
			return poolBrowser, nil
		}
		poolBrowser = nil
	}
	u, err := lightCDP()
	if err != nil {
		return nil, err
	}
	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("lightpanda connect %s: %w", u, err)
	}
	poolBrowser, poolKey = browser, key
	return browser, nil
}

func lightCDP() (string, error) {
	if ep := os.Getenv("WEBX_LIGHTPANDA_URL"); ep != "" {
		ws, _, err := cdpVersion(ep)
		return ws, err
	}
	if ws, err := probeLightpanda("http://127.0.0.1:9222"); err == nil {
		return ws, nil
	}
	bin, err := exec.LookPath("lightpanda")
	if err != nil {
		// `webx install lightpanda` drops it here, off-PATH.
		if h, herr := os.UserHomeDir(); herr == nil {
			cand := filepath.Join(h, ".webx", "bin", "lightpanda")
			if _, serr := os.Stat(cand); serr == nil {
				bin = cand
			}
		}
	}
	if bin == "" {
		return "", fmt.Errorf("engine=light needs lightpanda — `webx install lightpanda`, brew, or set WEBX_LIGHTPANDA_URL to a running server")
	}
	port := freePort()
	lightCmd = exec.Command(bin, "serve", "--host", "127.0.0.1", "--port", port, "--log-level", "error")
	if err := lightCmd.Start(); err != nil {
		return "", fmt.Errorf("lightpanda serve: %w", err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if ws, err := probeLightpanda("http://127.0.0.1:" + port); err == nil {
			return ws, nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return "", fmt.Errorf("lightpanda did not open CDP on :%s", port)
}

// probeLightpanda resolves a base URL's ws endpoint and only accepts it when
// /json/version identifies the browser as Lightpanda — a Chrome devtools
// listener on :9222 must not be silently hijacked as the "light" engine.
func probeLightpanda(base string) (string, error) {
	ws, browser, err := cdpVersion(base)
	if err != nil {
		return "", err
	}
	if !strings.Contains(strings.ToLower(browser), "lightpanda") {
		return "", fmt.Errorf("%s is %q, not lightpanda", base, browser)
	}
	return ws, nil
}

// cdpVersion resolves a CDP endpoint to its browser websocket URL. ws://
// inputs pass through; http(s):// queries /json/version.
func cdpVersion(base string) (ws, browser string, err error) {
	if strings.HasPrefix(base, "ws://") || strings.HasPrefix(base, "wss://") {
		return base, "", nil
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(strings.TrimSuffix(base, "/") + "/json/version")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var v struct {
		Browser string `json:"Browser"`
		WS      string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil || v.WS == "" {
		return "", "", fmt.Errorf("no webSocketDebuggerUrl at %s", base)
	}
	return v.WS, v.Browser, nil
}

func freePort() string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "9223"
	}
	defer l.Close()
	return fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
}

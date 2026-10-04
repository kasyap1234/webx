package fetch

import (
	"strings"
	"testing"
)

func TestNeedsRenderSPAShell(t *testing.T) {
	html := []byte(`<html><body><div id="root"></div><script src="/bundle.js"></script></body></html>`)
	if !NeedsRender(html, 20) {
		t.Fatal("SPA shell should need render")
	}
}

func TestNeedsRenderScriptDominated(t *testing.T) {
	// quotes.toscrape.com/js/ pattern: no framework markers, all data in JS
	script := `<script>var data = [` + strings.Repeat(`{"text":"q","author":"a"},`, 40) + `];</script>`
	html := []byte(`<html><body><nav>top</nav>` + script + `</body></html>`)
	if !NeedsRender(html, 40) {
		t.Fatal("script-dominated body should need render")
	}
}

func TestNeedsRenderSSRPage(t *testing.T) {
	body := strings.Repeat("<p>real server-rendered content paragraph here. </p>", 60)
	html := []byte(`<html><body>` + body + `</body></html>`)
	if NeedsRender(html, len(body)) {
		t.Fatal("SSR content must not need render")
	}
}

func TestNeedsRenderChallenge(t *testing.T) {
	html := []byte(`<html><title>Just a moment...</title><body>Enable JavaScript and cookies to continue</body></html>`)
	if !NeedsRender(html, 10) {
		t.Fatal("challenge page should need render")
	}
}

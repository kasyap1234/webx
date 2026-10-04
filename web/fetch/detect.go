package fetch

import (
	"bytes"
	"regexp"
)

// JS-rendering detection: distinguishes "thin extraction" from "the page
// actually is thin". The signal is *extraction output* relative to what a
// client-rendered page looks like — empty shell + framework markers + low
// text density.
var (
	spaMarkers = []string{
		`id="root"`, `id="app"`, `id="__next"`, `id="___gatsby"`,
		`id="__nuxt"`, `data-reactroot`, `ng-app`, `ng-version`,
		`window.__NEXT_DATA__`, `window.__NUXT__`, `data-server-rendered`,
		`data-v-app`, `ember-view`,
	}
	cfChallengeRe = regexp.MustCompile(`(?i)just a moment|checking your browser|cf-chl-|challenge-platform|attention required|verify you are human`)
	scriptBlockRe = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
)

// NeedsRender reports whether a fetched page is probably client-rendered —
// three signals, any sufficient when extraction came back thin:
//  1. an outright JS challenge (Cloudflare "Just a moment…")
//  2. an SPA container shell (id="root"/"app"/__next + friends)
//  3. a script-dominated body — >50% of bytes inside <script> tags, the
//     data-embedded-JS pattern (var data=[...] + jQuery render)
func NeedsRender(rawHTML []byte, extractedLen int) bool {
	if cfChallengeRe.Match(rawHTML) {
		return true
	}
	if extractedLen > 800 {
		return false // real content made it through — not an empty shell
	}
	if extractedLen < 400 {
		var markers int
		for _, m := range spaMarkers {
			if bytes.Contains(rawHTML, []byte(m)) {
				markers++
			}
		}
		if markers >= 1 {
			return true
		}
		if len(rawHTML) > 0 {
			var scriptBytes int
			for _, m := range scriptBlockRe.FindAll(rawHTML, -1) {
				scriptBytes += len(m)
			}
			if float64(scriptBytes)/float64(len(rawHTML)) > 0.5 {
				return true
			}
		}
	}
	return false
}

// IsChallenge reports whether rawHTML is a bot-challenge page rather than
// content — distinct from NeedsRender because rendering is the *fix*, not
// just the signal.
func IsChallenge(rawHTML []byte) bool {
	return cfChallengeRe.Match(rawHTML)
}

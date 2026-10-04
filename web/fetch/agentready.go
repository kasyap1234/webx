package fetch

// agentready.go — how agent-friendly is this host? One struct answering
// what every agent asks before trusting a site: does it publish llms.txt,
// does it serve markdown natively, which AI crawlers does robots.txt allow,
// does it expose WebMCP tools, and what schema.org types does it carry.
// Nobody else emits this; it's all metadata we can compute cheaply.
import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AgentReady is the host's agent-friendliness report.
type AgentReady struct {
	MarkdownNative bool              `json:"markdown_native,omitempty"` // origin served text/markdown directly
	LLMsTxt        string            `json:"llms_txt,omitempty"`        // URL when /llms.txt exists
	AIBots         map[string]string `json:"ai_bots,omitempty"`         // bot → allowed|blocked|unmentioned
	JSONLDTypes    []string          `json:"jsonld_types,omitempty"`    // schema.org @types present
	WebMCP         bool              `json:"webmcp,omitempty"`          // navigator.modelContext detected (render only)
	AgentTools     []string          `json:"agent_tools,omitempty"`     // WebMCP tool names when listable
	APICatalog     string            `json:"api_catalog,omitempty"`     // /.well-known/api-catalog URL (IETF api-catalog — ARD's site catalog)
	// Compliance surface — what an accountable agent checks before consuming:
	TDMReserved bool     `json:"tdm_reserved,omitempty"` // TDMRep well-known or tdm-reservation header — EU text/data-mining opt-out
	AITxt       bool     `json:"ai_txt,omitempty"`       // /ai.txt present (spawning.ai-style consent file)
	AgentsMD    bool     `json:"agents_md,omitempty"`    // /AGENTS.md present (agent instructions standard)
	AgentCard   string   `json:"agent_card,omitempty"`   // /.well-known/agent.json — A2A agent card URL
	Sitemaps    []string `json:"sitemaps,omitempty"`     // Sitemap: lines from robots.txt — the host's own map
}

// aiBotAgents are the crawlers sites explicitly gate in robots.txt.
var aiBotAgents = []string{
	"gptbot", "claudebot", "ccbot", "google-extended", "bytespider",
	"amazonbot", "perplexitybot", "anthropic-ai", "chatgpt-user", "meta-externalagent",
}

var agentReadyClient = &http.Client{Timeout: 6 * time.Second}

// probeAgentReady fills Document.AgentReady from host metadata. Probes are
// small well-known GETs — robots.txt, a few standards files — best-effort.
func probeAgentReady(ctx context.Context, finalURL *url.URL, doc *Document, respHeader http.Header) {
	ar := &AgentReady{}
	if doc.AgentReady != nil && doc.AgentReady.MarkdownNative {
		ar.MarkdownNative = true
	}
	if doc.LLMSTxt {
		ar.LLMsTxt = finalURL.Scheme + "://" + finalURL.Host + "/llms.txt"
	}
	if bots, sitemaps := aiBotPolicy(ctx, finalURL); len(bots) > 0 || len(sitemaps) > 0 {
		ar.AIBots = bots
		ar.Sitemaps = sitemaps
	}
	if cat := probeWellKnown(ctx, finalURL, "/.well-known/api-catalog", "json", "linkset"); cat != "" {
		ar.APICatalog = cat
	}
	if card := probeWellKnown(ctx, finalURL, "/.well-known/agent.json", "json"); card != "" {
		ar.AgentCard = card
	}
	// TDMRep — the EU's machine-readable text/data-mining reservation:
	// /.well-known/tdmrep.json or the tdm-reservation response header.
	if respHeader != nil && strings.EqualFold(strings.TrimSpace(respHeader.Get("tdm-reservation")), "1") {
		ar.TDMReserved = true
	}
	if !ar.TDMReserved && probeWellKnown(ctx, finalURL, "/.well-known/tdmrep.json", "json") != "" {
		ar.TDMReserved = true
	}
	if probeWellKnown(ctx, finalURL, "/ai.txt", "text") != "" {
		ar.AITxt = true
	}
	if probeWellKnown(ctx, finalURL, "/AGENTS.md", "text", "markdown") != "" {
		ar.AgentsMD = true
	}
	for _, raw := range doc.JSONLD {
		var probe struct {
			Type any `json:"@type"`
		}
		if json.Unmarshal(raw, &probe) == nil {
			switch t := probe.Type.(type) {
			case string:
				ar.JSONLDTypes = appendIfNew(ar.JSONLDTypes, t)
			case []any:
				for _, v := range t {
					if s, ok := v.(string); ok {
						ar.JSONLDTypes = appendIfNew(ar.JSONLDTypes, s)
					}
				}
			}
		}
	}
	if ar.MarkdownNative || ar.LLMsTxt != "" || len(ar.AIBots) > 0 || len(ar.JSONLDTypes) > 0 ||
		ar.WebMCP || ar.APICatalog != "" || ar.TDMReserved || ar.AITxt || ar.AgentsMD || ar.AgentCard != "" {
		doc.AgentReady = ar
	}
}

// probeWellKnown GETs a well-known/site-root file and returns its URL when
// it answers 200 with one of the expected content-type fragments.
func probeWellKnown(ctx context.Context, u *url.URL, path string, wantCT ...string) string {
	target := u.Scheme + "://" + u.Host + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return ""
	}
	signBotRequest(req)
	resp, err := agentReadyClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	for _, w := range wantCT {
		if strings.Contains(ct, w) {
			return target
		}
	}
	return ""
}

func appendIfNew(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// aiBotPolicy parses robots.txt for the known AI agents plus the Sitemap:
// lines — the host's own URL inventory is agent-relevant metadata too. A bot
// is "blocked" when its group (or *) disallows /, "allowed" otherwise.
func aiBotPolicy(ctx context.Context, u *url.URL) (map[string]string, []string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		u.Scheme+"://"+u.Host+"/robots.txt", nil)
	if err != nil {
		return nil, nil
	}
	signBotRequest(req)
	resp, err := agentReadyClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))

	// Group robots.txt into per-UA rule sets.
	groups := map[string][]string{} // ua → disallow patterns
	var cur []string
	var curUAs []string
	inGroup := false
	flush := func() {
		for _, ua := range curUAs {
			groups[ua] = append(groups[ua], cur...)
		}
		cur, curUAs = nil, nil
	}
	var sitemaps []string
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			if inGroup {
				flush()
				inGroup = false
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		switch k {
		case "sitemap":
			if v != "" {
				sitemaps = append(sitemaps, v)
			}
		case "user-agent":
			if inGroup && len(cur) > 0 { // new UA line after rules → new group
				flush()
			}
			inGroup = true
			curUAs = append(curUAs, strings.ToLower(v))
		case "disallow":
			cur = append(cur, "d:"+v)
		case "allow":
			cur = append(cur, "a:"+v)
		}
	}
	if inGroup {
		flush()
	}

	out := map[string]string{}
	// Whole-site gate only: Disallow:/ blocks the bot unless an explicit
	// Allow:/ exists. Per-path nuance is intentionally not reported.
	blocked := func(rules []string) bool {
		dis, allow := false, false
		for _, r := range rules {
			if r == "d:/" {
				dis = true
			}
			if r == "a:/" {
				allow = true
			}
		}
		return dis && !allow
	}
	for _, bot := range aiBotAgents {
		rules, named := groups[bot]
		if !named {
			rules = groups["*"]
		}
		if blocked(rules) {
			out[bot] = "blocked"
		} else {
			out[bot] = "allowed"
		}
	}
	return out, sitemaps
}

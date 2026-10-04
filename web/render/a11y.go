package render

// a11y.go — the accessibility snapshot: a compact role-tree of the live
// page with stable @eN refs on interactive elements. This is the page
// model agents *act* on (Playwright MCP's whole insight) — every scraper
// emits markdown for reading; refs + `click:@e5` make webx read+act.
import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// interactiveRoles are the roles that get @eN refs — things an agent
// plausibly clicks/types into. Landmark+structure roles render without
// refs.
var interactiveRoles = map[string]bool{
	"link": true, "button": true, "textbox": true, "searchbox": true,
	"combobox": true, "listbox": true, "checkbox": true, "radio": true,
	"switch": true, "menuitem": true, "option": true, "tab": true,
	"slider": true, "spinbutton": true,
}

// keepRoles always emit even without a name — landmarks + structure.
var keepRoles = map[string]bool{
	"document": true, "main": true, "navigation": true, "banner": true,
	"contentinfo": true, "complementary": true, "region": true, "form": true,
	"search": true, "heading": true, "menu": true, "menubar": true,
	"tablist": true, "tree": true, "table": true, "dialog": true,
	"alert": true, "img": true, "list": true,
}

// skipRoles are pure noise for an agent — the parent's name already
// carries the text; these just double the tree.
var skipRoles = map[string]bool{
	"statictext": true, "inlinetextbox": true, "generic": true,
	"none": true, "linebreak": true, "whitespace": true,
}

// axString unwraps an AXValue to its string form ("" when nil).
func axString(v *proto.AccessibilityAXValue) string {
	if v == nil || v.Value.Nil() {
		return ""
	}
	return v.Value.Str()
}

// captureA11y dumps the page's AX tree as an indented list. Interactive
// nodes get `[ref=eN]` markers whose backend node IDs land in sess.refs —
// the session keeps them so a later `click:@e5` resolves. Render-only:
// HTTP pages have no layout, hence no accessibility tree.
func captureA11y(page *rod.Page, sess *pageSession) (string, error) {
	out, err := (proto.AccessibilityGetFullAXTree{}).Call(page)
	if err != nil {
		return "", err
	}
	byID := map[proto.AccessibilityAXNodeID]*proto.AccessibilityAXNode{}
	hasParent := map[proto.AccessibilityAXNodeID]bool{}
	for _, n := range out.Nodes {
		byID[n.NodeID] = n
		for _, c := range n.ChildIDs {
			hasParent[c] = true
		}
	}
	var root *proto.AccessibilityAXNode
	for _, n := range out.Nodes {
		if !hasParent[n.NodeID] {
			root = n
			break
		}
	}
	if root == nil && len(out.Nodes) > 0 {
		root = out.Nodes[0]
	}

	var b strings.Builder
	refs := map[string]proto.DOMBackendNodeID{}
	lines := 0
	var walk func(n *proto.AccessibilityAXNode, depth int)
	walk = func(n *proto.AccessibilityAXNode, depth int) {
		if n == nil || depth > 40 || lines > 400 {
			return
		}
		role := strings.ToLower(axString(n.Role))
		if role == "rootwebarea" {
			role = "document" // Chrome's name — normalize to the W3C term
		}
		name := axString(n.Name)
		if !n.Ignored && role != "" && !skipRoles[role] {
			keep := keepRoles[role] || interactiveRoles[role]
			if !keep && name != "" {
				keep = true // named content is worth a line
			}
			if keep {
				b.WriteString(strings.Repeat("  ", depth) + "- " + role)
				if name != "" {
					b.WriteString(" " + strconv.Quote(name))
				}
				if interactiveRoles[role] && n.BackendDOMNodeID != 0 && sess != nil {
					ref := "e" + strconv.Itoa(len(refs)+1)
					refs[ref] = n.BackendDOMNodeID
					b.WriteString(" [ref=" + ref + "]")
				}
				b.WriteByte('\n')
				lines++
			}
		}
		for _, c := range n.ChildIDs {
			walk(byID[c], depth+1)
		}
	}
	walk(root, 0)
	if sess != nil {
		poolMu.Lock()
		sess.refs = refs
		poolMu.Unlock()
	}
	return b.String(), nil
}

// resolveElement turns an action target into a live element: "@eN" resolves
// a ref captured by a previous a11y snapshot on the same page session;
// anything else is a plain CSS selector.
func resolveElement(p *rod.Page, sess *pageSession, arg string) (*rod.Element, error) {
	if !strings.HasPrefix(arg, "@") {
		return p.Element(arg)
	}
	if sess == nil {
		return nil, fmt.Errorf("ref %s needs --page-session — refs only live on named sessions", arg)
	}
	poolMu.Lock()
	id, ok := sess.refs[strings.TrimPrefix(arg, "@")]
	poolMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no element ref %s — run an a11y snapshot on this session first", arg)
	}
	depth := 1
	desc, err := (proto.DOMDescribeNode{BackendNodeID: id, Depth: &depth}).Call(p)
	if err != nil {
		return nil, err
	}
	return p.ElementFromNode(desc.Node)
}

// occlusionJS checks elementFromPoint at the element's center — agent-
// browser's "fail early" check. A covered click would land on the modal or
// consent banner instead, so erroring with *what* covers it beats a silent
// miss. this/top/ancestor containment all count as a clean hit: the click
// still lands inside the same interactive surface.
const occlusionJS = `() => {
  const r = this.getBoundingClientRect();
  if (!r.width || !r.height) return {hidden: true};
  const top = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
  if (!top) return {outside: true};
  if (top === this || this.contains(top) || top.contains(this)) return null;
  return {cover: top.tagName.toLowerCase() + (top.id ? '#' + top.id : '') +
    (typeof top.className === 'string' && top.className.trim()
      ? '.' + top.className.trim().split(/\s+/).slice(0, 2).join('.') : '')};
}`

// clickElement scrolls, occlusion-checks, then clicks — the check failures
// name the covering element so the agent knows what to dismiss next.
func clickElement(p *rod.Page, el *rod.Element, what string) error {
	_ = el.ScrollIntoView()
	if v, err := el.Eval(occlusionJS); err == nil && !v.Value.Nil() {
		m := v.Value.Map()
		switch {
		case m["hidden"].Bool():
			return fmt.Errorf("%s has no visible box", what)
		case m["outside"].Bool():
			return fmt.Errorf("%s stayed outside the viewport", what)
		case m["cover"].Str() != "":
			return fmt.Errorf("%s is covered by %s — dismiss or interact with that element first", what, m["cover"].Str())
		}
	}
	return el.Click(proto.InputMouseButtonLeft, 1)
}

// ── act: natural-language actions ────────────────────────────────────────────

// actResolution is a cached instruction→element fingerprint — Scrapling's
// adaptive-relocation idea: not just a selector, but the element's identity
// (attrs + text), so a site redesign can be re-matched deterministically
// before we spend another LLM call.
type actResolution struct {
	Selector string            `json:"selector,omitempty"`
	Tag      string            `json:"tag,omitempty"`
	Text     string            `json:"text,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"` // name|aria-label|type|role|placeholder
}

var (
	actMu    sync.Mutex
	actCache = map[string]actResolution{} // host|instruction → resolution
)

// actDumpJS tags visible interactive elements with data-webx-el indices
// and returns a compact "idx tag role text" listing for the LLM resolver.
const actDumpJS = `() => {
	const els = [...document.querySelectorAll('button,a,input,select,textarea,summary,[role="button"],[role="link"],[role="checkbox"],[role="tab"],[role="menuitem"],[onclick]')]
		.filter(e => e.offsetParent !== null).slice(0, 150);
	els.forEach((e, i) => e.setAttribute('data-webx-el', String(i)));
	return els.map((e, i) => i + ' ' + e.tagName.toLowerCase() + ' ' +
		(e.getAttribute('role') || e.type || '') + ' "' +
		((e.textContent || e.value || e.placeholder || e.getAttribute('aria-label') || '')
			.trim().replace(/\s+/g, ' ').slice(0, 80)) + '"').join('\n')
}`

// actDeriveJS builds a stable selector+fingerprint descriptor for a
// data-webx-el element — id > name > aria-label > tag+text fallback, plus
// the identity attrs healing scores against.
const actDeriveJS = `(i) => {
	const e = document.querySelector('[data-webx-el="' + i + '"]');
	if (!e) return null;
	const tag = e.tagName.toLowerCase();
	const attrs = {};
	for (const k of ['name', 'aria-label', 'type', 'role', 'placeholder']) {
		const v = e.getAttribute(k); if (v) attrs[k] = v;
	}
	const t = (e.textContent || e.value || e.placeholder || '').trim().replace(/\s+/g, ' ').slice(0, 60);
	let selector = '';
	if (e.id) selector = '#' + CSS.escape(e.id);
	else if (e.name) selector = tag + '[name="' + e.name + '"]';
	else if (attrs['aria-label']) selector = tag + '[aria-label="' + attrs['aria-label'] + '"]';
	return {selector: selector, tag: tag, text: t, attrs: attrs};
}`

// actHealJS scores every tagged candidate against a stored fingerprint —
// tag match +1, exact text +3, half-token text overlap scaled, each exact
// identity attr +2. Returns the best data-webx-el index when the top score
// clears the heal bar (3.0): same tag + same text, or 2+ identity attrs.
const actHealJS = `(fp) => {
	const els = [...document.querySelectorAll('[data-webx-el]')];
	let best = -1, bestScore = 0;
	for (const e of els) {
		let s = 0;
		if (fp.tag && e.tagName.toLowerCase() === fp.tag) s += 1;
		const t = (e.textContent || e.value || e.placeholder || e.getAttribute('aria-label') || '')
			.trim().replace(/\s+/g, ' ').slice(0, 80);
		if (fp.text) {
			if (t === fp.text) s += 3;
			else {
				const fw = fp.text.toLowerCase().split(' ').filter(w => w.length > 2);
				const tw = new Set(t.toLowerCase().split(' '));
				const hit = fw.filter(w => tw.has(w)).length;
				if (fw.length && hit / fw.length >= 0.5) s += 2 * hit / fw.length;
			}
		}
		for (const k in (fp.attrs || {})) {
			const v = fp.attrs[k];
			if (v && (e.getAttribute(k) === v || (k === 'name' && e.name === v) || (k === 'type' && e.type === v))) s += 2;
		}
		if (s > bestScore) { bestScore = s; best = parseInt(e.getAttribute('data-webx-el')); }
	}
	return {idx: best, score: bestScore};
}`

// clickResolved replays a cached resolution: CSS selector → fingerprint
// heal (re-dump candidates, score vs identity) → tag+text fallback.
func clickResolved(p *rod.Page, res actResolution) error {
	if res.Selector != "" {
		if el, err := p.Element(res.Selector); err == nil {
			return clickElement(p, el, "act")
		}
	}
	// Self-heal: selector broke — rescore the live candidates against the
	// fingerprint. Beats spending an LLM call on every minor redesign.
	if res.Tag != "" || res.Text != "" || len(res.Attrs) > 0 {
		if _, derr := p.Eval(actDumpJS); derr == nil { // re-tag live DOM
			if hv, herr := p.Eval(actHealJS, map[string]any{
				"tag": res.Tag, "text": res.Text, "attrs": res.Attrs,
			}); herr == nil {
				if m := hv.Value.Map(); m["score"].Num() >= 3.0 && m["idx"].Int() >= 0 {
					if el, eerr := p.Element(`[data-webx-el="` + strconv.Itoa(int(m["idx"].Num())) + `"]`); eerr == nil {
						return clickElement(p, el, "act(healed)")
					}
				}
			}
		}
	}
	if res.Tag != "" && res.Text != "" {
		if el, err := p.ElementR(res.Tag, res.Text); err == nil {
			return clickElement(p, el, "act")
		}
	}
	return fmt.Errorf("cached resolution failed")
}

// act resolves a natural-language instruction to an element and clicks
// it. The candidate dump + index answer means the LLM only ever sees a
// numbered list, never raw HTML.
func act(ctx context.Context, p *rod.Page, req Request, instruction string) error {
	if req.ResolveAct == nil {
		return fmt.Errorf("act needs an LLM resolver — set WEBX_LLM_BASE/WEBX_LLM_KEY")
	}
	host := ""
	if info, err := p.Info(); err == nil {
		if u, perr := url.Parse(info.URL); perr == nil {
			host = u.Host
		}
	}
	key := host + "|" + instruction
	actMu.Lock()
	cached, ok := actCache[key]
	actMu.Unlock()
	if ok {
		if err := clickResolved(p, cached); err == nil {
			return nil
		} // stale — resolve fresh
	}
	v, err := p.Eval(actDumpJS)
	if err != nil {
		return fmt.Errorf("act dump: %w", err)
	}
	dump := v.Value.Str()
	if dump == "" {
		return fmt.Errorf("no interactive elements on page")
	}
	idx, err := req.ResolveAct(ctx, dump, instruction)
	if err != nil {
		return err
	}
	// Click the marked element directly — index→attr is exact for this page.
	el, err := p.Element(`[data-webx-el="` + strconv.Itoa(idx) + `"]`)
	if err != nil {
		return fmt.Errorf("act: element index %d not found", idx)
	}
	// Cache a stable resolution for replays (in-memory, per process) —
	// selector plus the identity fingerprint self-healing scores against.
	var res actResolution
	if dv, derr := p.Eval(actDeriveJS, idx); derr == nil {
		m := dv.Value.Map()
		res = actResolution{
			Selector: m["selector"].Str(), Tag: m["tag"].Str(), Text: m["text"].Str(),
		}
		if am := m["attrs"].Map(); len(am) > 0 {
			res.Attrs = map[string]string{}
			for k, v := range am {
				res.Attrs[k] = v.Str()
			}
		}
		actMu.Lock()
		actCache[key] = res
		actMu.Unlock()
	}
	return clickElement(p, el, "act:"+instruction)
}

// agentToolsJS probes for WebMCP (navigator.modelContext) — the W3C early
// preview where sites expose tools to agents directly. Detection only.
const agentToolsJS = `() => {
	const mc = navigator.modelContext;
	if (!mc) return null;
	const names = new Set();
	try { (mc.listTools?.() || []).forEach(t => names.add(t.name || String(t))); } catch (e) {}
	try { Object.keys(mc.tools || {}).forEach(k => names.add(k)); } catch (e) {}
	return { present: true, tools: [...names] };
}`

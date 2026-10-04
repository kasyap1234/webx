package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChunkMarkdownBreadcrumbs(t *testing.T) {
	md := "# Guide\n\nIntro text.\n\n## Install\n\nInstall steps here.\n\n### Linux\n\nLinux-specific bit.\n\n## Usage\n\nUsage text."
	chunks := ChunkMarkdown(md)
	if len(chunks) < 4 {
		t.Fatalf("expected ≥4 chunks, got %d: %+v", len(chunks), chunks)
	}
	// The Linux chunk must carry the full breadcrumb.
	var linux *Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Text, "Linux-specific") {
			linux = &chunks[i]
		}
	}
	if linux == nil || linux.HeadingPath != "Guide > Install > Linux" {
		t.Fatalf("breadcrumb wrong: %+v", linux)
	}
	// Usage resets the path at level 2 — "Install" must not leak in.
	var usage *Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Text, "Usage text") {
			usage = &chunks[i]
		}
	}
	if usage == nil || strings.Contains(usage.HeadingPath, "Install") {
		t.Fatalf("level reset failed: %+v", usage)
	}
	for _, c := range chunks {
		if c.EstTokens != (len(c.Text)+3)/4 {
			t.Fatalf("est_tokens off on %+v", c)
		}
	}
}

func TestChunkMarkdownOversized(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Big\n\n")
	for i := 0; i < 40; i++ {
		b.WriteString(strings.Repeat("word ", 40) + "\n\n")
	}
	chunks := ChunkMarkdown(b.String())
	if len(chunks) < 2 {
		t.Fatalf("oversized section should split, got %d", len(chunks))
	}
	for _, c := range chunks {
		if c.HeadingPath != "Big" {
			t.Fatalf("split chunks lost heading path: %q", c.HeadingPath)
		}
	}
}

func TestEdgeMarkdown(t *testing.T) {
	srv := serve(t, "text/markdown; charset=utf-8", "# Edge Title\n\nServed **directly** as markdown.\n\n## Section\n\nBody.")
	doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Extractor != "edge-markdown" {
		t.Fatalf("extractor = %q, want edge-markdown", doc.Extractor)
	}
	if doc.Title != "Edge Title" {
		t.Fatalf("title = %q — first ATX heading should win", doc.Title)
	}
	if !strings.Contains(doc.Markdown, "Served **directly**") {
		t.Fatal("body not passed through")
	}
	if doc.AgentReady == nil || !doc.AgentReady.MarkdownNative {
		t.Fatal("agent_ready.markdown_native should be set")
	}
	if doc.EstTokens != (len(doc.Markdown)+3)/4 {
		t.Fatal("est_tokens not populated")
	}
}

func TestAIBotPolicy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(`User-agent: *
Disallow: /private

User-agent: GPTBot
Disallow: /

User-agent: ClaudeBot
Allow: /
`))
		case "/llms.txt":
			w.WriteHeader(404)
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body><h1>page</h1><p>content here for the test</p></body></html>`))
		}
	}))
	defer srv.Close()
	doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL + "/", WantAgentReady: true})
	if err != nil {
		t.Fatal(err)
	}
	if doc.AgentReady == nil || doc.AgentReady.AIBots == nil {
		t.Fatal("agent_ready.ai_bots missing")
	}
	if doc.AgentReady.AIBots["gptbot"] != "blocked" {
		t.Fatalf("gptbot should be blocked: %v", doc.AgentReady.AIBots)
	}
	if doc.AgentReady.AIBots["claudebot"] != "allowed" {
		t.Fatalf("claudebot should be allowed: %v", doc.AgentReady.AIBots)
	}
	if doc.AgentReady.AIBots["ccbot"] != "allowed" {
		t.Fatalf("ccbot falls back to * rules — /private only, should be allowed: %v", doc.AgentReady.AIBots)
	}
}

package fetch

import (
	"strings"
	"testing"
)

func TestFitMarkdown(t *testing.T) {
	md := strings.Join([]string{
		"# Docs Home",
		"Welcome to our product. This site has lots of pages and navigation boilerplate that is not relevant to any particular question at all.",
		"## Installing the package\nRun `npm install foo` to install the package. The package manager resolves dependencies automatically and adds them to your lockfile.",
		"## Marketing copy\nWe are a great company with a lovely mission statement and happy customers all around the world today.",
		"## Configuring the timeout\nSet `timeout: 30` in the config file to control request timeout behavior. The timeout applies per request.",
		"Footer links careers blog contact press legal privacy terms conditions.",
	}, "\n\n")

	fit := FitMarkdown(md, "configuring request timeout")
	if !strings.Contains(fit, "timeout") {
		t.Fatalf("fit markdown lost the relevant section:\n%s", fit)
	}
	if strings.Contains(fit, "Marketing") || strings.Contains(fit, "Footer links") {
		t.Fatalf("fit markdown kept irrelevant boilerplate:\n%s", fit)
	}
	if len(fit) >= len(md) {
		t.Fatalf("fit did not reduce content: %d >= %d", len(fit), len(md))
	}
}

func TestFitMarkdownPassthrough(t *testing.T) {
	if got := FitMarkdown("", "q"); got != "" {
		t.Fatalf("empty md should pass through, got %q", got)
	}
	md := "# T\n\nshort page"
	if got := FitMarkdown(md, ""); got != md {
		t.Fatalf("empty query should pass through, got %q", got)
	}
	if got := FitMarkdown(md, "anything"); got != md {
		t.Fatalf("tiny page should pass through, got %q", got)
	}
}

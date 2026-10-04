package fetch

import (
	"strings"
	"testing"
)

const scopePage = `<html><body>
<nav><a href="/x">NAVJUNK</a></nav>
<article><h1>Real Content</h1><p>the good part</p></article>
<footer>FOOTJUNK</footer>
</body></html>`

func TestScopeInclude(t *testing.T) {
	out, err := applySelectors([]byte(scopePage), []string{"article"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "Real Content") {
		t.Fatalf("include lost the article: %s", s)
	}
	if strings.Contains(s, "NAVJUNK") || strings.Contains(s, "FOOTJUNK") {
		t.Fatalf("include kept surrounding boilerplate: %s", s)
	}
}

func TestScopeExclude(t *testing.T) {
	out, err := applySelectors([]byte(scopePage), nil, []string{"nav", "footer"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "NAVJUNK") || strings.Contains(s, "FOOTJUNK") {
		t.Fatalf("exclude kept boilerplate: %s", s)
	}
	if !strings.Contains(s, "Real Content") {
		t.Fatalf("exclude dropped content: %s", s)
	}
}

func TestScopeIncludeMiss(t *testing.T) {
	_, err := applySelectors([]byte(scopePage), []string{".nope"}, nil)
	if err == nil {
		t.Fatal("include matching nothing should error, not silently return empty")
	}
}

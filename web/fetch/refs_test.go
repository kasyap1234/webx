package fetch

import (
	"strings"
	"testing"
)

func TestStripHTMLComments(t *testing.T) {
	in := "intro\n<!--THE END-->\noutro\n<!-- multi\nline -->\nend"
	got := stripHTMLComments(in)
	if strings.Contains(got, "<!--") || strings.Contains(got, "-->") {
		t.Fatalf("comments survived: %q", got)
	}
	for _, want := range []string{"intro", "outro", "end"} {
		if !strings.Contains(got, want) {
			t.Fatalf("lost %q: %q", want, got)
		}
	}
}

func TestStripHTMLCommentsKeepsFencedCode(t *testing.T) {
	in := "text\n```html\n<!-- a real comment -->\n<p>x</p>\n```\nafter <!--junk-->"
	got := stripHTMLComments(in)
	if !strings.Contains(got, "<!-- a real comment -->") {
		t.Fatalf("fenced comment stripped: %q", got)
	}
	if strings.Contains(got, "junk") {
		t.Fatalf("outside comment survived: %q", got)
	}
}

func TestStripLinkClustersFootnotes(t *testing.T) {
	in := "Go is a language[\\[15\\]](https://en.wikipedia.org#cite_note-15) " +
		"designed at Google[\\[4\\]](https://en.wikipedia.org#cite_note-4 \"Google\")."
	got := stripLinkClusters(in)
	if strings.Contains(got, "cite_note") || strings.Contains(got, "\\[") {
		t.Fatalf("footnote markers survived: %q", got)
	}
	if !strings.Contains(got, "Go is a language") || !strings.Contains(got, "designed at Google") {
		t.Fatalf("prose damaged: %q", got)
	}
}

func TestStripLinkClustersNavSoup(t *testing.T) {
	soup := "[Home](/) [Docs](/docs) [Pricing](/pricing) [Blog](/blog) [Login](/login)"
	in := "real intro paragraph\n\n" + soup + "\n\nclosing text"
	got := stripLinkClusters(in)
	if strings.Contains(got, "Pricing") {
		t.Fatalf("nav soup survived: %q", got)
	}
	for _, want := range []string{"real intro paragraph", "closing text"} {
		if !strings.Contains(got, want) {
			t.Fatalf("lost %q: %q", want, got)
		}
	}
}

func TestStripLinkClustersPreservesListsAndProse(t *testing.T) {
	in := "## Index\n\n- [strings](https://pkg.go.dev/strings)\n- [bytes](https://pkg.go.dev/bytes)\n\n" +
		"See [the docs](https://example.com) for more."
	got := stripLinkClusters(in)
	if !strings.Contains(got, "- [strings]") || !strings.Contains(got, "- [bytes]") {
		t.Fatalf("link list damaged: %q", got)
	}
	if !strings.Contains(got, "See [the docs]") {
		t.Fatalf("inline link in prose damaged: %q", got)
	}
}

func TestStripLinkClustersKeepsFencedCode(t *testing.T) {
	in := "text\n```\n[a](/1) [b](/2) [c](/3) [d](/4) [e](/5)\n```\nafter"
	got := stripLinkClusters(in)
	if !strings.Contains(got, "[a](/1)") {
		t.Fatalf("fenced link soup stripped: %q", got)
	}
}

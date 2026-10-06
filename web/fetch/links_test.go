package fetch

import "testing"

func TestSameOrWWW(t *testing.T) {
	cases := []struct {
		link, host string
		want       bool
	}{
		{"https://go.dev/doc", "go.dev", true},
		{"https://www.go.dev/doc", "go.dev", true},
		{"https://go.dev/doc", "www.go.dev", true},
		{"https://blog.go.dev/x", "go.dev", false}, // subdomain excluded
		{"https://evil.dev/go.dev", "go.dev", false},
		{"not a url", "go.dev", false},
	}
	for _, c := range cases {
		if got := SameOrWWW(c.link, c.host); got != c.want {
			t.Errorf("SameOrWWW(%q,%q) = %v want %v", c.link, c.host, got, c.want)
		}
	}
}

func TestIsPagePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/doc/install", true},
		{"/doc/", true},
		{"/", true},
		{"/paper.pdf", true}, // documents count as pages
		{"/feed.xml", true},
		{"/x.html", true},
		{"/img/logo.svg", false},
		{"/dl/go1.9.darwin-amd64.pkg", false},
		{"/static/app.js", false},
		{"/font.woff2", false},
	}
	for _, c := range cases {
		if got := isPagePath(c.path); got != c.want {
			t.Errorf("isPagePath(%q) = %v want %v", c.path, got, c.want)
		}
	}
}

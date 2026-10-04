package fetch

import (
	"context"
	"testing"
)

const brandPage = `<html><head>
<title>Acme Docs</title>
<meta name="theme-color" content="#ff6600">
<meta name="generator" content="mkdocs">
<meta property="og:site_name" content="Acme">
<link rel="icon" href="/favicon.ico" sizes="32x32">
<link rel="apple-touch-icon" href="/apple.png">
<link rel="manifest" href="/site.webmanifest">
<meta property="og:image" content="/og-cover.png">
</head><body><p>hello</p></body></html>`

func TestExtractBrand(t *testing.T) {
	b := ExtractBrand([]byte(brandPage), "https://docs.acme.io/guide/")
	if b == nil {
		t.Fatal("no brand extracted")
	}
	if b.ThemeColor != "#ff6600" || b.Generator != "mkdocs" || b.Name != "Acme" {
		t.Fatalf("brand meta wrong: %+v", b)
	}
	if len(b.Icons) == 0 || b.Icons[0] != "https://docs.acme.io/favicon.ico" {
		t.Fatalf("relative icon not resolved: %v", b.Icons)
	}
	if b.Manifest != "https://docs.acme.io/site.webmanifest" {
		t.Fatalf("manifest not resolved: %s", b.Manifest)
	}
	if b.Logo != "https://docs.acme.io/og-cover.png" {
		t.Fatalf("og:image should populate logo, got %s", b.Logo)
	}
}

func TestFetchBrandFormat(t *testing.T) {
	srv := serve(t, "text/html; charset=utf-8", brandPage)
	doc, err := Fetch(context.Background(), FetchRequest{URL: srv.URL, WantBrand: true})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Brand == nil || doc.Brand.ThemeColor != "#ff6600" {
		t.Fatalf("brand missing from document: %+v", doc.Brand)
	}
	// Off by default.
	doc2, _ := Fetch(context.Background(), FetchRequest{URL: srv.URL})
	if doc2.Brand != nil {
		t.Fatal("brand should be nil unless requested")
	}
}

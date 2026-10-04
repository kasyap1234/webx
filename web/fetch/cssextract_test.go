package fetch

import (
	"testing"
)

const cssPage = `<html><body>
<h1 id="t">Price List</h1>
<table><tr class="row"><td class="n">Basic</td><td class="p">$10</td></tr>
<tr class="row"><td class="n">Pro</td><td class="p">$25</td></tr></table>
<a class="home" href="/home">Home</a>
<a class="docs" href="/docs">Docs</a>
</body></html>`

func TestExtractCSS(t *testing.T) {
	out, err := ExtractCSS([]byte(cssPage), map[string]any{
		"title": "#t",
		"home":  "a.home@href",
		"links": "a[]@href",
		"rows": map[string]any{
			"selector": "tr.row[]",
			"fields":   map[string]any{"name": ".n", "price": ".p"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["title"] != "Price List" {
		t.Fatalf("title = %v", out["title"])
	}
	if out["home"] != "/home" {
		t.Fatalf("home = %v", out["home"])
	}
	links, _ := out["links"].([]string)
	if len(links) != 2 || links[1] != "/docs" {
		t.Fatalf("links = %v", out["links"])
	}
	rows, _ := out["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows = %v", out["rows"])
	}
	r0, _ := rows[0].(map[string]any)
	if r0["name"] != "Basic" || r0["price"] != "$10" {
		t.Fatalf("row0 = %v", r0)
	}
}

func TestExtractCSSMissing(t *testing.T) {
	out, err := ExtractCSS([]byte(cssPage), map[string]any{"nope": ".doesnotexist"})
	if err != nil {
		t.Fatal(err)
	}
	if out["nope"] != nil {
		t.Fatalf("missing selector should yield null, got %v", out["nope"])
	}
}

func TestExtractCSSBadSelector(t *testing.T) {
	_, err := ExtractCSS([]byte(cssPage), map[string]any{"x": "[[["})
	if err == nil {
		t.Fatal("expected selector compile error")
	}
}

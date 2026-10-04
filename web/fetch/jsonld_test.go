package fetch

import (
	"strings"
	"testing"
)

const ldPage = `<html><head>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"Product","name":"Widget","offers":{"@type":"Offer","price":"9.99","priceCurrency":"USD"}}
</script>
<script type="application/ld+json">
[{"@type":"BreadcrumbList"},{"@type":"Article","headline":"Hello"}]
</script>
</head><body>x</body></html>`

func TestExtractLD(t *testing.T) {
	ents := ExtractLD([]byte(ldPage))
	if len(ents) < 2 {
		t.Fatalf("ExtractLD found %d entities, want ≥2", len(ents))
	}
	prods := LDOfType(ents, "Product")
	if len(prods) != 1 || !strings.Contains(string(prods[0]), `"Widget"`) {
		t.Errorf("LDOfType(Product) = %v", prods)
	}
	arts := LDOfType(ents, "Article")
	if len(arts) != 1 {
		t.Errorf("LDOfType(Article) = %d, want 1", len(arts))
	}
}

func TestExtractLDNone(t *testing.T) {
	if ents := ExtractLD([]byte("<html><body>no ld here</body></html>")); len(ents) != 0 {
		t.Errorf("ExtractLD on plain page returned %d entities", len(ents))
	}
}

func TestLDType(t *testing.T) {
	if got := LDType([]byte(`{"@type":"JobPosting"}`)); got != "jobposting" {
		t.Errorf("LDType = %q", got)
	}
}

func TestRefSummaryAndStrip(t *testing.T) {
	md := "text [a](https://x.com) ![img](https://x.com/i.png) more ![](data:image/png;base64,AAAA)"
	links := []string{"https://x.com"}
	images := []string{"https://x.com/i.png"}
	out := RefSummary(md, links, images)
	if !strings.Contains(out, "https://x.com") || !strings.Contains(out, "https://x.com/i.png") {
		t.Errorf("RefSummary missing refs:\n%s", out)
	}
	stripped := stripDataURIs("![img](data:image/png;base64,QUJDREVGRw==)")
	if strings.Contains(stripped, "data:image") {
		t.Errorf("stripDataURIs left data uri: %q", stripped)
	}
}

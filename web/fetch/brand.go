package fetch

// brand.go — Firecrawl's `branding` format, the deterministic subset: name,
// logo, theme/tile colors, icon set, manifest, generator. All from meta/link
// tags — zero LLM, works on the HTTP tier (rendered pages get it for free).
import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Brand is a site's visual identity summary.
type Brand struct {
	Name       string   `json:"name,omitempty"`
	Logo       string   `json:"logo,omitempty"`        // og:image or largest icon
	ThemeColor string   `json:"theme_color,omitempty"` // meta theme-color
	TileColor  string   `json:"tile_color,omitempty"`  // msapplication-TileColor
	Icons      []string `json:"icons,omitempty"`       // favicon + touch icons
	Manifest   string   `json:"manifest,omitempty"`
	Generator  string   `json:"generator,omitempty"` // meta generator (wp/next/hugo…)
}

var appleIconSizes = regexp.MustCompile(`(?i)(\d+)x\d+`)

// ExtractBrand pulls identity metadata out of an HTML document. baseURL
// resolves relative icon/manifest hrefs.
func ExtractBrand(doc []byte, baseURL string) *Brand {
	root, err := html.Parse(strings.NewReader(string(doc)))
	if err != nil {
		return nil
	}
	base, _ := url.Parse(baseURL)
	b := &Brand{}
	var icons []struct {
		href string
		size int
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[strings.ToLower(a.Key)] = a.Val
			}
			switch n.Data {
			case "meta":
				name := strings.ToLower(attrs["name"])
				prop := strings.ToLower(attrs["property"])
				content := attrs["content"]
				switch {
				case name == "theme-color" && content != "":
					b.ThemeColor = content
				case name == "msapplication-tilecolor" && content != "":
					b.TileColor = content
				case name == "application-name" && content != "":
					if b.Name == "" {
						b.Name = content
					}
				case name == "generator" && content != "":
					b.Generator = content
				case prop == "og:site_name" && content != "" && b.Name == "":
					b.Name = content
				case prop == "og:image" && content != "" && b.Logo == "":
					b.Logo = resolve(base, content)
				}
			case "link":
				rel := strings.ToLower(attrs["rel"])
				href := attrs["href"]
				if href == "" {
					break
				}
				switch {
				case strings.Contains(rel, "icon") && !strings.Contains(rel, "precomposed"):
					sz := 0
					if m := appleIconSizes.FindStringSubmatch(attrs["sizes"]); len(m) > 1 {
						sz, _ = strconv.Atoi(m[1])
					}
					icons = append(icons, struct {
						href string
						size int
					}{resolve(base, href), sz})
				case rel == "manifest":
					b.Manifest = resolve(base, href)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	for _, ic := range icons {
		b.Icons = append(b.Icons, ic.href)
		if b.Logo == "" && ic.size >= 144 {
			b.Logo = ic.href // og:image absent — biggest icon is the logo stand-in
		}
	}
	if b.Logo == "" && len(icons) > 0 {
		b.Logo = icons[len(icons)-1].href
	}
	if b.Name == "" && b.Logo == "" && b.ThemeColor == "" && len(b.Icons) == 0 {
		return nil
	}
	return b
}

func resolve(base *url.URL, href string) string {
	if base == nil {
		return href
	}
	if u, err := base.Parse(href); err == nil {
		return u.String()
	}
	return href
}

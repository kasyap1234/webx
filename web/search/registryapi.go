package search

import (
	"context"
	"fmt"
	"net/url"
)

// registryapi covers package registries — npm and crates.io both expose free
// JSON search. For identifier queries ("express.Router", "tokio spawn") a
// registry hit is a first-class answer: the package page IS the docs entry.
type npmjs struct{}

func (n *npmjs) Name() string { return "npm" }

type npmResponse struct {
	Objects []struct {
		Package struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Links       struct {
				Npm        string `json:"npm"`
				Repository string `json:"repository"`
			} `json:"links"`
			Version string `json:"version"`
		} `json:"package"`
	} `json:"objects"`
}

func (n *npmjs) Search(ctx context.Context, req Request) ([]Result, error) {
	u := "https://registry.npmjs.org/-/v1/search?text=" + url.QueryEscape(req.Query) + "&size=10"
	var out npmResponse
	if err := getJSON(ctx, u, nil, &out); err != nil {
		return nil, fmt.Errorf("npm: %w", err)
	}
	var results []Result
	for _, o := range out.Objects {
		p := o.Package
		link := p.Links.Repository
		if link == "" {
			link = p.Links.Npm
		}
		title := p.Name + " " + p.Version
		results = append(results, Result{
			Title:   title,
			URL:     link,
			Snippet: p.Description,
			Sources: []string{n.Name()},
		})
	}
	return results, nil
}

type cratesio struct{}

func (c *cratesio) Name() string { return "crates" }

type cratesResponse struct {
	Crates []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		MaxVersion    string `json:"max_version"`
		Repository    string `json:"repository"`
		Documentation string `json:"documentation"`
	} `json:"crates"`
}

func (c *cratesio) Search(ctx context.Context, req Request) ([]Result, error) {
	u := "https://crates.io/api/v1/crates?q=" + url.QueryEscape(req.Query) + "&per_page=10"
	var out cratesResponse
	if err := getJSON(ctx, u, nil, &out); err != nil {
		return nil, fmt.Errorf("crates: %w", err)
	}
	var results []Result
	for _, cr := range out.Crates {
		link := cr.Documentation
		if link == "" {
			link = cr.Repository
		}
		if link == "" {
			link = "https://crates.io/crates/" + cr.ID
		}
		name := cr.Name
		if name == "" {
			name = cr.ID
		}
		results = append(results, Result{
			Title:   fmt.Sprintf("%s %s", name, cr.MaxVersion),
			URL:     link,
			Snippet: cr.Description,
			Sources: []string{c.Name()},
		})
	}
	return results, nil
}

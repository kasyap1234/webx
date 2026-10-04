package search

import (
	"context"
	"fmt"

	"github.com/kasyap1234/webx/web/index"
)

// localIndex queries webx's own FTS5 corpus — the "not just a wrapper"
// provider. Results carry Sources=["index"].
type localIndex struct {
	path string
}

func (l *localIndex) Name() string { return "index" }

func (l *localIndex) Search(ctx context.Context, req Request) ([]Result, error) {
	idx, err := index.Open(l.path)
	if err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	defer idx.Close()

	var hits []index.Hit
	if req.Semantic {
		hits, err = idx.QuerySemantic(ctx, req.Query, req.Num)
	} else {
		hits, err = idx.Query(ctx, req.Query, req.Num)
	}
	if err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	results := make([]Result, 0, len(hits))
	for _, h := range hits {
		results = append(results, Result{
			Title:    h.Title,
			URL:      h.URL,
			Snippet:  h.Snippet,
			Sources:  []string{l.Name()},
			RawScore: h.Score, // real bm25 — fused as a normalized content signal
		})
	}
	return results, nil
}

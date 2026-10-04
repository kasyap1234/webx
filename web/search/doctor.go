package search

import (
	"context"
	"fmt"
	"time"

	"github.com/kasyap1234/webx/web/index"
)

// Health is one provider's liveness report — the `webx doctor` row.
type Health struct {
	Name    string
	OK      bool
	Results int
	Latency time.Duration
	Note    string
}

// Diagnose probes every registered provider with a canned query — the
// operational answer to "why did search return nothing". Index is checked
// by file existence + page count rather than a probe.
func Diagnose(ctx context.Context, probe string) []Health {
	if probe == "" {
		probe = "golang http client"
	}
	var hs []Health
	for name, makeP := range registry {
		p := makeP()
		h := Health{Name: name}
		if name == "index" {
			path := index.DefaultPathEnv()
			if !index.Exists(path) {
				h.Note = "no index file — run `webx index <domain>`"
				hs = append(hs, h)
				continue
			}
			idx, err := index.Open(path)
			if err != nil {
				h.Note = err.Error()
				hs = append(hs, h)
				continue
			}
			n, _ := idx.Stats(ctx)
			idx.Close()
			h.OK = n > 0
			h.Results = n
			h.Note = fmt.Sprintf("%d pages indexed", n)
			hs = append(hs, h)
			continue
		}
		start := time.Now()
		res, err := p.Search(ctx, Request{Query: probe, Num: 5})
		h.Latency = time.Since(start).Round(time.Millisecond)
		if err != nil {
			h.Note = err.Error()
		} else {
			h.OK = len(res) > 0
			h.Results = len(res)
			if len(res) == 0 {
				h.Note = "0 results (reachable but empty)"
			}
		}
		hs = append(hs, h)
	}
	return hs
}

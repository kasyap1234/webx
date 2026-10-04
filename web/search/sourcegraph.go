package search

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// sourcegraph searches Sourcegraph's public index (~2M open-source repos)
// via the streaming search API — second code-search provider, covers cases
// where grep.app's Vercel checkpoint blocks direct API access.
type sourcegraph struct{}

func (s *sourcegraph) Name() string { return "sg" }

// sgEvent is one SSE event payload from /api/search/stream.
type sgEvent struct {
	Data []json.RawMessage `json:"data"`
}

type sgMatch struct {
	Type        string   `json:"type"`
	Repository  string   `json:"repository"`
	Path        string   `json:"path"`
	Branches    []string `json:"branches"`
	LineMatches []struct {
		Line string `json:"line"`
	} `json:"lineMatches"`
	ChunkMatches []struct {
		Content string `json:"content"`
	} `json:"chunkMatches"`
}

func (s *sourcegraph) Search(ctx context.Context, req Request) ([]Result, error) {
	q := "context:global " + req.Query
	u := "https://sourcegraph.com/.api/search/stream?q=" + url.QueryEscape(q) + "&v=V3&t=literal"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("User-Agent", searchUserAgent)
	hreq.Header.Set("Accept", "text/event-stream")
	resp, err := httpClient.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("sg: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sg: status %d", resp.StatusCode)
	}

	var results []Result
	limit := req.Num
	if limit <= 0 {
		limit = 10
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 1<<20), 8<<20)
	var event string
	for sc.Scan() && len(results) < limit {
		line := sc.Text()
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
			continue
		}
		if event != "matches" || !strings.HasPrefix(line, "data: ") {
			continue
		}
		var matches []sgMatch
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &matches) != nil {
			continue
		}
		for _, m := range matches {
			if m.Type != "content" && m.Type != "path" {
				continue
			}
			// repository is "github.com/owner/name" — rebuild a blob URL.
			parts := strings.SplitN(m.Repository, "/", 2)
			if len(parts) != 2 {
				continue
			}
			repo := parts[1]
			branch := "HEAD"
			if len(m.Branches) > 0 && m.Branches[0] != "" {
				branch = m.Branches[0]
			}
			snippet := ""
			if len(m.LineMatches) > 0 {
				snippet = strings.TrimSpace(m.LineMatches[0].Line)
			} else if len(m.ChunkMatches) > 0 {
				snippet = strings.TrimSpace(m.ChunkMatches[0].Content)
			}
			if len(snippet) > 300 {
				snippet = snippet[:300] + "…"
			}
			results = append(results, Result{
				Title:   fmt.Sprintf("%s — %s", repo, m.Path),
				URL:     fmt.Sprintf("https://github.com/%s/blob/%s/%s", repo, branch, m.Path),
				Snippet: snippet,
				Sources: []string{s.Name()},
			})
			if len(results) >= limit {
				break
			}
		}
	}
	return results, sc.Err()
}

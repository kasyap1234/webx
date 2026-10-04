package fetch

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// llmsTxtCache remembers which hosts publish /llms.txt — one probe per host.
var llmsTxtCache sync.Map

// hasLLMSTxt reports whether the host publishes an llms.txt file — the
// site-curated machine-readable manifest of its best LLM content.
func hasLLMSTxt(ctx context.Context, u *url.URL) bool {
	key := u.Scheme + "://" + u.Host
	if v, ok := llmsTxtCache.Load(key); ok {
		return v.(bool)
	}
	ok := probeLLMSTxt(ctx, key)
	llmsTxtCache.Store(key, ok)
	return ok
}

func probeLLMSTxt(ctx context.Context, hostBase string) bool {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hostBase+"/llms.txt", nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	signBotRequest(req)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

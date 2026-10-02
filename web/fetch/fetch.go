package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
)

type FetchRequest struct {
	URL string
}

type Document struct {
	URL        string `json:"url"`
	FinalURL   string `json:"final_url"`
	StatusCode int    `json:"status_code"`
	Title      string `json:"title"`
	Markdown   string `json:"markdown"`
}

const maxBodyBytes = 10 << 20 // 10MiB

func Fetch(ctx context.Context, freq FetchRequest) (*Document, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, freq.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", freq.URL, err)
	}
	httpReq.Header.Set("User-Agent", "webx/0.1")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", freq.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", freq.URL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: read body: %w", freq.URL, err)
	}
	if int64(len(body)) > maxBodyBytes {
		return nil, fmt.Errorf("fetch %s: body exceeds %d bytes", freq.URL, maxBodyBytes)
	}

	finalURL := resp.Request.URL
	domain := finalURL.Scheme + "://" + finalURL.Host
	markdown, err := htmltomarkdown.ConvertString(string(body), converter.WithDomain(domain))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: convert to markdown: %w", freq.URL, err)
	}

	return &Document{
		URL:        freq.URL,
		FinalURL:   finalURL.String(),
		StatusCode: resp.StatusCode,
		Markdown:   markdown,
	}, nil
}

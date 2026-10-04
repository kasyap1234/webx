package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kasyap1234/webx/web/fetch"
)

// TestV2FormatsObject — Firecrawl v2 clients send formats as objects, not
// just strings. The screenshot object must reach the fetch request.
func TestV2FormatsObject(t *testing.T) {
	var got fetch.FetchRequest
	stubFetch(t, func(_ context.Context, r fetch.FetchRequest) (*fetch.Document, error) {
		got = r
		return &fetch.Document{Markdown: "hi", FinalURL: r.URL}, nil
	})
	srv, _ := testServer(t)
	code, out := post(t, srv.URL+"/v2/scrape", `{"url":"https://x.example",
		"formats":["markdown",{"type":"screenshot","fullPage":false,"quality":70,
			"viewport":{"width":800,"height":600}}]}`)
	if code != 200 || out["success"] != true {
		t.Fatalf("got %d %v", code, out)
	}
	if !got.Screenshot || got.ShotQuality != 70 ||
		got.ViewportW != 800 || got.ViewportH != 600 {
		t.Fatalf("screenshot opts not forwarded: %+v", got)
	}
	if got.ShotFullPage == nil || *got.ShotFullPage {
		t.Fatalf("fullPage should be explicit false, got %v", got.ShotFullPage)
	}
	if !got.Render {
		t.Fatal("screenshot must force render escalation")
	}
}

// TestV2FormatsStrings — plain string formats keep working.
func TestV2FormatsStrings(t *testing.T) {
	stubFetch(t, func(_ context.Context, r fetch.FetchRequest) (*fetch.Document, error) {
		return &fetch.Document{Markdown: "hi", Links: []string{"a"}, FinalURL: r.URL}, nil
	})
	srv, _ := testServer(t)
	code, out := post(t, srv.URL+"/v2/scrape", `{"url":"https://x.example","formats":["markdown","links"]}`)
	if code != 200 || out["success"] != true {
		t.Fatalf("got %d %v", code, out)
	}
	data, _ := out["data"].(map[string]any)
	if data["markdown"] != "hi" {
		t.Fatalf("missing markdown: %v", data)
	}
}

// TestV2AgentJob — POST /v2/agent must enqueue and expose the Firecrawl
// status shape at /v2/agent/{id}.
func TestV2AgentJob(t *testing.T) {
	srv, _ := testServer(t)
	code, out := post(t, srv.URL+"/v2/agent", `{"prompt":"find pricing","urls":["https://x.example/pricing"]}`)
	if code != 200 || out["success"] != true || out["id"] == "" {
		t.Fatalf("submit: %d %v", code, out)
	}
	if out["statusUrl"] != "/v2/agent/"+out["id"].(string) {
		t.Fatalf("statusUrl: %v", out["statusUrl"])
	}
	resp, err := http.Get(srv.URL + out["statusUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st["status"] != "processing" || st["success"] != true {
		t.Fatalf("status: %v", st)
	}
	if st["expiresAt"] == nil || st["expiresAt"] == "" {
		t.Fatal("missing expiresAt")
	}
}

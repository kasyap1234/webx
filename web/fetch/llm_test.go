package fetch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStructuredUnsupported(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"openai style", `{"error":{"message":"'response_format.type' must be 'json_schema'"}}`, true},
		{"openrouter wrapped", `{"error":{"message":"Provider returned error","metadata":{"raw":"does not support feature: structured-outputs"}}}`, true},
		{"unrelated 400", `{"error":{"message":"model not found"}}`, false},
		{"empty", ``, false},
	}
	for _, c := range cases {
		if got := structuredUnsupported([]byte(c.body)); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestLLMMaxTokens(t *testing.T) {
	if got := llmMaxTokens(); got != 4096 {
		t.Fatalf("default: got %d want 4096", got)
	}
	t.Setenv("WEBX_LLM_MAX_TOKENS", "512")
	if got := llmMaxTokens(); got != 512 {
		t.Fatalf("env override: got %d want 512", got)
	}
	t.Setenv("WEBX_LLM_MAX_TOKENS", "bogus")
	if got := llmMaxTokens(); got != 4096 {
		t.Fatalf("invalid env falls back: got %d want 4096", got)
	}
	t.Setenv("WEBX_LLM_MAX_TOKENS", "-5")
	if got := llmMaxTokens(); got != 4096 {
		t.Fatalf("negative env falls back: got %d want 4096", got)
	}
}

func TestLLMTimeout(t *testing.T) {
	if got := llmTimeout(); got != 120*time.Second {
		t.Fatalf("default: got %s want 120s", got)
	}
	t.Setenv("WEBX_LLM_TIMEOUT", "600")
	if got := llmTimeout(); got != 600*time.Second {
		t.Fatalf("env override: got %s want 600s", got)
	}
	t.Setenv("WEBX_LLM_TIMEOUT", "0")
	if got := llmTimeout(); got != 120*time.Second {
		t.Fatalf("zero env falls back: got %s want 120s", got)
	}
}

func TestLLMHealthModelReady(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "test-model"}},
		})
	}))
	defer srv.Close()
	t.Setenv("WEBX_LLM_BASE", srv.URL)
	t.Setenv("WEBX_LLM_MODEL", "test-model")
	ok, _, note := LLMHealth(context.Background())
	if !ok || note != "model ready" {
		t.Fatalf("got ok=%v note=%q want model ready", ok, note)
	}
}

func TestLLMHealthModelMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"id": "other-model"}},
		})
	}))
	defer srv.Close()
	t.Setenv("WEBX_LLM_BASE", srv.URL)
	t.Setenv("WEBX_LLM_MODEL", "test-model")
	ok, _, note := LLMHealth(context.Background())
	if ok || !strings.Contains(note, "not listed") {
		t.Fatalf("got ok=%v note=%q want model-not-listed", ok, note)
	}
}

func TestLLMHealthUnreachable(t *testing.T) {
	t.Setenv("WEBX_LLM_BASE", "http://127.0.0.1:1")
	ok, _, note := LLMHealth(context.Background())
	if ok || !strings.Contains(note, "unreachable") {
		t.Fatalf("got ok=%v note=%q want unreachable", ok, note)
	}
}

func TestLLMChatRetries429ThenSucceeds(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "ok"}},
			},
		})
	}))
	defer srv.Close()
	t.Setenv("WEBX_LLM_BASE", srv.URL)
	out, err := LLMChat(context.Background(), "sys", "user", false)
	if err != nil || out != "ok" {
		t.Fatalf("got out=%q err=%v", out, err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
}

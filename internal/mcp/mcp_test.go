package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func rpc(t *testing.T, method string, params any) *rpcRequest {
	t.Helper()
	var p json.RawMessage
	if params != nil {
		p, _ = json.Marshal(params)
	}
	return &rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: method, Params: p}
}

func TestDispatchInitialize(t *testing.T) {
	resp := dispatch(context.Background(), rpc(t, "initialize", nil))
	if resp == nil || resp.Error != nil {
		t.Fatalf("initialize resp = %+v", resp)
	}
	m, ok := resp.Result.(map[string]any)
	if !ok || m["protocolVersion"] == "" {
		t.Fatalf("bad initialize result %+v", resp.Result)
	}
}

func TestDispatchToolsList(t *testing.T) {
	resp := dispatch(context.Background(), rpc(t, "tools/list", nil))
	if resp.Error != nil {
		t.Fatal(resp.Error)
	}
	m := resp.Result.(map[string]any)
	tools, ok := m["tools"].([]map[string]any)
	if !ok || len(tools) < 15 {
		t.Fatalf("tools/list returned %d tools", len(tools))
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl["name"].(string)] = true
	}
	for _, want := range []string{
		"webx_search", "webx_scrape", "webx_ask", "webx_query", "webx_map",
		"webx_crawl", "webx_extract", "webx_research", "webx_verify",
		"webx_similar", "webx_wayback", "webx_llms", "webx_answer", "webx_batch",
	} {
		if !names[want] {
			t.Fatalf("missing tool %s", want)
		}
	}
}

func TestDispatchNotificationsReturnNil(t *testing.T) {
	// Notifications (no id) must not produce a response — spec 2025-03-26.
	req := &rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}
	if resp := dispatch(context.Background(), req); resp != nil {
		t.Fatalf("notification produced %+v", resp)
	}
}

func TestDispatchUnknownMethod(t *testing.T) {
	resp := dispatch(context.Background(), rpc(t, "bogus/method", nil))
	if resp == nil || resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("unknown method resp = %+v", resp)
	}
}

func TestDispatchBadToolCallParams(t *testing.T) {
	req := rpc(t, "tools/call", nil)
	req.Params = json.RawMessage(`{bad`)
	resp := dispatch(context.Background(), req)
	if resp == nil || resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("bad params resp = %+v", resp)
	}
}

func TestCallToolUnknown(t *testing.T) {
	res := callTool(context.Background(), "webx_nonexistent", json.RawMessage(`{}`))
	m, ok := res.(map[string]any)
	if !ok || m["isError"] != true {
		t.Fatalf("unknown tool result = %+v", res)
	}
}

func TestCallToolMissingArg(t *testing.T) {
	res := callTool(context.Background(), "webx_search", json.RawMessage(`{}`))
	m := res.(map[string]any)
	if m["isError"] != true {
		t.Fatal("missing query should be isError")
	}
}

// ── streamable HTTP transport ────────────────────────────────────────────

func TestHTTPPostSingle(t *testing.T) {
	srv := httptest.NewServer(StreamableHandler())
	defer srv.Close()
	body := `{"jsonrpc":"2.0","id":7,"method":"ping"}`
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Error != nil {
		t.Fatalf("ping error %+v", out.Error)
	}
}

func TestHTTPPostNotification(t *testing.T) {
	srv := httptest.NewServer(StreamableHandler())
	defer srv.Close()
	// No id → 202 with no body, per the spec's notification rule.
	body := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("notification status %d, want 202", resp.StatusCode)
	}
}

func TestHTTPPostBatch(t *testing.T) {
	srv := httptest.NewServer(StreamableHandler())
	defer srv.Close()
	body := `[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("batch got %d responses", len(out))
	}
}

func TestHTTPPostParseError(t *testing.T) {
	srv := httptest.NewServer(StreamableHandler())
	defer srv.Close()
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{not json`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("parse error status %d", resp.StatusCode)
	}
}

func TestHTTPDeleteSession(t *testing.T) {
	srv := httptest.NewServer(StreamableHandler())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodDelete, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("DELETE status %d", resp.StatusCode)
	}
}

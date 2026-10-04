package mcp

// http.go — the MCP "Streamable HTTP" transport (protocol 2025-03-26):
// one endpoint, POST carries JSON-RPC messages and gets a JSON response
// (or 202 for notifications), GET opens an SSE stream for
// server-initiated traffic, DELETE ends the session.
//
// webx tools are stateless, so there are no server-initiated messages:
// the GET stream exists for spec completeness — clients that insist on
// it get heartbeat comments, nothing more.
import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// StreamableHandler serves the MCP streamable HTTP transport. Mount at
// whatever path you like (convention: /mcp).
func StreamableHandler() http.Handler {
	return http.HandlerFunc(handleStreamable)
}

func handleStreamable(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		handlePost(w, r)
	case http.MethodGet:
		handleStream(w, r)
	case http.MethodDelete:
		// Stateless server: nothing to tear down — acknowledge and close.
		w.WriteHeader(http.StatusOK)
	default:
		w.Header().Set("Allow", "POST, GET, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePost runs a single JSON-RPC message (or a batch) through the same
// dispatch the stdio transport uses. Per spec we answer application/json —
// upgrading to an SSE response is only required when the server emits
// mid-request messages, which webx's synchronous tools never do.
func handlePost(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if ct != "" && ct != "application/json" {
		http.Error(w, "content-type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	body := http.MaxBytesReader(w, r.Body, 4<<20)
	defer body.Close()
	var raw json.RawMessage
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}
	// Batch arrays are legal JSON-RPC — answer each element.
	var reqs []rpcRequest
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &reqs); err != nil {
			writeRPCError(w, nil, -32700, "parse error")
			return
		}
	} else {
		var req rpcRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			writeRPCError(w, nil, -32700, "parse error")
			return
		}
		reqs = []rpcRequest{req}
	}

	var resps []rpcResponse
	notifications := 0
	for i := range reqs {
		if resp := dispatch(r.Context(), &reqs[i]); resp != nil {
			resps = append(resps, *resp)
		} else {
			notifications++
		}
	}
	// Pure notifications → 202, nothing to report.
	if len(resps) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if len(raw) > 0 && raw[0] == '[' {
		_ = json.NewEncoder(w).Encode(resps)
		return
	}
	_ = json.NewEncoder(w).Encode(resps[0])
}

// handleStream is the optional SSE channel for server→client traffic.
// webx has none — emit heartbeat comments so clients holding the stream
// don't time out, and let the request die with the connection.
func handleStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			fl.Flush()
		}
	}
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	if id == nil {
		id = json.RawMessage("null")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(rpcResponse{
		JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg},
	})
}

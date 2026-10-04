package search

import (
	"context"
	"testing"
)

// A panicking provider must surface as an error — the fan-out goroutine
// recovering is what keeps `webx serve` alive when an upstream or transport
// misbehaves (found by a real SIGSEGV during a 1300-query eval run).
type panicProvider struct{}

func (panicProvider) Name() string { return "panictest" }
func (panicProvider) Search(ctx context.Context, r Request) ([]Result, error) {
	panic("simulated provider crash")
}

func TestProviderPanicIsolated(t *testing.T) {
	registry["panictest"] = func() Provider { return panicProvider{} }
	defer delete(registry, "panictest")
	resp := Search(context.Background(), Request{Query: "x", Providers: []string{"panictest"}})
	if resp.Errors["panictest"] == "" {
		t.Fatal("panic should surface as provider error")
	}
}

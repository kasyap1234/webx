package fetch

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestWriteWARCResponse(t *testing.T) {
	var buf bytes.Buffer
	h := http.Header{"Content-Type": {"text/html"}, "X-Test": {"yes"}}
	body := []byte("<html><body>hello</body></html>")
	if err := WriteWARCResponse(&buf, "https://x.test/", 200, h, body); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"WARC/1.0", "WARC-Type: response", "WARC-Target-URI: https://x.test/",
		"WARC-Record-ID: <urn:uuid:", "application/http; msgtype=response",
		"HTTP/1.1 200 OK", "X-Test: yes", "hello",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("warc missing %q", want)
		}
	}
	// Content-Length must cover the recomputed body.
	want := fmt.Sprintf("Content-Length: %d\r\n", len(body))
	if !strings.Contains(out, want) {
		t.Errorf("inner Content-Length should be %q", want)
	}
}

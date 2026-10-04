package fetch

import (
	"strings"
	"testing"
)

func TestRedactPII(t *testing.T) {
	in := "Contact jane.doe@acme.com or +1 (415) 555-2671. SSN 078-05-1120. " +
		"Card 4111 1111 1111 1111. Server at 192.168.1.10. Date 2024-01-15."
	out := RedactPII(in)
	for _, tok := range []string{"jane.doe@acme.com", "415) 555-2671", "078-05-1120", "4111 1111 1111 1111", "192.168.1.10"} {
		if strings.Contains(out, tok) {
			t.Fatalf("PII survived redaction: %s", tok)
		}
	}
	for _, mask := range []string{"[email]", "[phone]", "[ssn]", "[card]", "[ip]"} {
		if !strings.Contains(out, mask) {
			t.Fatalf("missing mask %s in %q", mask, out)
		}
	}
	// Non-PII survives untouched.
	if !strings.Contains(out, "2024-01-15") {
		t.Fatal("date redacted as PII")
	}
}

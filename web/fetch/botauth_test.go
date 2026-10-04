package fetch

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBotKeyRoundTripAndSign(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "botkey.json")
	jwks, kid, err := GenerateBotKey(keyPath, "https://demo.example/.well-known/dir")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jwks, `"kid":"`+kid+`"`) || !strings.Contains(jwks, `"kty":"OKP"`) {
		t.Fatalf("jwks malformed: %s", jwks)
	}
	st, _ := os.Stat(keyPath)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o", st.Mode().Perm())
	}
	if k2, err := BotKeyKid(keyPath); err != nil || k2 != kid {
		t.Fatalf("kid roundtrip: %q %v", k2, err)
	}
	t.Setenv("WEBX_BOT_KEY", keyPath)
	t.Setenv("WEBX_BOT_DIRECTORY", "https://demo.example/.well-known/dir")
	req, _ := http.NewRequest("GET", "https://example.com/page", nil)
	signBotRequest(req)
	if !strings.HasPrefix(req.Header.Get("Signature"), "sig1=:") {
		t.Fatalf("missing signature: %v", req.Header)
	}
	if !strings.Contains(req.Header.Get("Signature-Input"), `tag="web-bot-auth"`) {
		t.Fatalf("missing tag param: %s", req.Header.Get("Signature-Input"))
	}
	if req.Header.Get("Signature-Agent") != "https://demo.example/.well-known/dir" {
		t.Fatalf("signature-agent = %s", req.Header.Get("Signature-Agent"))
	}
}

func TestSignNoConfig(t *testing.T) {
	t.Setenv("WEBX_BOT_KEY", "")
	t.Setenv("WEBX_BOT_DIRECTORY", "")
	req, _ := http.NewRequest("GET", "https://example.com/", nil)
	signBotRequest(req)
	if req.Header.Get("Signature") != "" {
		t.Fatal("signed without configuration")
	}
}

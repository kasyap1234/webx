package fetch

// botauth.go — Web Bot Auth (draft-meunier-webbotauth-httpsig-protocol):
// signing outbound requests with an ed25519 key so sites can *verify* the
// agent instead of fingerprint-blocking it. The anti-stealth: we identify
// honestly, cryptographically. WEBX_BOT_KEY points at a key file written by
// `webx botkey`; WEBX_BOT_DIRECTORY is the https URL where the operator
// serves their /.well-known/http-message-signatures-directory JWKS.
// Scope note: signatures cover the HTTP/TLS fetch tiers — CDP-rendered
// pages go through the browser's own network stack and can't be signed.
import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// botKeyFile is the on-disk format `webx botkey` writes (mode 0600).
type botKeyFile struct {
	Kty       string `json:"kty"`                 // OKP
	Crv       string `json:"crv"`                 // Ed25519
	D         string `json:"d"`                   // base64url private seed — KEEP SECRET
	X         string `json:"x"`                   // base64url public key
	Kid       string `json:"kid"`                 // JWK thumbprint (RFC 7638) — the keyid verifiers look up
	Directory string `json:"directory,omitempty"` // default directory URL for convenience
}

type botSigner struct {
	priv      ed25519.PrivateKey
	kid       string
	directory string
}

var (
	signerMu  sync.Mutex
	signer    *botSigner
	signerEnv string // the WEBX_BOT_KEY|WEBX_BOT_DIRECTORY pair we loaded
	signerErr error
)

// botSignerLoaded caches the env-configured signer, keyed on the env pair —
// reloads when the configuration changes (tests, late env setup).
func botSignerLoaded() *botSigner {
	keyPath, dir := os.Getenv("WEBX_BOT_KEY"), os.Getenv("WEBX_BOT_DIRECTORY")
	signerMu.Lock()
	defer signerMu.Unlock()
	if keyPath+"|"+dir == signerEnv {
		return signer
	}
	signerEnv, signer, signerErr = keyPath+"|"+dir, nil, nil
	if keyPath == "" {
		return nil
	}
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		signerErr = err
		return nil
	}
	var kf botKeyFile
	if err := json.Unmarshal(raw, &kf); err != nil {
		signerErr = fmt.Errorf("WEBX_BOT_KEY %s: %w", keyPath, err)
		return nil
	}
	seed, err := base64.RawURLEncoding.DecodeString(kf.D)
	if err != nil || len(seed) != ed25519.SeedSize {
		signerErr = fmt.Errorf("WEBX_BOT_KEY %s: invalid seed", keyPath)
		return nil
	}
	if dir == "" {
		dir = kf.Directory
	}
	if dir == "" {
		signerErr = fmt.Errorf("WEBX_BOT_DIRECTORY unset — the https URL where your JWKS is hosted")
		return nil
	}
	signer = &botSigner{priv: ed25519.NewKeyFromSeed(seed), kid: kf.Kid, directory: dir}
	return signer
}

// signBotRequest adds Signature-Agent/Signature-Input/Signature headers per
// draft-meunier-webbotauth-httpsig-protocol. Covered components are the
// minimal honest set the draft mandates: "@authority" + "signature-agent".
// No-op when signing isn't configured.
func signBotRequest(req *http.Request) {
	s := botSignerLoaded()
	if s == nil {
		return
	}
	now := time.Now().Unix()
	expires := now + 300 // 5-minute window — nonce + expiry bound replay
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	nonceB64 := base64.RawURLEncoding.EncodeToString(nonce[:])

	params := fmt.Sprintf(`("@authority" "signature-agent");created=%d;expires=%d;nonce="%s";keyid="%s";alg="ed25519";tag="web-bot-auth"`,
		now, expires, nonceB64, s.kid)
	base := "\"@authority\": " + req.URL.Host +
		"\n\"signature-agent\": " + s.directory +
		"\n\"@signature-params\": " + params
	sig := ed25519.Sign(s.priv, []byte(base))

	req.Header.Set("Signature-Agent", s.directory)
	req.Header.Set("Signature-Input", "sig1="+params)
	req.Header.Set("Signature", "sig1=:"+base64.StdEncoding.EncodeToString(sig)+":")
}

// ── key generation (webx botkey) ─────────────────────────────────────────────

// BotKeyPath is the default key location.
func BotKeyPath() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".webx", "botkey.json")
	}
	return "botkey.json"
}

// jwkThumbprint computes the RFC 7638 SHA-256 thumbprint of the public JWK —
// the keyid a /.well-known directory publishes and verifiers resolve.
func jwkThumbprint(x string) (string, error) {
	canonical := `{"crv":"Ed25519","kty":"OKP","x":"` + x + `"}`
	sum := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// GenerateBotKey creates a fresh ed25519 keypair, writes it mode 0600, and
// returns the JWKS the operator hosts at their directory URL.
func GenerateBotKey(path, directory string) (jwks string, kid string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	x := base64.RawURLEncoding.EncodeToString(pub)
	kid, err = jwkThumbprint(x)
	if err != nil {
		return "", "", err
	}
	kf := botKeyFile{
		Kty: "OKP", Crv: "Ed25519",
		D: base64.RawURLEncoding.EncodeToString(priv.Seed()),
		X: x, Kid: kid, Directory: directory,
	}
	raw, _ := json.MarshalIndent(kf, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", "", err
	}
	jwks = `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"` + x + `","kid":"` + kid + `","alg":"Ed25519"}]}`
	return jwks, kid, nil
}

// BotKeyKid reads a key file and reports its thumbprint (for display).
func BotKeyKid(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var kf botKeyFile
	if err := json.Unmarshal(raw, &kf); err != nil {
		return "", err
	}
	return kf.Kid, nil
}

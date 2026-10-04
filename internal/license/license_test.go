package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

// withPubKey points the lazy verifier at a throwaway test key — the env
// override is read at Parse time, so Setenv alone does it.
func withPubKey(t *testing.T, pub ed25519.PublicKey) {
	t.Helper()
	t.Setenv("WEBX_LICENSE_PUBKEY", base64.StdEncoding.EncodeToString(pub))
}

func TestRoundTrip(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	withPubKey(t, pub)

	l := &License{
		ID: "lic_test", Email: "a@b.c", Tier: TierPro,
		Issued:  time.Now().UTC().Format(time.RFC3339),
		Expires: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	}
	if err := Sign(priv, l); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(l)
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Tier != TierPro || !got.Allows(FeatMultiKey) || !got.Allows(FeatAudit) {
		t.Fatalf("features: %+v", got)
	}
	if got.Allows(FeatZDR) {
		t.Fatal("pro must not allow zdr — that's enterprise")
	}
}

func TestTamperedLicenseRejected(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	withPubKey(t, pub)

	l := &License{
		ID: "lic_t", Tier: TierPro,
		Issued:  time.Now().UTC().Format(time.RFC3339),
		Expires: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	}
	_ = Sign(priv, l)
	l.Tier = TierEnterprise // tamper after signing
	raw, _ := json.Marshal(l)
	if _, err := Parse(raw); err == nil {
		t.Fatal("tampered license verified")
	}
}

func TestExpiredRejected(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	withPubKey(t, pub)
	l := &License{
		ID: "lic_x", Tier: TierPro,
		Issued:  time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		Expires: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	}
	_ = Sign(priv, l)
	raw, _ := json.Marshal(l)
	if _, err := Parse(raw); err == nil {
		t.Fatal("expired license verified")
	}
}

func TestNilLicenseFreeTier(t *testing.T) {
	var l *License
	if l.Allows(FeatAudit) || l.TierName() != TierFree {
		t.Fatal("nil license must be free tier with nothing gated")
	}
}

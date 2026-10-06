// Package license implements signed license files — the open-core half of
// webx's monetization story. Files are JSON claims + an Ed25519 signature
// over the canonical claims; the verifier's public key is embedded, the
// signing key lives with the maintainer. This is trust licensing, not DRM —
// MIT code means anyone can patch the check; the license is what honest
// customers buy (and what support attaches to).
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Tiers — ordered; higher includes lower.
const (
	TierFree       = "free"
	TierPro        = "pro"
	TierEnterprise = "enterprise"
)

// Features a license can unlock. Free tier gets: metering, ≤3 API keys,
// schedules, global concurrency. Paid adds the multi-tenant controls.
const (
	FeatMultiKey  = "multikey"  // >3 API keys (Dagu's gate — key count is the lever)
	FeatRBAC      = "rbac"      // key roles: admin can manage keys/schedules
	FeatAudit     = "audit"     // per-request audit log
	FeatZDR       = "zdr"       // zero-data-retention mode (no cache/job persistence)
	FeatPriorityQ = "priorityq" // priority queue for scheduled jobs
)

var tierFeatures = map[string][]string{
	TierPro:        {FeatMultiKey, FeatRBAC, FeatAudit},
	TierEnterprise: {FeatMultiKey, FeatRBAC, FeatAudit, FeatZDR, FeatPriorityQ},
}

// License is a signed grant.
type License struct {
	ID       string   `json:"id"`
	Email    string   `json:"email"`
	Tier     string   `json:"tier"`
	Issued   string   `json:"issued"`  // RFC3339
	Expires  string   `json:"expires"` // RFC3339
	Features []string `json:"features,omitempty"`
	Sig      string   `json:"sig"` // base64 ed25519 over canonical(claims)
}

// claimsView is the signed payload — everything except Sig, canonically ordered.
func (l *License) claimsView() []byte {
	c := struct {
		ID, Email, Tier, Issued, Expires string
		Features                         []string
	}{l.ID, l.Email, l.Tier, l.Issued, l.Expires, l.Features}
	b, _ := json.Marshal(c)
	return b
}

// MaxKeys is the free tier's API-key cap — the same "2 API keys free"
// lever Dagu uses.
const FreeMaxKeys = 3

// pubKey resolves the verifier lazily — env override must be readable at
// Parse time, not package-init time, or WEBX_LICENSE_PUBKEY can't be set
// by tests/in-process embedders that missed init.
var pubKey = resolvePubKey

// resolvePubKey reads WEBX_LICENSE_PUBKEY, falling back to the embedded key.
func resolvePubKey() ed25519.PublicKey {
	if v := os.Getenv("WEBX_LICENSE_PUBKEY"); v != "" {
		if b, err := base64.StdEncoding.DecodeString(v); err == nil && len(b) == ed25519.PublicKeySize {
			return b
		}
	}
	b, _ := base64.StdEncoding.DecodeString(embeddedPubKey)
	return b
}

// embeddedPubKey is filled in by `webx license keygen` output pasted here.
// Empty until the maintainer commits a real key — without it, generated
// licenses verify only under WEBX_LICENSE_PUBKEY (dev mode).
const embeddedPubKey = "vEyGY7UlNU7Ci39kY14m67qNiY7P52NOUJmRvRdS1es="

// Load reads and verifies a license file. Invalid/missing → (nil, err).
func Load(path string) (*License, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// LoadEnv resolves the license from WEBX_LICENSE (path or inline JSON),
// falling back to the well-known path ~/.webx/license.json that
// `webx license install` writes. No license → (nil, nil) — free tier.
func LoadEnv() (*License, error) {
	if v := os.Getenv("WEBX_LICENSE"); v != "" {
		if st, err := os.Stat(v); err == nil && !st.IsDir() {
			return Load(v)
		}
		return Parse([]byte(v))
	}
	if p := DefaultPath(); p != "" {
		if _, err := os.Stat(p); err == nil {
			return Load(p)
		}
	}
	return nil, nil
}

// DefaultPath is where `webx license install` stores a verified license.
func DefaultPath() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(h, ".webx", "license.json")
}

// Parse verifies signature + expiry.
func Parse(raw []byte) (*License, error) {
	var l License
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("license: invalid json: %w", err)
	}
	pub := pubKey()
	if len(pub) == 0 {
		return nil, errors.New("license: no verification key configured (set WEBX_LICENSE_PUBKEY)")
	}
	sig, err := base64.StdEncoding.DecodeString(l.Sig)
	if err != nil || !ed25519.Verify(pub, l.claimsView(), sig) {
		return nil, errors.New("license: invalid signature")
	}
	if l.Tier != TierPro && l.Tier != TierEnterprise {
		return nil, fmt.Errorf("license: unknown tier %q", l.Tier)
	}
	if exp, err := time.Parse(time.RFC3339, l.Expires); err == nil && time.Now().After(exp) {
		return nil, fmt.Errorf("license: expired %s", l.Expires)
	}
	return &l, nil
}

// Sign produces a license — requires the maintainer's private key.
func Sign(priv ed25519.PrivateKey, l *License) error {
	if l.Features == nil {
		l.Features = tierFeatures[l.Tier]
	}
	l.Sig = ""
	l.Sig = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, l.claimsView()))
	return nil
}

// Allows reports whether a feature is unlocked by this license.
// nil license → free tier (nothing gated allowed).
func (l *License) Allows(feat string) bool {
	if l == nil {
		return false
	}
	for _, f := range l.Features {
		if f == feat {
			return true
		}
	}
	return false
}

// Tier returns the license tier, TierFree when nil.
func (l *License) TierName() string {
	if l == nil {
		return TierFree
	}
	return l.Tier
}

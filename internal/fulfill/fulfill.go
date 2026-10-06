// Package fulfill turns payment-provider webhooks into signed webx license
// files and delivers them to customers. It's the maintainer-side half of the
// purchase flow: a checkout link (Polar.sh or Stripe) → webhook → signed
// license → email. The customer's webx binary already knows how to verify the
// signature; this package just automates `webx license gen` + delivery.
//
// Standard library only — the service runs unattended next to the maintainer
// key, so the dependency surface stays deliberately thin.
package fulfill

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kasyap1234/webx/internal/license"
	"github.com/kasyap1234/webx/internal/store"
)

// sigTolerance is the Standard-Webhooks/Stripe timestamp window.
const sigTolerance = 5 * time.Minute

// Config is sourced from env in cmd/webxl.
type Config struct {
	// SigningKeyPath — maintainer ed25519 private key (WEBX_MAINTAINER_KEY).
	SigningKeyPath string
	// PolarSecret — webhook secret (POLAR_WEBHOOK_SECRET; Standard Webhooks,
	// optional whsec_-style prefix handled).
	PolarSecret string
	// StripeSecret — webhook endpoint secret (STRIPE_WEBHOOK_SECRET, whsec_…).
	StripeSecret string
	// StripeAPIKey — sk_… for fetching checkout line items (STRIPE_SECRET_KEY).
	StripeAPIKey string
	// ResendKey — email delivery (RESEND_API_KEY). Empty → mailbox files.
	ResendKey string
	// From — RFC-822 sender (LICENSE_FROM, e.g. "webx licenses <licenses@x.dev>").
	From string
	// MailboxDir — fallback spool: license + a .txt note per order for manual
	// sending when Resend isn't configured (default ~/.webx/mailbox).
	MailboxDir string
	// Products maps provider product/price id → tier+validity
	// (POLAR_TIERS / STRIPE_TIERS, e.g. "prod_abc:pro:35,prod_xyz:enterprise:400").
	Products map[string]ProductRule
	// HTTPClient is injectable for tests.
	HTTPClient *http.Client
}

// ProductRule binds a provider product/price id to a license tier + validity.
type ProductRule struct {
	Tier string
	Days int
}

// ParseProductMap parses "id:tier[:days],id2:tier" — missing days →
// per-tier default (pro 35 ≈ monthly renewal buffer, enterprise 400 ≈ annual).
func ParseProductMap(s string) map[string]ProductRule {
	m := map[string]ProductRule{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		f := strings.SplitN(part, ":", 3)
		if len(f) < 2 {
			continue
		}
		r := ProductRule{Tier: f[1]}
		if len(f) == 3 {
			r.Days, _ = strconv.Atoi(f[2])
		}
		if r.Days == 0 {
			r.Days = map[string]int{license.TierPro: 35, license.TierEnterprise: 400}[r.Tier]
		}
		m[f[0]] = r
	}
	return m
}

// ── Signature verification ──────────────────────────────────────────────

// VerifyPolar checks a Standard Webhooks delivery (webhook-id,
// webhook-timestamp, webhook-signature; HMAC-SHA256 base64 over
// "{id}.{ts}.{body}").
func VerifyPolar(secret, id, ts, sigHeader string, body []byte) error {
	if secret == "" {
		return errors.New("polar webhook secret not configured")
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || time.Since(time.Unix(t, 0)) > sigTolerance || time.Until(time.Unix(t, 0)) > sigTolerance {
		return errors.New("webhook timestamp outside tolerance")
	}
	key := []byte(secret)
	if i := strings.LastIndex(secret, "_"); strings.HasPrefix(secret, "whsec_") {
		// Standard Webhooks generated secrets: whsec_ + base64 key material.
		if b, derr := base64.StdEncoding.DecodeString(secret[i+1:]); derr == nil {
			key = b
		}
	}
	signed := id + "." + ts + "." + string(body)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signed))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	for _, entry := range strings.Fields(sigHeader) {
		ver, sig, ok := strings.Cut(entry, ",")
		if ok && ver == "v1" && hmac.Equal([]byte(sig), []byte(want)) {
			return nil
		}
	}
	return errors.New("webhook signature mismatch")
}

// VerifyStripe checks Stripe-Signature: "t=…,v1=hex,…" HMAC-SHA256 hex over
// "{t}.{body}" keyed by the endpoint secret (used raw, whsec_ prefix included).
func VerifyStripe(secret, header string, body []byte) error {
	if secret == "" {
		return errors.New("stripe webhook secret not configured")
	}
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || time.Since(time.Unix(t, 0)) > sigTolerance || time.Until(time.Unix(t, 0)) > sigTolerance {
		return errors.New("webhook timestamp outside tolerance")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(body)))
	want := hex.EncodeToString(mac.Sum(nil))
	for _, s := range sigs {
		if hmac.Equal([]byte(s), []byte(want)) {
			return nil
		}
	}
	return errors.New("webhook signature mismatch")
}

// ── Order extraction ────────────────────────────────────────────────────

// Order is the normalized "someone paid" fact both providers reduce to.
type Order struct {
	EventID   string // for retry dedup
	Email     string
	ProductID string // polar product id / stripe price id — tier mapping key
	Tier      string // explicit tier override (metadata) — beats ProductID
}

// PolarOrder parses an order.paid event (covers first purchase AND each
// subscription renewal — Polar emits an order per billing cycle).
func PolarOrder(body []byte) (*Order, error) {
	var ev struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Data struct {
			ID        string `json:"id"`
			ProductID string `json:"product_id"`
			Product   struct {
				ID string `json:"id"`
			} `json:"product"`
			Customer struct {
				Email string `json:"email"`
			} `json:"customer"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, err
	}
	if ev.Type != "order.paid" {
		return nil, nil // subscribed event we don't act on
	}
	pid := ev.Data.ProductID
	if pid == "" {
		pid = ev.Data.Product.ID
	}
	return &Order{
		EventID:   "polar:" + first(ev.ID, ev.Data.ID),
		Email:     ev.Data.Customer.Email,
		ProductID: pid,
	}, nil
}

// StripeSession parses checkout.session.completed.
func StripeSession(body []byte) (*Order, string, error) {
	var ev struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID             string `json:"id"`
				PaymentStatus  string `json:"payment_status"`
				CustomerDetail struct {
					Email string `json:"email"`
				} `json:"customer_details"`
				Metadata map[string]string `json:"metadata"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, "", err
	}
	if ev.Type != "checkout.session.completed" && ev.Type != "invoice.payment_succeeded" {
		return nil, "", nil
	}
	o := &Order{
		EventID: "stripe:" + ev.ID,
		Email:   ev.Data.Object.CustomerDetail.Email,
		Tier:    ev.Data.Object.Metadata["tier"],
	}
	return o, ev.Data.Object.ID, nil
}

// StripeInvoice extracts a renewal order from invoice.payment_succeeded —
// invoice line items carry price ids inline (no expansion call needed).
func StripeInvoice(body []byte) (*Order, error) {
	var ev struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object struct {
				CustomerEmail string `json:"customer_email"`
				Lines         struct {
					Data []struct {
						Price struct {
							ID string `json:"id"`
						} `json:"price"`
					} `json:"data"`
				} `json:"lines"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, err
	}
	if ev.Type != "invoice.payment_succeeded" {
		return nil, nil
	}
	o := &Order{EventID: "stripe:" + ev.ID, Email: ev.Data.Object.CustomerEmail}
	for _, l := range ev.Data.Object.Lines.Data {
		if l.Price.ID != "" {
			o.ProductID = l.Price.ID
			break
		}
	}
	return o, nil
}

// StripeSessionPrice fetches the price behind a checkout session when the
// webhook payload doesn't carry it — one authenticated call, only when
// STRIPE_SECRET_KEY is configured.
func (c *Config) StripeSessionPrice(sessionID string) (string, error) {
	if c.StripeAPIKey == "" || sessionID == "" {
		return "", nil
	}
	req, _ := http.NewRequest(http.MethodGet,
		"https://api.stripe.com/v1/checkout/sessions/"+sessionID+"/line_items?limit=1", nil)
	req.Header.Set("Authorization", "Bearer "+c.StripeAPIKey)
	resp, err := c.http().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			Price struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Data) == 0 {
		return "", nil
	}
	return out.Data[0].Price.ID, nil
}

func (c *Config) http() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ── Fulfillment ─────────────────────────────────────────────────────────

// Fulfill mints + delivers a license for a paid order. Idempotent per
// EventID — provider retries after our 2xx would otherwise double-email.
func (c *Config) Fulfill(o *Order) (string, error) {
	if o == nil || o.Email == "" {
		return "", nil
	}
	if c.seen(o.EventID) {
		return "dup", nil
	}
	rule := c.Products[o.ProductID]
	tier, days := o.Tier, rule.Days
	if tier == "" {
		tier = rule.Tier
	}
	if tier == "" {
		tier = license.TierPro // unmapped product → Pro; maintainer reviews mailbox
	}
	if days == 0 {
		days = map[string]int{license.TierPro: 35, license.TierEnterprise: 400}[tier]
	}

	priv, err := os.ReadFile(c.SigningKeyPath)
	if err != nil {
		return "", fmt.Errorf("maintainer key: %w", err)
	}
	lic := &license.License{
		ID:      "lic_" + time.Now().Format("20060102") + "_" + store.NewID(),
		Email:   o.Email,
		Tier:    tier,
		Issued:  time.Now().UTC().Format(time.RFC3339),
		Expires: time.Now().UTC().AddDate(0, 0, days).Format(time.RFC3339),
	}
	if err := license.Sign(priv, lic); err != nil {
		return "", err
	}
	raw, _ := json.MarshalIndent(lic, "", "  ")

	if err := c.deliver(o.Email, lic, raw); err != nil {
		return "", err
	}
	c.mark(o.EventID)
	return lic.ID, nil
}

// deliver sends the license — Resend when configured, mailbox spool otherwise.
func (c *Config) deliver(email string, lic *license.License, raw []byte) error {
	if c.ResendKey != "" && c.From != "" {
		return c.deliverResend(email, lic, raw)
	}
	dir := c.MailboxDir
	if dir == "" {
		h, _ := os.UserHomeDir()
		dir = filepath.Join(h, ".webx", "mailbox")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	safe := strings.NewReplacer("@", "_at_", "/", "_", "\\", "_").Replace(email)
	base := filepath.Join(dir, fmt.Sprintf("%s-%s", safe, lic.ID))
	if err := os.WriteFile(base+".json", raw, 0o600); err != nil {
		return err
	}
	note := fmt.Sprintf("License for %s (tier %s, expires %s)\n\nSend %s to the customer.\nThey run: webx license install <file>\n",
		email, lic.Tier, lic.Expires, base+".json")
	return os.WriteFile(base+".txt", []byte(note), 0o600)
}

func (c *Config) deliverResend(email string, lic *license.License, raw []byte) error {
	payload := map[string]any{
		"from":    c.From,
		"to":      []string{email},
		"subject": fmt.Sprintf("Your webx %s license", lic.Tier),
		"html": fmt.Sprintf(`<p>Thanks — your webx <b>%s</b> license is attached.</p>
<p>Install it on your deployment:</p>
<pre>webx license install webx-license.json
# or export WEBX_LICENSE=/path/to/webx-license.json</pre>
<p>Tier: %s · License: %s · Expires: %s<br>
Subscriptions renew the license automatically — each billing cycle emails a fresh file.</p>
<p>Verify on the server: <code>GET /license</code></p>`,
			lic.Tier, lic.Tier, lic.ID, lic.Expires),
		"attachments": []map[string]string{{
			"filename": "webx-license.json",
			"content":  base64.StdEncoding.EncodeToString(raw),
		}},
	}
	buf, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(buf))
	req.Header.Set("Authorization", "Bearer "+c.ResendKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("resend: %s: %s", resp.Status, b)
	}
	return nil
}

// ── retry dedup — tiny persistent set in the mailbox dir ────────────────

var seenMu sync.Mutex

func (c *Config) seenFile() string {
	dir := c.MailboxDir
	if dir == "" {
		h, _ := os.UserHomeDir()
		dir = filepath.Join(h, ".webx", "mailbox")
	}
	return filepath.Join(dir, ".fulfill-seen")
}

func (c *Config) seen(id string) bool {
	if id == "" {
		return false
	}
	seenMu.Lock()
	defer seenMu.Unlock()
	b, _ := os.ReadFile(c.seenFile())
	var m map[string]string
	json.Unmarshal(b, &m)
	_, ok := m[id]
	return ok
}

func (c *Config) mark(id string) {
	if id == "" {
		return
	}
	seenMu.Lock()
	defer seenMu.Unlock()
	f := c.seenFile()
	os.MkdirAll(filepath.Dir(f), 0o700)
	b, _ := os.ReadFile(f)
	m := map[string]string{}
	json.Unmarshal(b, &m)
	m[id] = time.Now().UTC().Format(time.RFC3339)
	// cap at 10k entries — oldest drop first
	if len(m) > 10000 {
		type kv struct{ k, v string }
		var all []kv
		for k, v := range m {
			all = append(all, kv{k, v})
		}
		for i := 0; i < len(all)-10000; i++ {
			oldest := ""
			for _, e := range all {
				if e.v != "" && (oldest == "" || e.v < m[oldest]) {
					oldest = e.k
				}
			}
			delete(m, oldest)
		}
	}
	w, _ := json.Marshal(m)
	os.WriteFile(f, w, 0o600)
}

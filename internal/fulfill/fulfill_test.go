package fulfill

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kasyap1234/webx/internal/license"
)

func signStd(secret, id, ts string, body []byte) string {
	key := []byte(secret)
	if strings.HasPrefix(secret, "whsec_") {
		if b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_")); err == nil {
			key = b
		}
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "." + string(body)))
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func signStripe(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(body)))
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyPolar(t *testing.T) {
	body := []byte(`{"type":"order.paid","data":{}}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	cases := []struct {
		name    string
		secret  string
		wantErr bool
	}{
		{"plain secret", "mysecret", false},
		{"whsec_ base64", "whsec_" + base64.StdEncoding.EncodeToString([]byte("k3y-bytes")), false},
		{"empty secret", "", true},
	}
	for _, tc := range cases {
		sig := signStd(tc.secret, "msg_1", ts, body)
		err := VerifyPolar(tc.secret, "msg_1", ts, sig, body)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", tc.name, err, tc.wantErr)
		}
	}
	// tampered body fails
	sig := signStd("s", "m", ts, body)
	if err := VerifyPolar("s", "m", ts, sig, []byte(`{"tampered":1}`)); err == nil {
		t.Error("tampered body verified")
	}
	// stale timestamp fails
	old := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	sig = signStd("s", "m", old, body)
	if err := VerifyPolar("s", "m", old, sig, body); err == nil {
		t.Error("stale timestamp verified")
	}
}

func TestVerifyStripe(t *testing.T) {
	body := []byte(`{"type":"checkout.session.completed"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	hdr := signStripe("whsec_test", ts, body)
	if err := VerifyStripe("whsec_test", hdr, body); err != nil {
		t.Fatalf("valid stripe sig rejected: %v", err)
	}
	if err := VerifyStripe("whsec_wrong", hdr, body); err == nil {
		t.Error("wrong secret verified")
	}
	if err := VerifyStripe("whsec_test", hdr, []byte(`{}`)); err == nil {
		t.Error("tampered body verified")
	}
}

func TestPolarOrderParse(t *testing.T) {
	body := []byte(`{"type":"order.paid","id":"evt_1","data":{"id":"ord_1","product_id":"prod_pro","customer":{"email":"a@b.c"}}}`)
	o, err := PolarOrder(body)
	if err != nil || o == nil {
		t.Fatalf("order.paid not extracted: %v", err)
	}
	if o.Email != "a@b.c" || o.ProductID != "prod_pro" || o.EventID != "polar:evt_1" {
		t.Errorf("bad order: %+v", o)
	}
	// unrelated event → nil (ack'd but not fulfilled)
	o, err = PolarOrder([]byte(`{"type":"subscription.updated","data":{}}`))
	if o != nil || err != nil {
		t.Errorf("non-paid event should be nil, got %+v", o)
	}
}

func TestStripeSessionParse(t *testing.T) {
	body := []byte(`{"id":"evt_9","type":"checkout.session.completed","data":{"object":{"id":"cs_1","payment_status":"paid","customer_details":{"email":"x@y.z"},"metadata":{"tier":"enterprise"}}}}`)
	o, sid, err := StripeSession(body)
	if err != nil || o == nil {
		t.Fatalf("session not extracted: %v", err)
	}
	if o.Tier != "enterprise" || o.Email != "x@y.z" || sid != "cs_1" {
		t.Errorf("bad session: %+v sid=%s", o, sid)
	}
}

func TestStripeInvoiceParse(t *testing.T) {
	body := []byte(`{"id":"evt_i","type":"invoice.payment_succeeded","data":{"object":{"customer_email":"x@y.z","lines":{"data":[{"price":{"id":"price_pro"}}]}}}}`)
	o, err := StripeInvoice(body)
	if err != nil || o == nil {
		t.Fatalf("invoice not extracted: %v", err)
	}
	if o.ProductID != "price_pro" || o.Email != "x@y.z" {
		t.Errorf("bad invoice order: %+v", o)
	}
}

func TestParseProductMap(t *testing.T) {
	m := ParseProductMap(" a:pro , b:enterprise:400 ,c:pro:90")
	if m["a"].Tier != "pro" || m["a"].Days != 35 {
		t.Errorf("default days for pro: %+v", m["a"])
	}
	if m["b"].Tier != "enterprise" || m["b"].Days != 400 {
		t.Errorf("explicit days: %+v", m["b"])
	}
	if m["c"].Days != 90 {
		t.Errorf("custom days: %+v", m["c"])
	}
}

func TestFulfillMailboxAndDedup(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "m.pem")
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	os.WriteFile(key, priv, 0o600)

	cfg := &Config{
		SigningKeyPath: key,
		MailboxDir:     filepath.Join(dir, "mbox"),
		Products:       ParseProductMap("prod_pro:pro"),
	}
	o := &Order{EventID: "polar:e1", Email: "buyer@x.io", ProductID: "prod_pro"}
	id, err := cfg.Fulfill(o)
	if err != nil || id == "" {
		t.Fatalf("fulfill: %v id=%q", err, id)
	}
	// license written + verifies
	matches, _ := filepath.Glob(filepath.Join(cfg.MailboxDir, "*.json"))
	if len(matches) != 1 {
		t.Fatalf("expected 1 license file, got %d", len(matches))
	}
	raw, _ := os.ReadFile(matches[0])
	l, err := license.Parse(raw)
	// Parse uses env pubkey — set it to our generated pub
	_ = l
	_ = err
	if l == nil {
		os.Setenv("WEBX_LICENSE_PUBKEY", base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)))
		defer os.Unsetenv("WEBX_LICENSE_PUBKEY")
		l, err = license.Parse(raw)
	}
	if err != nil || l.Tier != "pro" {
		t.Fatalf("minted license invalid: %v", err)
	}
	// dedup: same event → "dup", no second file
	id2, _ := cfg.Fulfill(o)
	if id2 != "dup" {
		t.Errorf("expected dup, got %q", id2)
	}
	matches, _ = filepath.Glob(filepath.Join(cfg.MailboxDir, "*.json"))
	if len(matches) != 1 {
		t.Errorf("dedup failed: %d files", len(matches))
	}
	fmt.Println("minted license verified, dedup ok")
}

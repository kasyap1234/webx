// webxl — the license-fulfillment service. Maintainer-side only: it holds the
// Ed25519 signing key and turns provider webhooks into delivered licenses.
//
//	Polar.sh checkout → POST /webhooks/polar  (order.paid → license → email)
//	Stripe checkout   → POST /webhooks/stripe (checkout.session.completed /
//	                    invoice.payment_succeeded → license → email)
//
// Env:
//
//	WEBX_MAINTAINER_KEY   ed25519 private key file — from `webx license keygen` (required)
//	POLAR_WEBHOOK_SECRET  Polar webhook secret
//	POLAR_TIERS           "product_id:tier[:days],..." e.g. "prod_9x:pro,prod_yy:enterprise:400"
//	STRIPE_WEBHOOK_SECRET Stripe endpoint secret (whsec_…)
//	STRIPE_TIERS          "price_id:tier[:days],..."
//	STRIPE_SECRET_KEY     sk_… — resolves the price behind checkout sessions
//	RESEND_API_KEY        email delivery; without it licenses spool to MAILBOX_DIR
//	LICENSE_FROM          sender, e.g. "webx licenses <licenses@yourdomain>"
//	MAILBOX_DIR           spool dir (default ~/.webx/mailbox)
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/kasyap1234/webx/internal/fulfill"
)

func main() {
	cfg := &fulfill.Config{
		SigningKeyPath: os.Getenv("WEBX_MAINTAINER_KEY"),
		PolarSecret:    os.Getenv("POLAR_WEBHOOK_SECRET"),
		StripeSecret:   os.Getenv("STRIPE_WEBHOOK_SECRET"),
		StripeAPIKey:   os.Getenv("STRIPE_SECRET_KEY"),
		ResendKey:      os.Getenv("RESEND_API_KEY"),
		From:           os.Getenv("LICENSE_FROM"),
		MailboxDir:     os.Getenv("MAILBOX_DIR"),
	}
	// provider→tier maps share one product-id keyed table
	cfg.Products = fulfill.ParseProductMap(os.Getenv("POLAR_TIERS"))
	for k, v := range fulfill.ParseProductMap(os.Getenv("STRIPE_TIERS")) {
		cfg.Products[k] = v
	}

	if cfg.SigningKeyPath == "" {
		log.Fatal("WEBX_MAINTAINER_KEY required — run `webx license keygen --key maintainer.pem`")
	}
	if cfg.ResendKey == "" || cfg.From == "" {
		log.Print("RESEND_API_KEY/LICENSE_FROM unset — licenses spool to mailbox for manual delivery")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("POST /webhooks/polar", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read", 400)
			return
		}
		if err := fulfill.VerifyPolar(cfg.PolarSecret,
			r.Header.Get("webhook-id"), r.Header.Get("webhook-timestamp"),
			r.Header.Get("webhook-signature"), body); err != nil {
			http.Error(w, "signature: "+err.Error(), http.StatusForbidden)
			return
		}
		o, err := fulfill.PolarOrder(body)
		if err != nil {
			http.Error(w, "payload: "+err.Error(), 400)
			return
		}
		fulfillOrder(w, cfg, o)
	})

	mux.HandleFunc("POST /webhooks/stripe", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read", 400)
			return
		}
		if err := fulfill.VerifyStripe(cfg.StripeSecret,
			r.Header.Get("Stripe-Signature"), body); err != nil {
			http.Error(w, "signature: "+err.Error(), http.StatusForbidden)
			return
		}
		// invoice.payment_succeeded carries price ids inline
		if o, err := fulfill.StripeInvoice(body); err == nil && o != nil {
			fulfillOrder(w, cfg, o)
			return
		}
		o, sessionID, err := fulfill.StripeSession(body)
		if err != nil {
			http.Error(w, "payload: "+err.Error(), 400)
			return
		}
		if o != nil && o.ProductID == "" && o.Tier == "" {
			if pid, err := cfg.StripeSessionPrice(sessionID); err == nil {
				o.ProductID = pid
			}
		}
		fulfillOrder(w, cfg, o)
	})

	addr := ":8090"
	if a := os.Getenv("ADDR"); a != "" {
		addr = a
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("webxl listening on %s (products mapped: %d)", addr, len(cfg.Products))
	log.Fatal(srv.ListenAndServe())
}

func fulfillOrder(w http.ResponseWriter, cfg *fulfill.Config, o *fulfill.Order) {
	if o == nil || o.Email == "" {
		w.WriteHeader(http.StatusAccepted) // event we don't act on — ack so provider stops retrying
		return
	}
	id, err := cfg.Fulfill(o)
	if err != nil {
		log.Printf("fulfill %s: %v", o.Email, err)
		http.Error(w, "fulfill", 500) // provider retries with backoff
		return
	}
	log.Printf("license %s → %s (%s)", id, o.Email, strings.TrimPrefix(o.EventID, "polar:"))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"license": id})
}

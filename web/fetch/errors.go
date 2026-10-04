package fetch

// errors.go — machine-readable failure taxonomy. Fetch failures surface as
// *FetchError with a stable Code agents can branch on ("payment_required",
// "blocked_by_policy") instead of string-matching messages. The HTTP layer
// maps Code → status + an `errors[].code` field; CLI prints it verbatim.

import (
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Stable error codes — documented contract, new codes are additive.
const (
	CodeInvalidURL      = "invalid_url"       // unparsable/unsupported scheme
	CodeBlockedByPolicy = "blocked_by_policy" // SSRF guard / domain policy refusal
	CodePaymentRequired = "payment_required"  // HTTP 402 (+x402/AP2 headers when present)
	CodeUnsupportedType = "unsupported_type"  // non-HTML/PDF/markdown content
	CodeHTTPStatus      = "http_status"       // other non-2xx
	CodeRenderFailed    = "render_failed"     // browser tier errored
	CodeEngineMissing   = "engine_missing"    // requested render engine not installed
	CodeRateLimited     = "rate_limited"      // 429 after Retry-After retry
	CodeBodyTooLarge    = "body_too_large"    // > maxBodyBytes
	CodeUpStream        = "upstream"          // transport/network failure
)

// PaymentInfo describes a payment gate the host demanded — enough for an
// agent (or its wallet layer) to act. Detection only: webx never pays.
type PaymentInfo struct {
	Protocol string `json:"protocol,omitempty"` // "x402" when X-Payment headers present, "ap2" for Payment-*
	Required string `json:"required,omitempty"` // raw Payment-Required / WWW-Authenticate header
	Accepts  string `json:"accepts,omitempty"`  // raw X-Payment-Accepts / payment method hints
	Price    string `json:"price,omitempty"`    // parsed amount when a scheme advertises one
}

// FetchError carries a failure with its stable machine code.
type FetchError struct {
	Code    string
	Message string
	Payment *PaymentInfo // set when Code == payment_required
	Err     error        // wrapped cause, when any
}

func (e *FetchError) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *FetchError) Unwrap() error { return e.Err }

// ferr wraps msg+err in a coded FetchError (nil-safe for chaining).
func ferr(code, msg string, err error) *FetchError {
	return &FetchError{Code: code, Message: msg, Err: err}
}

// ErrorCode extracts the stable code from an error chain — "" for
// non-webx errors. Used by serve to emit errors[].code.
func ErrorCode(err error) string {
	for ; err != nil; err = errCause(err) {
		if fe, ok := err.(*FetchError); ok {
			return fe.Code
		}
	}
	return ""
}

// PaymentOf extracts payment-gate details when the error is one.
func PaymentOf(err error) *PaymentInfo {
	for ; err != nil; err = errCause(err) {
		if fe, ok := err.(*FetchError); ok && fe.Payment != nil {
			return fe.Payment
		}
	}
	return nil
}

func errCause(err error) error {
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return u.Unwrap()
	}
	return nil
}

// paymentGate reads a 402's headers for the payment protocol in play:
// x402 (Coinbase/Cloudflare — X-Payment-*), AP2/IETF httpbis-payments
// (Payment-Required/Payment-*), or a bare 402 challenge.
func paymentGate(h http.Header) *PaymentInfo {
	p := &PaymentInfo{}
	for _, k := range []string{"X-Payment-Required", "X-Payment-Accepts", "X-Payment"} {
		if v := h.Get(k); v != "" {
			p.Protocol = "x402"
			if p.Accepts == "" {
				p.Accepts = v
			}
		}
	}
	for _, k := range []string{"Payment-Required", "Payment"} {
		if v := h.Get(k); v != "" && p.Protocol == "" {
			p.Protocol = "ap2"
			p.Required = v
		}
	}
	if wa := h.Get("WWW-Authenticate"); wa != "" {
		if p.Required == "" {
			p.Required = wa
		}
		if p.Protocol == "" && strings.Contains(strings.ToLower(wa), "payment") {
			p.Protocol = "http-payment"
		}
	}
	// Price extraction — x402 bodies carry {"accepts":[{"price":...}]; AP2
	// puts `price=` params in Payment-Required. Check whichever we captured.
	for _, src := range []string{p.Accepts, p.Required} {
		if src == "" {
			continue
		}
		if m := priceRe.FindStringSubmatch(src); len(m) > 1 {
			p.Price = m[1]
			break
		}
		if m := priceParamRe.FindStringSubmatch(src); len(m) > 1 {
			p.Price = m[1]
			break
		}
	}
	if p.Protocol == "" {
		p.Protocol = "unknown" // bare 402 — gate exists, scheme unstated
	}
	return p
}

var (
	priceRe      = regexp.MustCompile(`"(?:price|amount|cost|maxAmountRequired)"\s*:\s*"?([\$€£]?[0-9][0-9.,]*[A-Za-z$€£]*)`)
	priceParamRe = regexp.MustCompile(`(?:price|amount|cost)\s*[=:]\s*"?([\$€£]?[0-9][0-9.,]*[A-Za-z$€£]*)`)
)

// retryAfterHint parses a Retry-After header (seconds or HTTP-date).
func retryAfterHint(v string) time.Duration {
	if v == "" {
		return 0
	}
	if d, err := time.ParseDuration(v + "s"); err == nil {
		return d
	}
	if t, err := time.Parse(time.RFC1123, v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	if t, err := time.Parse(http_timeFormatGMT, v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

const http_timeFormatGMT = "Mon, 02 Jan 2006 15:04:05 GMT"

// cappedRetry bounds the wait so a hostile "Retry-After: 86400" can't hang a
// fetch — we honor up to 8s, then report rate_limited honestly.
const maxRetryWait = 8 * time.Second

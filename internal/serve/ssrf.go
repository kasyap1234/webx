package serve

// ssrf.go — outbound-target guard for the server. A public webxd must not
// become an internal-network proxy: /scrape of http://169.254.169.254/ or
// http://localhost:6379 is how scrapers get turned into credential leaks.
//
// Policy (all env-driven, all optional):
//   WEBX_BLOCK_PRIVATE   "1"/"true" (default ON when set to anything but "0"/"false")
//   WEBX_ALLOW_DOMAINS   comma list — only these domains (+subdomains) allowed
//   WEBX_DENY_DOMAINS    comma list — these domains (+subdomains) refused
//
// The guard resolves the hostname once and rejects if ANY A/AAAA answer is
// private — a DNS-rebinding "one good, one bad" answer still fails closed.

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// TargetGuard decides whether an outbound fetch URL is permitted.
type TargetGuard struct {
	blockPrivate bool
	allow        []string
	deny         []string
}

// NewTargetGuard builds the guard from the WEBX_* environment. Private-IP
// blocking defaults ON — opt out explicitly with WEBX_BLOCK_PRIVATE=0 for
// intranet deployments.
func NewTargetGuard() *TargetGuard {
	g := &TargetGuard{blockPrivate: true}
	switch strings.ToLower(os.Getenv("WEBX_BLOCK_PRIVATE")) {
	case "0", "false", "off":
		g.blockPrivate = false
	}
	g.allow = splitCSV(os.Getenv("WEBX_ALLOW_DOMAINS"))
	g.deny = splitCSV(os.Getenv("WEBX_DENY_DOMAINS"))
	return g
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// CheckURL vetoes a target URL. Returns "" when allowed, else a reason.
func (g *TargetGuard) CheckURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("bad url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if len(g.allow) > 0 && !domainMatch(host, g.allow) {
		return fmt.Errorf("domain %s not in allow list", host)
	}
	if domainMatch(host, g.deny) {
		return fmt.Errorf("domain %s is denied", host)
	}
	if !g.blockPrivate {
		return nil
	}
	// Literal-IP fast path — no DNS needed.
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("private/internal address not allowed")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil // DNS failure is the fetcher's error to report, not ours
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("%s resolves to a private/internal address", host)
		}
	}
	return nil
}

func domainMatch(host string, list []string) bool {
	for _, d := range list {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// isPrivateIP covers loopback, RFC1918, link-local (incl. the cloud-metadata
// 169.254.0.0/16), ULA v6, and CGNAT space.
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	private := []string{
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		"100.64.0.0/10", "fc00::/7", "fe80::/10",
		"198.18.0.0/15", // benchmarking space — never a public site
	}
	for _, cidr := range private {
		_, n, _ := net.ParseCIDR(cidr)
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Package dns resolves the authoritative DNS provider of a domain's apex and
// classifies it for the CNAME-add strategy in `exebox new`:
//
//   - cloudflare:   NS contains *.cloudflare.com (direct API apply with a token)
//   - domainconnect: apex publishes TXT _domainconnect.<apex> (DC apply URL)
//   - manual:       anything else (print exact record values)
//
// Apex detection uses golang.org/x/net/publicsuffix so e.g.
// new-app.devbox.nesin.io -> nesin.io, and app.co.uk -> app.co.uk.
package dns

import (
	"fmt"
	"net"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// Provider is the classification result.
type Provider string

const (
	ProviderCloudflare     Provider = "cloudflare"
	ProviderDomainConnect  Provider = "domainconnect" // generic DC-capable (non-Cloudflare)
	ProviderManual         Provider = "manual"
)

// Result is the DNS investigation outcome for one domain.
type Result struct {
	Domain      string   `json:"domain"`      // the input FQDN
	Apex        string   `json:"apex"`        // registered domain (public suffix + 1)
	HostLabel   string   `json:"host_label"`   // labels left of apex, e.g. "new-app.devbox"
	Provider    Provider `json:"provider"`
	Nameservers []string `json:"nameservers"`
	DCAPI       string   `json:"dc_api,omitempty"` // TXT _domainconnect value, when present
}

// IsCloudflare reports whether the apex's NS is Cloudflare.
func (r Result) IsCloudflare() bool { return r.Provider == ProviderCloudflare }

// Investigate classifies the provider of the domain's apex.
func Investigate(domain string) (Result, error) {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	apex, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return Result{}, fmt.Errorf("apex for %q: %w", domain, err)
	}
	hostLabel := ""
	if domain != apex {
		hostLabel = strings.TrimSuffix(domain, "."+apex)
	}

	r := Result{Domain: domain, Apex: apex, HostLabel: hostLabel}

	// NS lookup -> Cloudflare detection.
	ns, _ := net.LookupNS(apex)
	for _, n := range ns {
		r.Nameservers = append(r.Nameservers, strings.TrimSuffix(n.Host, "."))
		if strings.HasSuffix(n.Host, "cloudflare.com.") {
			r.Provider = ProviderCloudflare
		}
	}

	// Domain Connect discovery: TXT _domainconnect.<apex>.
	if txts, err := net.LookupTXT("_domainconnect." + apex); err == nil {
		for _, t := range txts {
			t = strings.TrimSpace(t)
			// DC API endpoints look like a host/path (often scheme-less, e.g.
			// "api.cloudflare.com/client/v4/dns/domainconnect"). Reject obvious
			// non-endpoints (short tokens, spf/dkim).
			if isDCEndpoint(t) {
				r.DCAPI = normalizeDCEndpoint(t)
				if r.Provider == "" {
					r.Provider = ProviderDomainConnect
				}
				break
			}
		}
	}

	if r.Provider == "" {
		r.Provider = ProviderManual
	}
	return r, nil
}

// Record is the CNAME to add (what exebox prints/creates).
type Record struct {
	Host   string // name relative to apex, e.g. "new-app.devbox"
	Type   string // "CNAME"
	Target string // <vm>.exe.xyz
	TTL    int    // seconds; 300 default
	Proxied bool   // Cloudflare orange-cloud; we default false (DNS-only)
}

// isDCEndpoint heuristically recognizes a Domain Connect API endpoint in a
// TXT record: it contains a dot and a slash (host/path), isn't an SPF/dkim
// token. Conservative: false negatives just downgrade to "manual".
func isDCEndpoint(t string) bool {
	if t == "" || len(t) < 8 {
		return false
	}
	low := strings.ToLower(t)
	if strings.HasPrefix(low, "v=spf1") || strings.HasPrefix(low, "v=dkim") {
		return false
	}
	return strings.Contains(t, ".") && strings.Contains(t, "/")
}

// normalizeDCEndpoint ensures a scheme for display, strips trailing slash.
func normalizeDCEndpoint(t string) string {
	t = strings.TrimRight(t, "/")
	if !strings.HasPrefix(t, "http://") && !strings.HasPrefix(t, "https://") {
		t = "https://" + t
	}
	return t
}

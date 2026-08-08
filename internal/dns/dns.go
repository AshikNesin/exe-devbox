// Package dns resolves the authoritative DNS provider of a domain's apex and
// classifies it for the CNAME-add strategy in `devbox new`:
//
//   - cloudflare: NS contains *.cloudflare.com (direct API apply)
//   - manual:     anything else (print exact record values)
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
	ProviderCloudflare Provider = "cloudflare"
	ProviderManual     Provider = "manual"
)

// Result is the DNS investigation outcome for one domain.
type Result struct {
	Domain      string   `json:"domain"`       // the input FQDN
	Apex        string   `json:"apex"`         // registered domain (public suffix + 1)
	HostLabel   string   `json:"host_label"`   // labels left of apex, e.g. "new-app.devbox"
	Provider    Provider `json:"provider"`
	Nameservers []string `json:"nameservers"`
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

	if r.Provider == "" {
		r.Provider = ProviderManual
	}
	return r, nil
}

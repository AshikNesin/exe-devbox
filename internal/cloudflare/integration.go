package cloudflare

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/ashiknesin/exe-devbox/internal/reflection"
)

// urlRE matches the first http(s) URL token (no whitespace) in free text. We
// extract the proxy base URL from the reflection integration's Help text,
// which looks like: "Cloudflare API via https://cloudflare.int.exe.xyz/ …".
var urlRE = regexp.MustCompile(`https?://[^\s]+`)

// proxyBaseFromHelp extracts the proxy host URL from the integration Help
// text. It returns the URL with any trailing slash stripped so callers can
// append "/client/v4/...". Returns "" if no URL is found.
func proxyBaseFromHelp(help string) string {
	m := urlRE.FindString(help)
	if m == "" {
		return ""
	}
	return strings.TrimRight(m, "/")
}

// proxyBase returns the discovered Cloudflare proxy base URL and whether the
// "cloudflare" integration is attached to this VM. Returns ("", false) if
// reflection is unavailable or the integration isn't attached.
var proxyBase = proxyBaseReflection

// proxyBaseReflection is the default proxyBase implementation: it queries the
// exe.dev reflection endpoint for the "cloudflare" integration and extracts
// the proxy URL from its Help text.
func proxyBaseReflection() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	in, err := reflection.New().Integration(ctx, "cloudflare")
	if err != nil || in == nil {
		return "", false
	}
	base := proxyBaseFromHelp(in.Help)
	if base == "" {
		return "", false
	}
	return base, true
}

// Package cloudflare applies the CNAME directly via the Cloudflare API when
// credentials are available. Two sources, in priority order:
//
//   - $CLOUDFLARE_API_TOKEN (needs Zone.DNS Edit + Zone.Read on the apex's zone).
//   - The exe.dev "cloudflare" integration: credentials are injected by a
//     network-edge proxy whose base URL is discovered at runtime from the
//     reflection endpoint's integration Help text, so no token is needed in
//     the VM. We route API calls through that host and the proxy adds auth.
//
// We never print the token.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// directAPI is the public Cloudflare API (used in token mode).
const directAPI = "https://api.cloudflare.com/client/v4"
// Client calls the Cloudflare API. Exactly one of Token/Base is set:
//   - Token+Base=directAPI: direct API with a bearer token.
//   - Base=<discovered proxy URL> (no Token): exe.dev proxy injects auth.
type Client struct {
	Token string // empty when using the exe.dev proxy
	Base  string // resolved API base URL (already includes /client/v4)
	HTTP  *http.Client
}

// New returns a direct-API client if $CLOUDFLARE_API_TOKEN is set, else a
// proxy-mode client whose base URL is discovered at runtime from the exe.dev
// reflection endpoint's "cloudflare" integration. If the token is unset and
// the proxy base can't be discovered, Base is empty and Available() is false.
func New() *Client {
	hc := &http.Client{Timeout: 15 * time.Second}
	if t := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN")); t != "" {
		return &Client{Token: t, Base: directAPI, HTTP: hc}
	}
	// proxy mode: discover the base URL from reflection. We don't fail here;
	// callers check Available() before use, and Available() re-discovers if Base
	// is still empty (covers the case where reflection wasn't ready at New()).
	c := &Client{HTTP: hc}
	if base, ok := proxyBase(); ok {
		c.Base = base + "/client/v4"
	}
	return c
}

// Available reports whether this client can authenticate.
//   - token mode: always true (a token is set).
//   - proxy mode: true only if a proxy base URL could be discovered. If Base
//     is still empty, we retry discovery once (covers reflection not being
//     ready at New() time).
func (c *Client) Available() bool {
	if c == nil {
		return false
	}
	if c.Token != "" {
		return true
	}
	if c.Base != "" {
		return true
	}
	// last-chance discovery
	if base, ok := proxyBase(); ok {
		c.Base = base + "/client/v4"
		return true
	}
	return false
}

// Mode returns a human label for the active credential source ("token" or "proxy").
func (c *Client) Mode() string {
	if c != nil && c.Token != "" {
		return "token"
	}
	return "proxy"
}

// zoneID finds the zone for an apex (e.g. nesin.io).
func (c *Client) zoneID(ctx context.Context, apex string) (string, error) {
	u := c.Base + "/zones?name=" + url.QueryEscape(apex)
	var resp struct {
		Result []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
		Success bool `json:"success"`
		Errors  []struct{ Message string } `json:"errors"`
	}
	if err := c.do(ctx, u, nil, &resp); err != nil {
		return "", err
	}
	if len(resp.Result) == 0 {
		return "", fmt.Errorf("no Cloudflare zone for %q (token may lack Zone.Read)", apex)
	}
	return resp.Result[0].ID, nil
}

// UpsertCNAME creates or updates a CNAME record (host.target). Returns the
// record id and "created" vs "updated".
func (c *Client) UpsertCNAME(ctx context.Context, apex, host, target string) (id string, created bool, err error) {
	zid, err := c.zoneID(ctx, apex)
	if err != nil {
		return "", false, err
	}

	// Look for an existing record at this host (any type) to update.
	listU := fmt.Sprintf("%s/zones/%s/dns_records?name=%s", c.Base, zid, url.QueryEscape(host))
	var existing struct {
		Result []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Content string `json:"content"`
		} `json:"result"`
	}
	if err := c.do(ctx, listU, nil, &existing); err != nil {
		return "", false, err
	}

	body := map[string]any{
		"type": "CNAME", "name": host, "content": target,
		"ttl": 300, "proxied": false,
	}

	for _, rec := range existing.Result {
		// update in place (type may already be CNAME; we overwrite to be idempotent)
		putU := fmt.Sprintf("%s/zones/%s/dns_records/%s", c.Base, zid, rec.ID)
		var resp struct {
			Success bool `json:"success"`
			Errors  []struct{ Message string } `json:"errors"`
			Result  struct{ ID string } `json:"result"`
		}
		if err := c.do(ctx, putU, body, &resp); err != nil {
			return "", false, err
		}
		if !resp.Success {
			return "", false, fmt.Errorf("cloudflare update: %v", resp.Errors)
		}
		return resp.Result.ID, false, nil
	}

	// create
	postU := fmt.Sprintf("%s/zones/%s/dns_records", c.Base, zid)
	var resp struct {
		Success bool `json:"success"`
		Errors  []struct{ Message string } `json:"errors"`
		Result  struct{ ID string } `json:"result"`
	}
	if err := c.do(ctx, postU, body, &resp); err != nil {
		return "", false, err
	}
	if !resp.Success {
		return "", false, fmt.Errorf("cloudflare create: %v", resp.Errors)
	}
	return resp.Result.ID, true, nil
}

// DeleteCNAME removes the DNS record at the given host. Returns whether a
// record was found and deleted. It matches any record type at that name.
func (c *Client) DeleteCNAME(ctx context.Context, apex, host string) (deleted bool, err error) {
	zid, err := c.zoneID(ctx, apex)
	if err != nil {
		return false, err
	}

	// Look for an existing record at this host (any type).
	listU := fmt.Sprintf("%s/zones/%s/dns_records?name=%s", c.Base, zid, url.QueryEscape(host))
	var existing struct {
		Result []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"result"`
	}
	if err := c.do(ctx, listU, nil, &existing); err != nil {
		return false, err
	}
	if len(existing.Result) == 0 {
		return false, nil // nothing to delete
	}

	for _, rec := range existing.Result {
		delU := fmt.Sprintf("%s/zones/%s/dns_records/%s", c.Base, zid, rec.ID)
		var resp struct {
			Success bool `json:"success"`
			Errors  []struct{ Message string } `json:"errors"`
		}
		if err := c.doDelete(ctx, delU, &resp); err != nil {
			return false, err
		}
		if !resp.Success {
			return false, fmt.Errorf("cloudflare delete: %v", resp.Errors)
		}
	}
	return true, nil
}

// do executes an authenticated request. method is POST for a body, GET otherwise.
func (c *Client) do(ctx context.Context, u string, body any, dst any) error {
	hc, err := c.request(ctx, u, body, http.MethodGet)
	if err != nil {
		return err
	}
	return c.run(hc, dst)
}

// doDelete executes an authenticated DELETE request.
func (c *Client) doDelete(ctx context.Context, u string, dst any) error {
	hc, err := c.request(ctx, u, nil, http.MethodDelete)
	if err != nil {
		return err
	}
	return c.run(hc, dst)
}

// request builds an authenticated request. method defaults to GET; POST for a
// body ending in /dns_records; PUT for other bodies.
func (c *Client) request(ctx context.Context, u string, body any, defaultMethod string) (*http.Request, error) {
	var rdr io.Reader
	method := defaultMethod
	if body != nil {
		method = http.MethodPut
		if strings.HasSuffix(u, "/dns_records") {
			method = http.MethodPost
		}
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, err
	}
	// The exe.dev proxy injects auth for proxy-mode; only set the header in
	// direct-token mode (setting it in proxy mode would be harmless but wrong).
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// run executes the request and unmarshals the response.
func (c *Client) run(req *http.Request, dst any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s %s: %s: %s", req.Method, redactURL(req.URL.String()), resp.Status, strings.TrimSpace(string(data)))
	}
	return json.Unmarshal(data, dst)
}

// redactURL strips query for log safety (never logs token; token is in header).
func redactURL(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	p.RawQuery = ""
	return p.String()
}
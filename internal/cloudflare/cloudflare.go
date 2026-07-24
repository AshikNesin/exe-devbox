// Package cloudflare applies the CNAME directly via the Cloudflare API when the
// user provides a token (the PRD's "today-working fallback", since real
// one-click Domain Connect needs a signed/onboarded template).
//
// Token source: $CLOUDFLARE_API_TOKEN (needs Zone.DNS Edit + Zone.Read on the
// apex's zone). We never print the token.
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

const api = "https://api.cloudflare.com/client/v4"

// Client calls the Cloudflare API with a bearer token.
type Client struct {
	Token string
	HTTP  *http.Client
}

// New returns a client from $CLOUDFLARE_API_TOKEN, or nil if unset.
func New() *Client {
	t := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
	if t == "" {
		return nil
	}
	return &Client{Token: t, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Available reports whether a token is configured.
func (c *Client) Available() bool { return c != nil && c.Token != "" }

// zoneID finds the zone for an apex (e.g. nesin.io).
func (c *Client) zoneID(ctx context.Context, apex string) (string, error) {
	u := api + "/zones?name=" + url.QueryEscape(apex)
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
	listU := fmt.Sprintf("%s/zones/%s/dns_records?name=%s", api, zid, url.QueryEscape(host))
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
		putU := fmt.Sprintf("%s/zones/%s/dns_records/%s", api, zid, rec.ID)
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
	postU := fmt.Sprintf("%s/zones/%s/dns_records", api, zid)
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

// do executes an authenticated request. method is POST for a body, GET otherwise.
func (c *Client) do(ctx context.Context, u string, body any, dst any) error {
	var rdr io.Reader
	var method = http.MethodGet
	if body != nil {
		method = http.MethodPut
		if strings.HasSuffix(u, "/dns_records") {
			method = http.MethodPost
		}
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s %s: %s: %s", method, redactURL(u), resp.Status, strings.TrimSpace(string(data)))
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

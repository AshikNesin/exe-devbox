// Package exeapi calls the exe.dev HTTPS exec API (POST https://exe.dev/exec).
// It's the same as running `ssh exe.dev <cmd>` but auth'd via a bearer token,
// so exebox can automate lobby commands like `domain add` without SSH access.
//
// Token source: $EXE_API_TOKEN. Create one scoped to just the commands you
// need (e.g. domain add) — see https://exe.dev/docs/https-api-local-key.md
package exeapi

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// execEndpoint is the exe.dev HTTPS API.
const execEndpoint = "https://exe.dev/exec"

// Client calls the exe.dev exec API with a bearer token.
type Client struct {
	Token string
	HTTP  *http.Client
}

// New returns a client from $EXE_API_TOKEN, or nil if unset.
func New() *Client {
	t := strings.TrimSpace(os.Getenv("EXE_API_TOKEN"))
	if t == "" {
		return nil
	}
	return &Client{Token: t, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Available reports whether a token is configured.
func (c *Client) Available() bool { return c != nil && c.Token != "" }

// Exec runs a lobby command via the exe.dev HTTPS API and returns the output.
// The body is the exact lobby command string (e.g. "domain add myvm app.com").
// JSON output is always enabled by the API (equivalent to --json).
func (c *Client) Exec(cmd string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, execEndpoint, bytes.NewReader([]byte(cmd)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "text/plain")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	out := strings.TrimSpace(string(body))
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return out, fmt.Errorf("api token lacks permission for %q (status 403)", cmd)
	case resp.StatusCode == http.StatusUnauthorized:
		return out, fmt.Errorf("api token invalid or expired (status 401)")
	case resp.StatusCode >= 400:
		return out, fmt.Errorf("exe.dev exec failed (status %d): %s", resp.StatusCode, out)
	}
	return out, nil
}

// DomainAdd registers a custom domain with exe.dev via the HTTPS API.
// Returns the API response text.
func (c *Client) DomainAdd(vm, domain string) (string, error) {
	return c.Exec(fmt.Sprintf("domain add %s %s", vm, domain))
}
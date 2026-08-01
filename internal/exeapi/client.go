// Package exeapi calls the exe.dev HTTPS exec API (POST https://exe.dev/exec).
// It's the same as running `ssh exe.dev <cmd>` but auth'd via a bearer token,
// so exebox can automate lobby commands like `domain add` without SSH access.
//
// Token source (checked in order):
//   - config.json "api_token" field (set via exebox config set-token)
//   - $EXE_API_TOKEN env var
//
// Create one scoped to just the commands you need (e.g. domain add) — see
// https://exe.dev/docs/https-api.md
package exeapi

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Error is a typed error wrapping a non-OK HTTP response from the exec API.
// It lets callers distinguish auth/permanent failures from transient ones
// (e.g. DNS propagation) that are worth retrying.
type Error struct {
	Status int
	Body   string
}

func (e *Error) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "api token invalid or expired (status 401)"
	case http.StatusForbidden:
		return fmt.Sprintf("api token lacks permission (status 403): %s", e.Body)
	default:
		return fmt.Sprintf("exe.dev exec failed (status %d): %s", e.Status, e.Body)
	}
}

// Retryable reports whether an error is worth retrying. Auth failures
// (401/403) are permanent; everything else (DNS propagation, 5xx, network
// blips) is treated as transient.
func Retryable(err error) bool {
	var ae *Error
	if errors.As(err, &ae) {
		return ae.Status != http.StatusUnauthorized && ae.Status != http.StatusForbidden
	}
	return true // network errors etc.
}

// execEndpoint is the exe.dev HTTPS API.
const execEndpoint = "https://exe.dev/exec"

// Client calls the exe.dev exec API with a bearer token.
type Client struct {
	Token string
	HTTP  *http.Client
}

// New returns a client if a token is available, or nil otherwise. It reads the
// token from the given string (typically from config.json), falling back to
// the $EXE_API_TOKEN env var.
func New(tokenFromConfig string) *Client {
	tok := strings.TrimSpace(tokenFromConfig)
	if tok == "" {
		return nil
	}
	return &Client{Token: tok, HTTP: &http.Client{Timeout: 30 * time.Second}}
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
	if resp.StatusCode >= 400 {
		return out, &Error{Status: resp.StatusCode, Body: out}
	}
	return out, nil
}

// DomainAdd registers a custom domain with exe.dev via the HTTPS API.
// Returns the API response text.
func (c *Client) DomainAdd(vm, domain string) (string, error) {
	return c.Exec(fmt.Sprintf("domain add %s %s", vm, domain))
}
// Package reflection talks to the exe.dev VM reflection endpoint
// (https://reflection.int.exe.xyz) to discover this VM's identity: name,
// owner email, and default proxy port.
package reflection

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultBase is the exe.dev reflection endpoint. Override with EXEBOX_REFLECTION_URL
// (useful for tests; never needed in production).
const DefaultBase = "https://reflection.int.exe.xyz"

// VM describes the parts of the reflection response we care about.
type VM struct {
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
}

// Identity aggregates everything devbox learns about the VM from reflection.
type Identity struct {
	Name        string `json:"name"`        // e.g. "nesins-devbox"
	Email       string `json:"email,omitempty"`       // owner email
	DefaultPort int    `json:"default_port"`    // main proxy port exe.dev points at this VM
}

// CNAME returns the public hostname to CNAME custom domains to.
// Confirmed: <vm>.exe.xyz resolves; <vm>.exe.dev does not.
func (i Identity) CNAME() string {
	return i.Name + ".exe.xyz"
}

// Client fetches VM metadata from reflection.
type Client struct {
	Base    string
	HTTP    *http.Client
}

// New returns a reflection client with a sensible timeout.
func New() *Client {
	return &Client{
		Base: envOr("EXEBOX_REFLECTION_URL", DefaultBase),
		HTTP: &http.Client{Timeout: 10 * time.Second},
	}
}

// Integration describes one entry from the reflection /integrations list.
// The Help field often contains the proxy URL (e.g.
// "Cloudflare API via https://cloudflare.int.exe.xyz/ ..."), which callers
// parse to discover the credential-injected base URL.
type Integration struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Help    string `json:"help"`
	Comment string `json:"comment"`
}

// Integration looks up an attached integration by name. Returns (nil, nil) if
// the integration is not attached; an error only if reflection is unreachable.
func (c *Client) Integration(ctx context.Context, name string) (*Integration, error) {
	var resp struct {
		Integrations []Integration `json:"integrations"`
	}
	if err := c.getJSON(ctx, "/integrations", &resp); err != nil {
		return nil, err
	}
	for i := range resp.Integrations {
		if resp.Integrations[i].Name == name {
			return &resp.Integrations[i], nil
		}
	}
	return nil, nil
}

// Discover fetches the full VM identity (name + email + default port).
// All three endpoints must succeed; missing any is treated as an error since
// devbox's whole UX assumes it knows the VM's name and port.
func (c *Client) Discover(ctx context.Context) (Identity, error) {
	vm, err := c.vm(ctx)
	if err != nil {
		return Identity{}, fmt.Errorf("reflection name: %w", err)
	}
	if vm.Name == "" {
		return Identity{}, fmt.Errorf("reflection returned empty VM name")
	}

	port, err := c.defaultPort(ctx)
	if err != nil {
		return Identity{}, fmt.Errorf("reflection default_port: %w", err)
	}

	email, _ := c.email(ctx) // optional; ignore failure

	return Identity{Name: vm.Name, Email: email, DefaultPort: port}, nil
}

func (c *Client) vm(ctx context.Context) (VM, error) {
	var v VM
	return v, c.getJSON(ctx, "/", &v)
}

func (c *Client) defaultPort(ctx context.Context) (int, error) {
	var p struct {
		DefaultPort int `json:"default_port"`
	}
	if err := c.getJSON(ctx, "/default_port", &p); err != nil {
		return 0, err
	}
	return p.DefaultPort, nil
}

func (c *Client) email(ctx context.Context) (string, error) {
	var e struct {
		Email string `json:"email"`
	}
	if err := c.getJSON(ctx, "/email", &e); err != nil {
		return "", err
	}
	return e.Email, nil
}

func (c *Client) getJSON(ctx context.Context, path string, dst any) error {
	base := strings.TrimRight(c.Base, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

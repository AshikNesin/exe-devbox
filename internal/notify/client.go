// Package notify sends push notifications to the VM owner's devices via the
// exe.dev "notify" integration. The endpoint is discovered at runtime from the
// reflection endpoint's integration list; if the integration isn't attached,
// all calls are no-ops (the caller never sees an error).
//
// This keeps devbox quiet on VMs that opted out of push notifications.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ashiknesin/exe-devbox/internal/reflection"
)

// DefaultBase is the conventional exe.dev notify endpoint.
const DefaultBase = "https://notify.int.exe.xyz"

// Client posts messages to the exe.dev notify endpoint. If the integration
// isn't attached to this VM, Available() is false and Send is a no-op.
type Client struct {
	Base   string
	HTTP   *http.Client
	once   sync.Once
	avail  bool
}

// global singleton so callers don't re-query reflection on every notification.
var global = &Client{HTTP: &http.Client{Timeout: 8 * time.Second}}

// Default returns the shared singleton client.
func Default() *Client { return global }

// available caches a single reflection lookup (the integration list doesn't
// change during a single CLI invocation).
func (c *Client) available() bool {
	c.once.Do(func() {
		if env := strings.TrimSpace(os.Getenv("EXEBOX_NOTIFY_URL")); env != "" {
			c.Base = env
			c.avail = true
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		in, err := reflection.New().Integration(ctx, "notify")
		if err != nil || in == nil {
			c.avail = false
			return
		}
		c.Base = DefaultBase
		c.avail = true
	})
	return c.avail
}

// Available reports whether the notify integration is attached to this VM.
func (c *Client) Available() bool {
	if c == nil {
		return false
	}
	return c.available()
}

// Send posts a push notification. Returns nil (no-op) if the integration is
// not attached, so callers can call it unconditionally without checking.
func (c *Client) Send(ctx context.Context, title, message string) error {
	if !c.Available() {
		return nil // integration not attached — skip silently
	}
	return c.send(ctx, title, message, "")
}

// SendWithURL is like Send but includes a tappable URL in the notification.
func (c *Client) SendWithURL(ctx context.Context, title, message, url string) error {
	if !c.Available() {
		return nil
	}
	return c.send(ctx, title, message, url)
}

func (c *Client) send(ctx context.Context, title, message, url string) error {
	payload := map[string]string{"title": title, "message": message}
	if url != "" {
		payload["url"] = url
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("notify: %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return nil
}

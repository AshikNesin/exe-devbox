// Package deps installs the toolchain devbox needs: Node.js (latest LTS),
// portless, and nginx. Each installer is idempotent: if the tool is already
// present (and meets the bar), it's a no-op.
//
// Strategy:
//   - node:   prefer NodeSource's apt repo for a system-wide /usr/bin/node
//     (services need an absolute path; nvm paths are per-user and drift).
//   - portless: `npm i -g portless` via the node/npm above.
//   - nginx:  apt-get install -y nginx.
package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/ashiknesin/exe-devbox/internal/system"
)

// NodeLTSResolution is the latest LTS release from nodejs.org/dist/index.json.
type NodeLTSResolution struct {
	Version string // e.g. "v22.11.0"
	LTS     string // e.g. "Jod"
}

// LatestLTS queries nodejs.org for the newest LTS release.
func LatestLTS(ctx context.Context) (NodeLTSResolution, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://nodejs.org/dist/index.json", nil)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return NodeLTSResolution{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return NodeLTSResolution{}, fmt.Errorf("nodejs.org/dist/index.json: %s", resp.Status)
	}
	var entries []struct {
		Version string `json:"version"`
		LTS     any    `json:"lts"` // false for non-LTS, string name for LTS
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return NodeLTSResolution{}, err
	}
	for _, e := range entries { // index.json is newest-first
		if s, ok := e.LTS.(string); ok && s != "" {
			return NodeLTSResolution{Version: e.Version, LTS: s}, nil
		}
	}
	return NodeLTSResolution{}, fmt.Errorf("no LTS release found")
}

// InstalledNode returns the node version string if present ("v22.11.0"), else "".
func InstalledNode() string {
	out, err := exec.Command("node", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// InstallNode installs Node.js LTS via the NodeSource setup script + apt.
// Idempotent: if InstalledNode() already returns a version, it's a no-op
// (unless force). Returns the version present after install.
func InstallNode(ctx context.Context, force bool) (string, error) {
	if v := InstalledNode(); v != "" && !force {
		return v, nil
	}
	// NodeSource setup installs nodejs package providing /usr/bin/node + npm.
	setup := "curl -fsSL https://deb.nodesource.com/setup_lts.x | bash -"
	if out, err := system.AsRoot("bash", "-c", setup).CombinedOutput(); err != nil {
		return "", fmt.Errorf("nodesource setup: %w: %s", err, out)
	}
	if out, err := system.AsRoot("apt-get", "install", "-y", "nodejs").CombinedOutput(); err != nil {
		return "", fmt.Errorf("apt install nodejs: %w: %s", err, out)
	}
	return InstalledNode(), nil
}

// InstallPortless runs `npm i -g portless`. Assumes node/npm are installed.
func InstallPortless(ctx context.Context) error {
	out, err := exec.Command("npm", "i", "-g", "portless").CombinedOutput()
	if err != nil {
		return fmt.Errorf("npm i -g portless: %w: %s", err, out)
	}
	return nil
}

// InstallNginx installs nginx via apt if not already on PATH.
func InstallNginx(ctx context.Context) error {
	if _, err := exec.LookPath("nginx"); err == nil {
		return nil
	}
	// apt update can be slow; run separately so failure is clear.
	if out, err := system.AsRoot("apt-get", "update").CombinedOutput(); err != nil {
		return fmt.Errorf("apt-get update: %w: %s", err, out)
	}
	if out, err := system.AsRoot("apt-get", "install", "-y", "nginx").CombinedOutput(); err != nil {
		return fmt.Errorf("apt install nginx: %w: %s", err, out)
	}
	return nil
}

// drain reads and discards a body (kept for completeness).
func drain(b io.Closer) { _, _ = io.Copy(io.Discard, b.(io.Reader)) }

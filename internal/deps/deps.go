// Package deps installs the toolchain exebox needs: Node.js (latest LTS),
// portless, and nginx. Each installer is idempotent: if the tool is already
// present, it's a no-op.
//
// Dependency chain: node → portless (portless is an npm package). nginx is
// independent.
//
// Strategy:
//   - node:     NodeSource's apt repo for a system-wide /usr/bin/node + npm
//     (services need absolute paths; nvm paths are per-user and drift).
//   - portless: `npm i -g portless` via the node/npm above.
//   - nginx:    apt-get install -y nginx.
package deps

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ashiknesin/exebox/internal/system"
)

// installedVersion returns the version string for a binary (e.g. "v22.11.0"),
// or "" if not on PATH. The --version flag is passed.
func installedVersion(bin string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// onPath reports whether a binary is on $PATH.
func onPath(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// InstallNode installs Node.js LTS (system-wide via NodeSource) if node or npm
// is missing. Idempotent: if both are already on PATH, it's a no-op (unless
// force). Returns the installed node version.
func InstallNode(ctx context.Context, force bool) (string, error) {
	if !force && onPath("node") && onPath("npm") {
		return installedVersion("node"), nil
	}
	// NodeSource setup configures the apt repo for the latest Node.js LTS,
	// then apt installs nodejs (provides both /usr/bin/node and /usr/bin/npm).
	setup := "curl -fsSL https://deb.nodesource.com/setup_lts.x | bash -"
	if out, err := system.AsRoot("bash", "-c", setup).CombinedOutput(); err != nil {
		return "", fmt.Errorf("nodesource setup: %w: %s", err, out)
	}
	if out, err := system.AsRoot("apt-get", "install", "-y", "nodejs").CombinedOutput(); err != nil {
		return "", fmt.Errorf("apt install nodejs: %w: %s", err, out)
	}
	return installedVersion("node"), nil
}

// InstallPortless runs `npm i -g portless` if portless is not already on PATH.
// Needs root because NodeSource installs globals into /usr/lib/node_modules
// (not user-writable). Requires node + npm (install via InstallNode first).
func InstallPortless(ctx context.Context) error {
	if onPath("portless") {
		return nil
	}
	if !onPath("npm") {
		return fmt.Errorf("npm not found — install Node.js first (InstallNode)")
	}
	cmd := system.AsRoot("npm", "i", "-g", "portless")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("npm i -g portless: %w: %s", err, out)
	}
	return nil
}

// InstallNginx installs nginx via apt if not already on PATH.
func InstallNginx(ctx context.Context) error {
	if onPath("nginx") {
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


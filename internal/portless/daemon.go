// Package portless manages the one shared portless daemon on :8888 (HTTP, no
// TLS) that all project dev servers register routes on.
//
// We delegate systemd unit creation to `portless service install`, which writes
// a correct unit (absolute nvm node path, the right env vars) — better than
// hand-rolling a unit file. setup calls EnsureDaemon; the daemon is idempotent:
// if it's already active on the right port, we leave it alone.
package portless

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ashiknesin/exebox/internal/system"
)

// DaemonPort is the shared portless port the whole architecture assumes.
const DaemonPort = 8888

// UnitName is the systemd unit portless service install creates.
const UnitName = "portless"

// ServiceActive reports whether the portless unit is active.
func ServiceActive() bool {
	out, err := exec.Command("systemctl", "is-active", UnitName).Output()
	return err == nil && strings.TrimSpace(string(out)) == "active"
}

// EnsureDaemon installs + enables + starts the portless daemon on :8888 HTTP.
// Idempotent: if already active, returns nil. It first removes any prior HTTPS
// (port 443) service so there's no stale unit.
func EnsureDaemon() error {
	if ServiceActive() {
		return nil
	}
	// Clean up a prior HTTPS/443 service if present (from the reference doc era).
	_ = exec.Command("portless", "service", "uninstall").Run()

	// Install on 8888, HTTP, no TLS. Needs root (it binds a service port +
	// writes a root-owned systemd unit). portless service install is itself
	// sudo-aware internally, but we wrap it for clarity.
	cmd := system.AsRoot("portless", "service", "install", "--no-tls", "--port", fmt.Sprint(DaemonPort))
	cmd.Env = append(cmd.Environ(), "PORTLESS_PORT="+fmt.Sprint(DaemonPort), "PORTLESS_HTTPS=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("portless service install: %w: %s", err, out)
	}

	// Make sure it's enabled + started.
	if out, err := system.AsRoot("systemctl", "enable", "--now", UnitName).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable portless: %w: %s", err, out)
	}
	return nil
}

// List routes prints `portless list` (route health). Returns its output.
func List() (string, error) {
	out, err := exec.Command("portless", "list").CombinedOutput()
	return string(out), err
}

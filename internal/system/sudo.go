package system

import (
	"os"
	"os/exec"
)

// AsRoot runs a command with sudo if we're not already root. If sudo is
// unavailable or the user cancels, the error is returned. The caller decides
// whether that's fatal.
func AsRoot(cmd string, args ...string) *exec.Cmd {
	if os.Geteuid() == 0 {
		return exec.Command(cmd, args...)
	}
	return exec.Command("sudo", append([]string{"--", cmd}, args...)...)
}

// NeedRoot reports whether we'd need sudo (i.e. not currently root).
func NeedRoot() bool { return os.Geteuid() != 0 }

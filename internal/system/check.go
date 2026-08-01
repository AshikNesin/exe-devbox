// Package system wraps the OS-level probes exebox needs: binary lookups,
// systemd unit state, and whether a port is listening. doctor and setup both
// call into these so health checks aren't duplicated.
package system

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Check is one row in a doctor report.
type Check struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"` // "ok" value or failure reason
}

// BinaryOnPATH reports whether name is executable on $PATH (and where).
func BinaryOnPATH(name string) Check {
	path, err := exec.LookPath(name)
	if err != nil {
		return Check{Name: name + " on PATH", Pass: false, Detail: "not found"}
	}
	return Check{Name: name + " on PATH", Pass: true, Detail: path}
}

// UnitActive reports whether the named systemd unit is active.
func UnitActive(unit string) Check {
	out, err := exec.Command("systemctl", "is-active", unit).Output()
	state := strings.TrimSpace(string(out))
	if err != nil {
		return Check{Name: "systemctl " + unit, Pass: false, Detail: state}
	}
	return Check{Name: "systemctl " + unit, Pass: true, Detail: state}
}

// PortListening reports whether anything is listening on 127.0.0.1:port (TCP).
func PortListening(port int) Check {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return Check{Name: fmt.Sprintf("port :%d listening", port), Pass: false, Detail: "not listening"}
	}
	_ = conn.Close()
	return Check{Name: fmt.Sprintf("port :%d listening", port), Pass: true, Detail: "127.0.0.1:" + strconv.Itoa(port)}
}

// Package output centralizes user-facing printing: ANSI colors (auto-disabled
// when not a TTY or --json), and the --json envelope used by every command.
//
// Two modes:
//   - Human (default): colored, copy-paste friendly. Use Step/OK/Info/Warn/Err.
//   - JSON (--json):    one JSON object on stdout per command, no color/spinners.
//     Commands build a Result struct and call Print.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Mode controls how output is rendered. Fields are named OutW/ErrW to avoid
// clashing with the Err() / Plain() printer methods.
type Mode struct {
	JSON  bool
	Color bool
	OutW  io.Writer
	ErrW  io.Writer
}

// Global is configured by root cmd from flags + TTY detection.
var Global = New()

// New returns the default human-mode writer (color iff stdout is a TTY).
func New() *Mode {
	color := isTerminal(os.Stdout)
	return &Mode{Color: color, OutW: os.Stdout, ErrW: os.Stderr}
}

// Result is the --json envelope. Exit=0 success; non-zero error with Message.
type Result struct {
	OK      bool   `json:"ok"`
	Exit    int    `json:"exit,omitempty"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// Print writes a result: JSON to stdout in JSON mode, else nothing (human
// output was already printed during the command).
func (m *Mode) Print(r Result) {
	if !m.JSON {
		return
	}
	enc := json.NewEncoder(m.OutW)
	enc.SetIndent("", "  ")
	_ = enc.Encode(r)
}

// --- human-mode line printers (all go to stderr; stdout reserved for payload) ---

func (m *Mode) Step(format string, a ...any) {
	if m.JSON {
		return
	}
	m.fprintln(m.ErrW, m.c(cyan, "▶ ")+fmt.Sprintf(format, a...))
}

func (m *Mode) OK(format string, a ...any) {
	if m.JSON {
		return
	}
	m.fprintln(m.ErrW, m.c(green, "✓ ")+fmt.Sprintf(format, a...))
}

func (m *Mode) Info(format string, a ...any) {
	if m.JSON {
		return
	}
	m.fprintln(m.ErrW, "  "+fmt.Sprintf(format, a...))
}

func (m *Mode) Warn(format string, a ...any) {
	if m.JSON {
		return
	}
	m.fprintln(m.ErrW, m.c(yellow, "! ")+fmt.Sprintf(format, a...))
}

func (m *Mode) Err(format string, a ...any) {
	if m.JSON {
		return
	}
	m.fprintln(m.ErrW, m.c(red, "✗ ")+fmt.Sprintf(format, a...))
}

// Line prints an already-formatted line to stderr in human mode. Used by
// doctor's table where the caller owns the layout. Suppressed in JSON mode.
func (m *Mode) Line(s string) {
	if m.JSON {
		return
	}
	m.fprintln(m.ErrW, s)
}

// Plain prints to stdout (for real payload in human mode: e.g. a generated URL
// the user pipes/ copies). Suppressed in JSON mode where it'd corrupt the envelope.
func (m *Mode) Plain(format string, a ...any) {
	if m.JSON {
		return
	}
	fmt.Fprintf(m.OutW, format, a...)
}

func (m *Mode) fprintln(w io.Writer, s string) {
	fmt.Fprintln(w, s)
}

// Block prints a copy-paste block: indented, dim, already-formatted text.
func (m *Mode) Block(s string) {
	if m.JSON {
		return
	}
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		m.fprintln(m.ErrW, dim+"    "+line+reset)
	}
}

// Heading prints a section heading in human mode.
func (m *Mode) Heading(s string) {
	if m.JSON {
		return
	}
	m.fprintln(m.ErrW, "")
	m.fprintln(m.ErrW, m.c(bold, s))
}

// --- color primitives ---

const (
	reset  = "\033[0m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
	dim    = "\033[2m"
	bold   = "\033[1m"
)

func (m *Mode) c(color, s string) string {
	if !m.Color {
		return s
	}
	return color + s + reset
}

// Bold wraps text in bold (no color).
func (m *Mode) Bold(s string) string { return m.c(bold, s) }

// Green/Red/Yellow/Cyan colorize a string when color is on.
func (m *Mode) Green(s string) string  { return m.c(green, s) }
func (m *Mode) Red(s string) string    { return m.c(red, s) }
func (m *Mode) Yellow(s string) string { return m.c(yellow, s) }
func (m *Mode) Cyan(s string) string   { return m.c(cyan, s) }

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

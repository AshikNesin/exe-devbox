package output

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Spinner shows an animated spinner on stderr while work is happening.
// When done, the spinner line is replaced by a ✓/✗ result line.
//
// Auto-disabled in JSON mode, when stderr is not a TTY, or when the
// terminal doesn't support cursor movement (TERM=dumb, web terminals).
// Falls back to a simple static "▶ ..." line in those cases.
//
// Usage:
//
//	s := output.Global.Spinner("installing node")
//	// ... do work ...
//	s.OK("node %s installed", version) // or s.Fail(err)
type Spinner struct {
	m        *Mode
	label    string
	stop     chan struct{}
	done     bool
	mu       sync.Mutex
	frame    int
	animated bool // true if the goroutine is running
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// canAnimate reports whether the terminal supports the \r-based line
// rewriting that the spinner needs. Returns false for TERM=dumb, empty,
// or "none" — these terminals render \r as a newline, causing each frame
// to print on its own line.
func canAnimate() bool {
	switch os.Getenv("TERM") {
	case "dumb", "", "none":
		return false
	}
	return true
}

// Spinner starts an animated spinner with the given label.
func (m *Mode) Spinner(format string, a ...any) *Spinner {
	label := fmt.Sprintf(format, a...)
	s := &Spinner{m: m, label: label, stop: make(chan struct{})}

	// Animate only when: not JSON, color is on, and terminal can handle \r.
	if m.JSON || !m.Color || !canAnimate() {
		m.Step("%s", label)
		return s
	}

	s.animated = true
	go s.animate()
	return s
}

func (s *Spinner) animate() {
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			frame := spinnerFrames[s.frame%len(spinnerFrames)]
			s.frame++
			s.mu.Unlock()
			fmt.Fprintf(s.m.ErrW, "\r\033[36m%s\033[0m %s\033[K", frame, s.label)
		}
	}
}

// stop halts the animation and clears the line.
func (s *Spinner) stop_() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.done = true
	close(s.stop)
	if s.animated {
		fmt.Fprint(s.m.ErrW, "\r\033[K")
	}
}

// OK stops the spinner and prints a ✓ success line.
func (s *Spinner) OK(format string, a ...any) {
	s.stop_()
	s.m.OK(format, a...)
}

// Fail stops the spinner and prints a ✗ error line.
func (s *Spinner) Fail(format string, a ...any) {
	s.stop_()
	s.m.Err(format, a...)
}

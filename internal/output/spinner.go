package output

import (
	"fmt"
	"sync"
	"time"
)

// Spinner shows an animated spinner on stderr while work is happening.
// When done, the spinner line is replaced by a ✓/✗ result line.
//
// Auto-disabled in JSON mode and when stderr is not a TTY.
//
// Usage:
//   s := output.Global.Spinner("installing node")
//   // ... do work ...
//   s.OK("node %s installed", version)  // or s.Fail(err)
type Spinner struct {
	m       *Mode
	label   string
	stop    chan struct{}
	done    bool
	mu      sync.Mutex
	frame   int
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner starts an animated spinner with the given label.
func (m *Mode) Spinner(format string, a ...any) *Spinner {
	label := fmt.Sprintf(format, a...)
	s := &Spinner{m: m, label: label, stop: make(chan struct{})}
	if m.JSON || !m.Color {
		// Non-interactive: just print the step line, no animation.
		m.Step("%s", label)
		return s
	}
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
			// \r returns to line start; [K clears to end of line
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
	if s.m.Color {
		fmt.Fprint(s.m.ErrW, "\r\033[K") // clear spinner line
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

// Done is an alias for OK.
func (s *Spinner) Done(format string, a ...any) { s.OK(format, a...) }

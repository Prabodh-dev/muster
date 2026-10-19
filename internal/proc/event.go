// Package proc spawns the configured processes, captures their output and
// reports everything that happens to them as events on a single channel.
package proc

import "fmt"

// Event is implemented by every message a process can emit.
type Event interface {
	// ProcName is the name of the process the event belongs to.
	ProcName() string
}

// LineEvent carries one line of combined stdout/stderr output.
type LineEvent struct {
	Proc string
	Text string
}

// ExitEvent is sent exactly once per process, after its output has been
// drained. Code is the exit code, or -1 when the process was killed by a
// signal or never started.
type ExitEvent struct {
	Proc string
	Code int
	Err  error // non-nil for start failures and abnormal terminations
}

func (e LineEvent) ProcName() string { return e.Proc }
func (e ExitEvent) ProcName() string { return e.Proc }

// Crashed reports whether the exit counts as a crash rather than a clean exit.
func (e ExitEvent) Crashed() bool { return e.Code != 0 || e.Err != nil }

func (e ExitEvent) String() string {
	switch {
	case e.Err != nil:
		return fmt.Sprintf("%s: %v", e.Proc, e.Err)
	default:
		return fmt.Sprintf("%s: exit %d", e.Proc, e.Code)
	}
}

func itoa(n int) string { return fmt.Sprint(n) }

// Package proc spawns the configured processes, captures their output and
// reports everything that happens to them as events on a single channel.
package proc

import (
	"fmt"
	"strconv"
	"time"
)

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

// StartEvent is sent each time the process is launched.
type StartEvent struct {
	Proc     string
	PID      int
	Restarts int // how many times it has been relaunched so far
	At       time.Time
}

// StatsEvent is a periodic resource sample for a running process and its
// descendants.
type StatsEvent struct {
	Proc  string
	CPU   float64 // percent of one core, summed over the tree
	RSS   uint64  // resident bytes, summed over the tree
	Procs int     // processes in the tree
}

// ExitEvent is sent once per run, after the process's output has been
// drained. Code is the exit code, or -1 when the process was killed by a
// signal or never started.
type ExitEvent struct {
	Proc    string
	Code    int
	Err     error         // non-nil for start failures and abnormal terminations
	Stopped bool          // the exit was requested by the user (or a restart)
	RetryIn time.Duration // > 0 when an automatic restart has been scheduled
	Attempt int           // which consecutive restart attempt RetryIn is for
	Max     int           // the policy's limit on consecutive attempts
	GaveUp  bool          // the restart policy ran out of attempts
}

func (e LineEvent) ProcName() string  { return e.Proc }
func (e StartEvent) ProcName() string { return e.Proc }
func (e ExitEvent) ProcName() string  { return e.Proc }
func (e StatsEvent) ProcName() string { return e.Proc }

// Crashed reports whether the exit counts as a crash rather than a clean exit
// or a requested stop.
func (e ExitEvent) Crashed() bool { return !e.Stopped && (e.Code != 0 || e.Err != nil) }

func (e ExitEvent) String() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Proc, e.Err)
	}
	return fmt.Sprintf("%s: exit %d", e.Proc, e.Code)
}

// Summary describes the exit in words, for logs.
func (e ExitEvent) Summary() string {
	what := "exited with code " + strconv.Itoa(e.Code)
	if e.Err != nil {
		what = e.Err.Error()
	}
	switch {
	case e.Stopped:
		return "stopped"
	case e.RetryIn > 0:
		return fmt.Sprintf("%s; restart %d/%d in %s", what, e.Attempt, e.Max, e.RetryIn.Round(time.Millisecond))
	case e.GaveUp:
		return fmt.Sprintf("%s; giving up after %d restarts", what, e.Max)
	}
	return what
}

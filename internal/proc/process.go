package proc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Prabodh-dev/muster/internal/config"
)

const (
	// maxLine caps a single emitted line; longer lines are split.
	maxLine = 64 * 1024
	// drainTimeout bounds how long we wait for output to reach EOF after the
	// process itself has exited (a background grandchild can hold the pipe).
	drainTimeout = 2 * time.Second
	// stopGrace is how long Stop waits after the polite signal before killing.
	stopGrace = 2 * time.Second
)

// Process is one running (or finished) child.
type Process struct {
	cfg config.Process

	cmd  *exec.Cmd
	done chan struct{} // closed once the ExitEvent has been delivered
}

// Name returns the configured process name.
func (p *Process) Name() string { return p.cfg.Name }

// spawn starts the child with stdout and stderr redirected into one pipe, and
// starts the goroutines that read it. Events are delivered on out; delivery
// is abandoned if quit is closed so a departed consumer never wedges them.
func spawn(cfg config.Process, out chan<- Event, quit <-chan struct{}) *Process {
	p := &Process{cfg: cfg, done: make(chan struct{})}

	fail := func(err error) *Process {
		go func() {
			defer close(p.done)
			send(out, quit, ExitEvent{Proc: cfg.Name, Code: -1, Err: err})
		}()
		return p
	}

	r, w, err := os.Pipe()
	if err != nil {
		return fail(fmt.Errorf("creating pipe: %w", err))
	}

	cmd := shellCommand(cfg.Command)
	cmd.Dir = cfg.Cwd
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdin = nil
	cmd.Stdout = w
	cmd.Stderr = w
	configureProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return fail(fmt.Errorf("start: %w", err))
	}
	// The child holds its own copy; closing ours lets the reader see EOF.
	w.Close()
	p.cmd = cmd

	pumped := make(chan struct{})
	go func() {
		defer close(pumped)
		pump(r, cfg.Name, out, quit)
	}()

	go func() {
		defer close(p.done)
		waitErr := cmd.Wait()

		select {
		case <-pumped:
		case <-time.After(drainTimeout):
			r.Close() // unblock the reader; some trailing output may be lost
			<-pumped
		}
		r.Close()

		ev := ExitEvent{Proc: cfg.Name, Code: cmd.ProcessState.ExitCode()}
		var exitErr *exec.ExitError
		if waitErr != nil && !errors.As(waitErr, &exitErr) {
			ev.Err = waitErr
		} else if ev.Code == -1 {
			ev.Err = errors.New("terminated by signal")
		}
		send(out, quit, ev)
	}()
	return p
}

// pump reads lines from r until EOF and sends each as a LineEvent.
func pump(r io.Reader, name string, out chan<- Event, quit <-chan struct{}) {
	br := bufio.NewReaderSize(r, 4096)
	var pending strings.Builder
	for {
		chunk, isPrefix, err := br.ReadLine()
		if err != nil {
			if pending.Len() > 0 {
				send(out, quit, LineEvent{Proc: name, Text: pending.String()})
			}
			return
		}
		pending.Write(chunk)
		if isPrefix && pending.Len() < maxLine {
			continue
		}
		send(out, quit, LineEvent{Proc: name, Text: pending.String()})
		pending.Reset()
	}
}

// send delivers ev, giving up if quit is closed. It reports whether the
// event was delivered.
func send(out chan<- Event, quit <-chan struct{}, ev Event) bool {
	select {
	case out <- ev:
		return true
	case <-quit:
		return false
	}
}

// stop asks the process tree to terminate, escalating to a hard kill if it
// has not exited after the grace period, and waits for it to finish.
func (p *Process) stop() {
	if p.cmd == nil || p.cmd.Process == nil {
		<-p.done
		return
	}
	select {
	case <-p.done:
		return
	default:
	}
	terminate(p.cmd)
	select {
	case <-p.done:
	case <-time.After(stopGrace):
		kill(p.cmd)
		<-p.done
	}
}

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
	// defaultGrace is how long a stop waits after the polite signal before killing.
	defaultGrace = 5 * time.Second
)

// instance is one running (or finished) child process.
type instance struct {
	cmd  *exec.Cmd
	pid  int
	done chan struct{} // closed once the child has exited and its output is drained
	res  exitInfo      // valid after done is closed
}

type exitInfo struct {
	code int
	err  error
}

// startInstance launches the child with stdout and stderr redirected into one
// pipe and starts the goroutine that reads it. started is called with the pid
// before any output is delivered. Events go to out; delivery is abandoned if
// quit is closed so a departed consumer never wedges the reader.
func startInstance(cfg config.Process, out chan<- Event, quit <-chan struct{}, started func(pid int)) (*instance, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("creating pipe: %w", err)
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
		return nil, fmt.Errorf("start: %w", err)
	}
	// The child holds its own copy; closing ours lets the reader see EOF.
	w.Close()

	inst := &instance{cmd: cmd, pid: cmd.Process.Pid, done: make(chan struct{})}
	started(inst.pid)

	pumped := make(chan struct{})
	go func() {
		defer close(pumped)
		pump(r, cfg.Name, out, quit)
	}()

	go func() {
		defer close(inst.done)
		waitErr := cmd.Wait()

		select {
		case <-pumped:
		case <-time.After(drainTimeout):
			r.Close() // unblock the reader; some trailing output may be lost
			<-pumped
		}
		r.Close()

		inst.res.code = cmd.ProcessState.ExitCode()
		var exitErr *exec.ExitError
		if waitErr != nil && !errors.As(waitErr, &exitErr) {
			inst.res.err = waitErr
		} else if inst.res.code == -1 {
			inst.res.err = errors.New("terminated by signal")
		}
	}()
	return inst, nil
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
func (i *instance) stop(grace time.Duration) {
	select {
	case <-i.done:
		return
	default:
	}
	terminate(i.cmd)
	select {
	case <-i.done:
	case <-time.After(grace):
		kill(i.cmd)
		<-i.done
	}
}

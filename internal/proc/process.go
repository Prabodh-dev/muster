package proc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
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
	// pollInterval is how often a stop checks whether the group has emptied.
	pollInterval = 20 * time.Millisecond
)

// instance is one running (or finished) child process.
type instance struct {
	cmd   *exec.Cmd
	pid   int
	group *group

	leaderExited chan struct{} // closed when the top-level process has exited
	done         chan struct{} // closed once everything has exited and output is drained
	res          exitInfo      // valid after done is closed

	mu       sync.Mutex
	stopping bool // a stop is in progress and owns cleaning up the group
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

	grp, err := newGroup(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		r.Close()
		return nil, err
	}
	inst := &instance{
		cmd:          cmd,
		pid:          cmd.Process.Pid,
		group:        grp,
		leaderExited: make(chan struct{}),
		done:         make(chan struct{}),
	}
	started(inst.pid)

	pumped := make(chan struct{})
	go func() {
		defer close(pumped)
		pump(r, cfg.Name, out, quit)
	}()

	go func() {
		defer close(inst.done)
		waitErr := cmd.Wait()
		close(inst.leaderExited)

		// If the top-level process left on its own, anything it started is
		// an orphan nobody will clean up: sweep it. During a stop, stop()
		// is giving the group its grace period instead.
		inst.mu.Lock()
		stopping := inst.stopping
		inst.mu.Unlock()
		if !stopping {
			inst.group.kill()
		}

		select {
		case <-pumped:
		case <-time.After(drainTimeout):
			r.Close() // unblock the reader; some trailing output may be lost
			<-pumped
		}
		r.Close()
		inst.group.close()

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

// stop asks the whole process group to terminate and waits up to grace for
// every member to go away, then force-kills whatever is left. It returns once
// the instance is completely done.
func (i *instance) stop(grace time.Duration) {
	select {
	case <-i.done:
		return
	default:
	}
	i.mu.Lock()
	i.stopping = true
	i.mu.Unlock()

	if err := i.group.terminate(); err != nil {
		// No way to ask politely (e.g. no console to signal): don't wait.
		grace = 0
	}
	expire := time.NewTimer(grace)
	defer expire.Stop()
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
wait:
	for {
		if i.leaderGone() && i.group.empty() {
			break
		}
		select {
		case <-expire.C:
			break wait
		case <-tick.C:
		}
	}
	// Final sweep: a no-op when everything exited in time.
	i.group.kill()
	<-i.done
}

func (i *instance) leaderGone() bool {
	select {
	case <-i.leaderExited:
		return true
	default:
		return false
	}
}

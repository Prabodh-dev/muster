package proc

import (
	"sync/atomic"
	"time"

	"github.com/Prabodh-dev/muster/internal/config"
)

type command int

const (
	cmdStart command = iota
	cmdStop
	cmdRestart
)

// managed owns the lifecycle of one configured process. A single goroutine
// (run) holds all of its state, so commands from the UI never race with the
// process exiting.
type managed struct {
	cfg      config.Process
	out      chan<- Event
	quit     <-chan struct{}
	cmds     chan command
	finished chan struct{}
	tm       timing

	pid atomic.Int64 // pid of the running child, or 0
}

func newManaged(cfg config.Process, out chan<- Event, quit <-chan struct{}, tm timing) *managed {
	return &managed{
		tm:       tm,
		cfg:      cfg,
		out:      out,
		quit:     quit,
		cmds:     make(chan command, 8),
		finished: make(chan struct{}),
	}
}

// command queues a lifecycle command without ever blocking the caller.
func (m *managed) command(c command) {
	select {
	case m.cmds <- c:
	default:
	}
}

func (m *managed) emit(ev Event) { send(m.out, m.quit, ev) }

func (m *managed) grace() time.Duration {
	if m.cfg.GracePeriod < 0 {
		return defaultGrace
	}
	return m.cfg.GracePeriod
}

// backoff is the delay before restart attempt n (1-based): base, 2*base,
// 4*base, ... capped at backoffMax.
func (t timing) backoff(n int) time.Duration {
	d := t.backoffBase
	for i := 1; i < n; i++ {
		d *= 2
		if d >= t.backoffMax {
			return t.backoffMax
		}
	}
	return min(d, t.backoffMax)
}

func (m *managed) wantsRestart(res exitInfo) bool {
	switch m.cfg.Restart {
	case config.RestartAlways:
		return true
	case config.RestartOnFailure:
		return res.code != 0 || res.err != nil
	default:
		return false
	}
}

func (m *managed) run() {
	defer close(m.finished)

	var (
		inst      *instance
		startedAt time.Time
		launches  int
		failures  int // consecutive automatic restarts without a stable run
		retry     *time.Timer
		retryC    <-chan time.Time
	)

	cancelRetry := func() {
		if retry != nil {
			retry.Stop()
			retry, retryC = nil, nil
		}
	}
	// exited reports a finished run and, per the restart policy, schedules
	// the next one.
	exited := func(res exitInfo, ran time.Duration) {
		ev := ExitEvent{Proc: m.cfg.Name, Code: res.code, Err: res.err}
		if m.wantsRestart(res) {
			if ran >= m.tm.stableAfter {
				failures = 0
			}
			failures++
			if failures > m.cfg.MaxRestarts {
				ev.GaveUp, ev.Max = true, m.cfg.MaxRestarts
			} else {
				ev.RetryIn = m.tm.backoff(failures)
				ev.Attempt, ev.Max = failures, m.cfg.MaxRestarts
				retry = time.NewTimer(ev.RetryIn)
				retryC = retry.C
			}
		}
		m.emit(ev)
	}
	start := func() {
		var err error
		inst, err = startInstance(m.cfg, m.out, m.quit, func(pid int) {
			m.pid.Store(int64(pid))
			m.emit(StartEvent{Proc: m.cfg.Name, PID: pid, Restarts: launches, At: time.Now()})
		})
		launches++
		startedAt = time.Now()
		if err != nil {
			inst = nil
			m.pid.Store(0)
			exited(exitInfo{code: -1, err: err}, 0)
		}
	}
	// halt stops the running child at the user's request.
	halt := func() {
		inst.stop(m.grace())
		m.emit(ExitEvent{Proc: m.cfg.Name, Code: inst.res.code, Stopped: true})
		inst = nil
		m.pid.Store(0)
	}
	// fresh begins a new run on the user's initiative, forgetting past failures.
	fresh := func() {
		cancelRetry()
		failures = 0
		start()
	}

	start()
	for {
		var done <-chan struct{}
		if inst != nil {
			done = inst.done
		}
		select {
		case <-m.quit:
			cancelRetry()
			if inst != nil {
				inst.stop(m.grace())
			}
			return

		case c := <-m.cmds:
			switch c {
			case cmdStop:
				if inst != nil {
					halt()
				} else if retry != nil {
					// Waiting to restart: the user wants it to stay down.
					cancelRetry()
					m.emit(ExitEvent{Proc: m.cfg.Name, Stopped: true})
				}
			case cmdStart:
				if inst == nil {
					fresh()
				}
			case cmdRestart:
				if inst != nil {
					halt()
				}
				fresh()
			}

		case <-done:
			res := inst.res
			inst = nil
			m.pid.Store(0)
			exited(res, time.Since(startedAt))

		case <-retryC:
			retry, retryC = nil, nil
			start()
		}
	}
}

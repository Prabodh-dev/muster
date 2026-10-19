package proc

import (
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
}

func newManaged(cfg config.Process, out chan<- Event, quit <-chan struct{}) *managed {
	return &managed{
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

func (m *managed) grace() time.Duration { return defaultGrace }

func (m *managed) run() {
	defer close(m.finished)

	var inst *instance
	restarts := 0

	start := func() {
		var err error
		inst, err = startInstance(m.cfg, m.out, m.quit, func(pid int) {
			m.emit(StartEvent{Proc: m.cfg.Name, PID: pid, Restarts: restarts})
		})
		if err != nil {
			inst = nil
			m.emit(ExitEvent{Proc: m.cfg.Name, Code: -1, Err: err})
		}
	}
	// halt stops the running child at the user's request.
	halt := func() {
		inst.stop(m.grace())
		m.emit(ExitEvent{Proc: m.cfg.Name, Code: inst.res.code, Stopped: true})
		inst = nil
	}

	start()
	for {
		var done <-chan struct{}
		if inst != nil {
			done = inst.done
		}
		select {
		case <-m.quit:
			if inst != nil {
				inst.stop(m.grace())
			}
			return

		case c := <-m.cmds:
			switch c {
			case cmdStop:
				if inst != nil {
					halt()
				}
			case cmdStart:
				if inst == nil {
					start()
				}
			case cmdRestart:
				if inst != nil {
					halt()
				}
				restarts++
				start()
			}

		case <-done:
			res := inst.res
			inst = nil
			m.emit(ExitEvent{Proc: m.cfg.Name, Code: res.code, Err: res.err})
		}
	}
}

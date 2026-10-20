package proc

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/Prabodh-dev/muster/internal/config"
	"github.com/Prabodh-dev/muster/internal/ringbuf"
)

// eventBuffer is the capacity of the shared event channel. It only smooths
// bursts; producers block (individually) when it is full.
const eventBuffer = 256

// Status is the lifecycle state of a process as seen by the UI.
type Status int

const (
	Running    Status = iota
	Exited            // exited with code 0
	Crashed           // non-zero exit, killed by a signal, or failed to start
	Stopped           // stopped by the user
	Restarting        // exited; waiting out the backoff before relaunching
)

func (s Status) String() string {
	switch s {
	case Running:
		return "running"
	case Exited:
		return "exited"
	case Stopped:
		return "stopped"
	case Restarting:
		return "restarting"
	default:
		return "crashed"
	}
}

// State is the UI-facing view of one process: its status and bounded log.
// It is only mutated through Supervisor.Apply.
type State struct {
	Name     string
	Status   Status
	Code     int
	Restarts int

	mu   sync.Mutex
	logs *ringbuf.Buffer
}

// Lines returns a snapshot of the buffered log, oldest first.
func (s *State) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logs.Lines()
}

// Dropped reports how many old lines have been evicted from the buffer.
func (s *State) Dropped() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logs.Dropped()
}

// Supervisor owns every child process. All of their output and exit events
// are fanned in to the single channel returned by Events.
type Supervisor struct {
	events chan Event
	quit   chan struct{}

	states []*State
	byName map[string]*State
	procs  map[string]*managed

	stopOnce sync.Once
}

// timing controls automatic restarts.
type timing struct {
	backoffBase time.Duration // delay before the first restart; doubles each time
	backoffMax  time.Duration // cap on the delay
	stableAfter time.Duration // a run this long resets the failure count
}

var defaultTiming = timing{backoffBase: time.Second, backoffMax: 30 * time.Second, stableAfter: 10 * time.Second}

// New starts every process in cfg and returns the supervisor.
func New(cfg *config.Config) *Supervisor { return newSupervisor(cfg, defaultTiming) }

func newSupervisor(cfg *config.Config, tm timing) *Supervisor {
	s := &Supervisor{
		events: make(chan Event, eventBuffer),
		quit:   make(chan struct{}),
		byName: map[string]*State{},
		procs:  map[string]*managed{},
	}
	for _, pc := range cfg.Processes {
		st := &State{Name: pc.Name, logs: ringbuf.New(cfg.BufferLines)}
		s.states = append(s.states, st)
		s.byName[pc.Name] = st
	}
	for _, pc := range cfg.Processes {
		m := newManaged(pc, s.events, s.quit, tm)
		s.procs[pc.Name] = m
		go m.run()
	}
	return s
}

// Events is the fan-in channel: many producers, one consumer.
func (s *Supervisor) Events() <-chan Event { return s.events }

// States returns the per-process states in config order.
func (s *Supervisor) States() []*State { return s.states }

// Apply folds an event into the state it belongs to. It must be called from
// the single consumer of Events.
func (s *Supervisor) Apply(ev Event) {
	st := s.byName[ev.ProcName()]
	if st == nil {
		return
	}
	switch e := ev.(type) {
	case LineEvent:
		st.mu.Lock()
		st.logs.Push(e.Text)
		st.mu.Unlock()
	case StartEvent:
		st.Status = Running
		st.Code = 0
		st.Restarts = e.Restarts
		st.push("[muster] started (pid " + strconv.Itoa(e.PID) + ")")
	case ExitEvent:
		st.Code = e.Code
		switch {
		case e.Stopped:
			st.Status = Stopped
			st.push("[muster] stopped")
		case e.RetryIn > 0:
			st.Status = Restarting
			st.push(fmt.Sprintf("[muster] %s; restart %d/%d in %s", describeExit(e), e.Attempt, e.Max, e.RetryIn.Round(time.Millisecond)))
		case e.GaveUp:
			st.Status = Crashed
			if !e.Crashed() {
				st.Status = Exited
			}
			st.push(fmt.Sprintf("[muster] %s; giving up after %d restarts", describeExit(e), e.Max))
		case e.Crashed():
			st.Status = Crashed
			st.push("[muster] " + describeExit(e))
		default:
			st.Status = Exited
			st.push("[muster] exited with code 0")
		}
	}
}

func describeExit(e ExitEvent) string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "exited with code " + strconv.Itoa(e.Code)
}

func (s *State) push(line string) {
	s.mu.Lock()
	s.logs.Push(line)
	s.mu.Unlock()
}

// Start launches a stopped or exited process. It does nothing if the process
// is already running.
func (s *Supervisor) Start(name string) { s.send(name, cmdStart) }

// Stop gracefully stops one process and leaves the others alone.
func (s *Supervisor) Stop(name string) { s.send(name, cmdStop) }

// Restart stops (if running) and relaunches one process.
func (s *Supervisor) Restart(name string) { s.send(name, cmdRestart) }

func (s *Supervisor) send(name string, c command) {
	if m := s.procs[name]; m != nil {
		m.command(c)
	}
}

// Shutdown terminates every child process tree and waits for them to finish.
// It is safe to call more than once.
func (s *Supervisor) Shutdown() {
	s.stopOnce.Do(func() {
		// Release any producer blocked on a full channel first.
		close(s.quit)
		for _, m := range s.procs {
			<-m.finished
		}
	})
}

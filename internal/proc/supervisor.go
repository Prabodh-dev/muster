package proc

import (
	"sync"

	"github.com/Prabodh-dev/muster/internal/config"
	"github.com/Prabodh-dev/muster/internal/ringbuf"
)

// eventBuffer is the capacity of the shared event channel. It only smooths
// bursts; producers block (individually) when it is full.
const eventBuffer = 256

// Status is the lifecycle state of a process as seen by the UI.
type Status int

const (
	Running Status = iota
	Exited         // exited with code 0
	Crashed        // non-zero exit, killed by a signal, or failed to start
)

func (s Status) String() string {
	switch s {
	case Running:
		return "running"
	case Exited:
		return "exited"
	default:
		return "crashed"
	}
}

// State is the UI-facing view of one process: its status and bounded log.
// It is only mutated through Supervisor.Apply.
type State struct {
	Name   string
	Status Status
	Code   int

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
	procs  []*Process

	stopOnce sync.Once
}

// New starts every process in cfg and returns the supervisor.
func New(cfg *config.Config) *Supervisor {
	s := &Supervisor{
		events: make(chan Event, eventBuffer),
		quit:   make(chan struct{}),
		byName: map[string]*State{},
	}
	for _, pc := range cfg.Processes {
		st := &State{Name: pc.Name, logs: ringbuf.New(cfg.BufferLines)}
		s.states = append(s.states, st)
		s.byName[pc.Name] = st
	}
	for _, pc := range cfg.Processes {
		s.procs = append(s.procs, spawn(pc, s.events, s.quit))
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
	case ExitEvent:
		st.Code = e.Code
		if e.Crashed() {
			st.Status = Crashed
		} else {
			st.Status = Exited
		}
		st.mu.Lock()
		switch {
		case e.Err != nil:
			st.logs.Push("[muster] " + e.Err.Error())
		default:
			st.logs.Push("[muster] exited with code " + itoa(e.Code))
		}
		st.mu.Unlock()
	}
}

// Stop terminates every child process tree and waits for them to finish.
// It is safe to call more than once.
func (s *Supervisor) Stop() {
	s.stopOnce.Do(func() {
		// Release any producer blocked on a full channel first.
		close(s.quit)
		var wg sync.WaitGroup
		for _, p := range s.procs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p.stop()
			}()
		}
		wg.Wait()
	})
}

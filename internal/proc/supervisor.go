package proc

import (
	"strconv"
	"sync"
	"time"

	"github.com/Prabodh-dev/muster/internal/stats"

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

	// StartedAt is when the current run began; it moves forward on every
	// (re)start. Only meaningful while Status is Running.
	StartedAt time.Time

	// Resource use of the running process tree, from the latest sample.
	HasStats bool
	CPU      float64
	RSS      uint64
	Procs    int

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

	sampling chan struct{} // closed when the stats goroutine has exited; nil if off

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
		var fl *fileLog
		if cfg.Logs.Enabled {
			var err error
			if fl, err = openFileLog(cfg.Logs.Dir, pc.Name); err != nil {
				s.byName[pc.Name].push("[muster] cannot write log file, continuing without it: " + err.Error())
				fl = nil
			} else {
				s.byName[pc.Name].push("[muster] logging to " + fl.path)
			}
		}
		m := newManaged(pc, s.events, s.quit, tm, fl)
		s.procs[pc.Name] = m
		go m.run()
	}
	if cfg.StatsInterval > 0 {
		s.sampling = make(chan struct{})
		go s.sampleLoop(cfg.StatsInterval)
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
	case StatsEvent:
		if st.Status == Running {
			st.HasStats, st.CPU, st.RSS, st.Procs = true, e.CPU, e.RSS, e.Procs
		}
	case StartEvent:
		st.Status = Running
		st.Code = 0
		st.Restarts = e.Restarts
		st.StartedAt = e.At
		st.HasStats = false
		st.push("[muster] started (pid " + strconv.Itoa(e.PID) + ")")
	case ExitEvent:
		st.Code = e.Code
		st.HasStats = false
		switch {
		case e.Stopped:
			st.Status = Stopped
		case e.RetryIn > 0:
			st.Status = Restarting
		case e.Crashed():
			st.Status = Crashed
		default:
			st.Status = Exited
		}
		st.push("[muster] " + e.Summary())
	}
}

// Uptime is how long the current run has been going at time now, or zero
// when the process is not running.
func (s *State) Uptime(now time.Time) time.Duration {
	if s.Status != Running || s.StartedAt.IsZero() {
		return 0
	}
	return max(now.Sub(s.StartedAt), 0)
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
		if s.sampling != nil {
			<-s.sampling
		}
	})
}

// sampleLoop periodically measures every running process tree and reports
// the result through the same event channel as everything else.
func (s *Supervisor) sampleLoop(every time.Duration) {
	defer close(s.sampling)
	collector := stats.NewCollector()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-tick.C:
		}
		roots := make([]int, 0, len(s.procs))
		owner := make(map[int]string, len(s.procs))
		for name, m := range s.procs {
			if pid := int(m.pid.Load()); pid != 0 {
				roots = append(roots, pid)
				owner[pid] = name
			}
		}
		for pid, sample := range collector.Sample(roots) {
			ev := StatsEvent{Proc: owner[pid], CPU: sample.CPU, RSS: sample.RSS, Procs: sample.Procs}
			if !send(s.events, s.quit, ev) {
				return
			}
		}
	}
}

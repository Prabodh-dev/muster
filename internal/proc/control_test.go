package proc

import (
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Prabodh-dev/muster/internal/config"
)

// fastTiming keeps restart tests quick.
var fastTiming = timing{backoffBase: 40 * time.Millisecond, backoffMax: 320 * time.Millisecond, stableAfter: time.Hour}

func helperProc(t *testing.T, name, mode string, extra map[string]string) config.Process {
	t.Helper()
	cmd, env := helperCmd(mode)
	for k, v := range extra {
		env[k] = v
	}
	return config.Process{
		Name: name, Command: cmd, Env: env, Cwd: t.TempDir(),
		GracePeriod: 3 * time.Second, Restart: config.RestartNever, MaxRestarts: config.DefaultMaxRestarts,
	}
}

func newTest(t *testing.T, tm timing, procs ...config.Process) *Supervisor {
	t.Helper()
	s := newSupervisor(&config.Config{BufferLines: 200, Processes: procs}, tm)
	t.Cleanup(s.Shutdown)
	return s
}

// run applies events until fn returns true for one of them.
func run(t *testing.T, s *Supervisor, fn func(Event) bool) {
	t.Helper()
	timeout := time.After(15 * time.Second)
	for {
		select {
		case ev := <-s.Events():
			s.Apply(ev)
			if fn(ev) {
				return
			}
		case <-timeout:
			t.Fatal("timed out waiting for events")
		}
	}
}

// settle applies whatever events arrive for d, returning them.
func settle(s *Supervisor, d time.Duration) []Event {
	var evs []Event
	timer := time.After(d)
	for {
		select {
		case ev := <-s.Events():
			s.Apply(ev)
			evs = append(evs, ev)
		case <-timer:
			return evs
		}
	}
}

func waitStart(t *testing.T, s *Supervisor, name string) StartEvent {
	t.Helper()
	var got StartEvent
	run(t, s, func(ev Event) bool {
		se, ok := ev.(StartEvent)
		if ok && se.Proc == name {
			got = se
		}
		return ok && se.Proc == name
	})
	return got
}

func waitExit(t *testing.T, s *Supervisor, name string) ExitEvent {
	t.Helper()
	var got ExitEvent
	run(t, s, func(ev Event) bool {
		ee, ok := ev.(ExitEvent)
		if ok && ee.Proc == name {
			got = ee
		}
		return ok && ee.Proc == name
	})
	return got
}

// waitPID waits for a log line with the given prefix and returns the
// integer after it (e.g. "child=1234").
func waitPID(t *testing.T, s *Supervisor, st *State, prefix string) int {
	t.Helper()
	find := func() (int, bool) {
		for _, l := range st.Lines() {
			if strings.HasPrefix(l, prefix) {
				n, err := strconv.Atoi(strings.TrimPrefix(l, prefix))
				return n, err == nil
			}
		}
		return 0, false
	}
	if n, ok := find(); ok {
		return n
	}
	var n int
	run(t, s, func(Event) bool {
		var ok bool
		n, ok = find()
		return ok
	})
	return n
}

func waitLine(t *testing.T, s *Supervisor, st *State, want string) {
	t.Helper()
	if hasLine(st, want) {
		return
	}
	run(t, s, func(Event) bool { return hasLine(st, want) })
}

func TestControlDoesNotAffectSiblings(t *testing.T) {
	s := newTest(t, defaultTiming,
		helperProc(t, "a", "sleep", nil),
		helperProc(t, "b", "sleep", nil),
		helperProc(t, "c", "sleep", nil),
	)
	pids := map[string]int{}
	for i := 0; i < 3; i++ {
		var se StartEvent
		run(t, s, func(ev Event) bool { se, _ = ev.(StartEvent); return se.PID != 0 })
		pids[se.Proc] = se.PID
	}
	st := s.States()

	// Stop b: only b goes down.
	s.Stop("b")
	if ee := waitExit(t, s, "b"); !ee.Stopped {
		t.Errorf("stop should be reported as requested: %+v", ee)
	}
	awaitDead(t, pids["b"])
	if st[1].Status != Stopped || st[0].Status != Running || st[2].Status != Running {
		t.Errorf("after stop: %v %v %v", st[0].Status, st[1].Status, st[2].Status)
	}
	if !processAlive(pids["a"]) || !processAlive(pids["c"]) {
		t.Error("siblings must survive a stop")
	}

	// Start it again: new pid, others untouched.
	s.Start("b")
	se := waitStart(t, s, "b")
	if se.PID == pids["b"] || st[1].Status != Running {
		t.Errorf("start: pid %d (old %d), status %v", se.PID, pids["b"], st[1].Status)
	}
	pids["b"] = se.PID

	// Restart a: a gets a new pid, b and c keep theirs.
	s.Restart("a")
	if ee := waitExit(t, s, "a"); !ee.Stopped {
		t.Errorf("restart should stop the old process first: %+v", ee)
	}
	se = waitStart(t, s, "a")
	if se.PID == pids["a"] || se.Restarts != 1 {
		t.Errorf("restart: pid %d (old %d), restarts %d", se.PID, pids["a"], se.Restarts)
	}
	awaitDead(t, pids["a"])
	if !processAlive(pids["b"]) || !processAlive(pids["c"]) {
		t.Error("siblings must survive a restart")
	}
	if st[1].Restarts != 1 && st[1].Restarts != 0 { // b was started manually once
		t.Errorf("b restarts = %d", st[1].Restarts)
	}

	// Start on an already-running process is a no-op.
	s.Start("c")
	if evs := settle(s, 200*time.Millisecond); len(evs) > 0 {
		for _, ev := range evs {
			if _, ok := ev.(StartEvent); ok {
				t.Errorf("start of a running process relaunched it")
			}
		}
	}
}

func TestStopKillsProcessTree(t *testing.T) {
	s := newTest(t, defaultTiming, helperProc(t, "tree", "tree", nil))
	st := s.States()[0]
	self := waitPID(t, s, st, "self=")
	child := waitPID(t, s, st, "child=")
	if !processAlive(self) || !processAlive(child) {
		t.Fatal("tree should be running")
	}

	s.Stop("tree")
	waitExit(t, s, "tree")
	awaitDead(t, self)
	awaitDead(t, child)
}

func TestShutdownKillsProcessTree(t *testing.T) {
	s := newTest(t, defaultTiming, helperProc(t, "tree", "tree", nil))
	st := s.States()[0]
	self := waitPID(t, s, st, "self=")
	child := waitPID(t, s, st, "child=")

	s.Shutdown()
	s.Shutdown() // idempotent
	awaitDead(t, self)
	awaitDead(t, child)
}

func TestExitSweepsLeftoverChildren(t *testing.T) {
	s := newTest(t, defaultTiming, helperProc(t, "orphan", "orphan", nil))
	st := s.States()[0]
	child := waitPID(t, s, st, "child=")
	if ee := waitExit(t, s, "orphan"); ee.Code != 0 {
		t.Fatalf("exit: %+v", ee)
	}
	awaitDead(t, child)
}

func TestGracefulStopLetsProcessExitItself(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("CTRL_BREAK delivery needs a real console, which CI runners lack")
	}
	s := newTest(t, defaultTiming, helperProc(t, "g", "graceful", nil))
	st := s.States()[0]
	waitLine(t, s, st, "ready")

	begin := time.Now()
	s.Stop("g")
	ee := waitExit(t, s, "g")
	if !ee.Stopped {
		t.Errorf("want a requested exit, got %+v", ee)
	}
	if took := time.Since(begin); took > 2*time.Second {
		t.Errorf("graceful stop took %v; it should not wait out the 3s grace period", took)
	}
	if !hasLine(st, "graceful-exit") {
		t.Errorf("process never saw the termination signal: %q", st.Lines())
	}
}

func TestStopForceKillsAfterGrace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("CTRL_BREAK delivery needs a real console, which CI runners lack")
	}
	p := helperProc(t, "s", "stubborn", nil)
	p.GracePeriod = 500 * time.Millisecond
	s := newTest(t, defaultTiming, p)
	st := s.States()[0]
	pid := waitStart(t, s, "s").PID
	waitLine(t, s, st, "ready")

	begin := time.Now()
	s.Stop("s")
	ee := waitExit(t, s, "s")
	took := time.Since(begin)
	if !ee.Stopped {
		t.Errorf("exit: %+v", ee)
	}
	if took < 450*time.Millisecond {
		t.Errorf("stopped after %v: the force-kill came before the grace period ended", took)
	}
	if took > 3*time.Second {
		t.Errorf("stopped after %v: the force-kill did not happen at the grace period", took)
	}
	awaitDead(t, pid)
}

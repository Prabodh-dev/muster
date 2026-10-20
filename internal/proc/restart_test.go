package proc

import (
	"testing"
	"time"

	"github.com/Prabodh-dev/muster/internal/config"
)

func exitProc(t *testing.T, policy config.RestartPolicy, code string, maxRestarts int) config.Process {
	p := helperProc(t, "job", "exit", map[string]string{"MUSTER_TEST_EXIT": code})
	p.Restart = policy
	p.MaxRestarts = maxRestarts
	return p
}

// starts counts StartEvents seen for "job" over d.
func starts(s *Supervisor, d time.Duration) int {
	n := 0
	for _, ev := range settle(s, d) {
		if _, ok := ev.(StartEvent); ok {
			n++
		}
	}
	return n
}

func TestBackoffSchedule(t *testing.T) {
	tm := timing{backoffBase: time.Second, backoffMax: 10 * time.Second}
	want := []time.Duration{1, 2, 4, 8, 10, 10, 10}
	for i, w := range want {
		if got := tm.backoff(i + 1); got != w*time.Second {
			t.Errorf("backoff(%d) = %v, want %v", i+1, got, w*time.Second)
		}
	}
	if got := tm.backoff(500); got != 10*time.Second {
		t.Errorf("large attempt counts must stay capped, got %v", got)
	}
}

func TestPolicyNeverDoesNotRestart(t *testing.T) {
	s := newTest(t, fastTiming, exitProc(t, config.RestartNever, "1", 5))
	waitExit(t, s, "job")
	if n := starts(s, 500*time.Millisecond); n != 0 {
		t.Errorf("restarted %d times under policy never", n)
	}
	if st := s.States()[0]; st.Status != Crashed {
		t.Errorf("status = %v, want crashed", st.Status)
	}
}

func TestPolicyOnFailure(t *testing.T) {
	t.Run("restarts after a failure", func(t *testing.T) {
		s := newTest(t, fastTiming, exitProc(t, config.RestartOnFailure, "1", 5))
		waitStart(t, s, "job")
		ee := waitExit(t, s, "job")
		if ee.RetryIn <= 0 {
			t.Fatalf("expected a scheduled restart, got %+v", ee)
		}
		if st := s.States()[0]; st.Status != Restarting {
			t.Errorf("status = %v, want restarting", st.Status)
		}
		se := waitStart(t, s, "job")
		if se.Restarts != 1 {
			t.Errorf("restarts = %d, want 1", se.Restarts)
		}
	})

	t.Run("leaves a clean exit alone", func(t *testing.T) {
		s := newTest(t, fastTiming, exitProc(t, config.RestartOnFailure, "0", 5))
		waitExit(t, s, "job")
		if n := starts(s, 500*time.Millisecond); n != 0 {
			t.Errorf("restarted %d times after a clean exit", n)
		}
		if st := s.States()[0]; st.Status != Exited {
			t.Errorf("status = %v, want exited", st.Status)
		}
	})
}

func TestPolicyAlwaysRestartsCleanExits(t *testing.T) {
	s := newTest(t, fastTiming, exitProc(t, config.RestartAlways, "0", 5))
	waitStart(t, s, "job")
	waitExit(t, s, "job")
	waitStart(t, s, "job")
	waitExit(t, s, "job")
	if n := s.States()[0].Restarts; n != 1 {
		t.Errorf("restarts = %d, want 1", n)
	}
}

func TestRestartCapGivesUp(t *testing.T) {
	s := newTest(t, fastTiming, exitProc(t, config.RestartOnFailure, "1", 2))
	nStarts := 0
	var last ExitEvent
	run(t, s, func(ev Event) bool {
		switch e := ev.(type) {
		case StartEvent:
			nStarts++
		case ExitEvent:
			last = e
			return e.GaveUp
		}
		return false
	})
	if nStarts != 3 { // the first run plus two restarts
		t.Errorf("started %d times, want 3", nStarts)
	}
	if !last.GaveUp || last.Max != 2 {
		t.Errorf("final exit = %+v", last)
	}
	if st := s.States()[0]; st.Status != Crashed {
		t.Errorf("status = %v, want crashed", st.Status)
	}
	if n := starts(s, 600*time.Millisecond); n != 0 {
		t.Errorf("kept restarting after giving up (%d more starts)", n)
	}

	// The user can still bring it back by hand, which resets the budget.
	s.Start("job")
	waitStart(t, s, "job")
}

func TestBackoffActuallyBacksOff(t *testing.T) {
	s := newTest(t, fastTiming, exitProc(t, config.RestartOnFailure, "1", 4))
	var exits, startsAt []time.Time
	run(t, s, func(ev Event) bool {
		switch e := ev.(type) {
		case StartEvent:
			startsAt = append(startsAt, time.Now())
		case ExitEvent:
			exits = append(exits, time.Now())
			return e.GaveUp
		}
		return false
	})
	if len(startsAt) != 5 {
		t.Fatalf("got %d starts, want 5", len(startsAt))
	}
	// Gap between an exit and the next start is the backoff: 40, 80, 160, 320ms.
	// Timers never fire early, so these are hard lower bounds.
	for i, min := range []time.Duration{40, 80, 160, 320} {
		gap := startsAt[i+1].Sub(exits[i])
		if gap < min*time.Millisecond*9/10 {
			t.Errorf("restart %d came after %v, want at least %v", i+1, gap, min*time.Millisecond)
		}
	}
}

func TestStableRunResetsFailureCount(t *testing.T) {
	// With a tiny stableAfter every run counts as stable, so the failure
	// counter keeps resetting and a cap of 1 is never exceeded.
	tm := fastTiming
	tm.stableAfter = time.Nanosecond
	s := newTest(t, tm, exitProc(t, config.RestartOnFailure, "1", 1))
	for i := 0; i < 4; i++ {
		waitStart(t, s, "job")
		if ee := waitExit(t, s, "job"); ee.GaveUp {
			t.Fatalf("gave up on run %d despite stable runs", i+1)
		}
	}
}

func TestStopCancelsPendingRestart(t *testing.T) {
	tm := fastTiming
	tm.backoffBase, tm.backoffMax = 600*time.Millisecond, 600*time.Millisecond
	s := newTest(t, tm, exitProc(t, config.RestartAlways, "1", 5))
	waitExit(t, s, "job") // now waiting out the backoff
	s.Stop("job")
	if ee := waitExit(t, s, "job"); !ee.Stopped {
		t.Fatalf("expected the pending restart to be cancelled, got %+v", ee)
	}
	if n := starts(s, time.Second); n != 0 {
		t.Errorf("process restarted %d times after being stopped", n)
	}
	if st := s.States()[0]; st.Status != Stopped {
		t.Errorf("status = %v, want stopped", st.Status)
	}
}

func TestStartFailureFollowsPolicy(t *testing.T) {
	p := exitProc(t, config.RestartOnFailure, "0", 1)
	p.Cwd = "/definitely/not/a/dir"
	s := newTest(t, fastTiming, p)
	var final ExitEvent
	run(t, s, func(ev Event) bool {
		e, ok := ev.(ExitEvent)
		if ok && e.GaveUp {
			final = e
		}
		return ok && e.GaveUp
	})
	if final.Err == nil {
		t.Errorf("expected the start error to be reported: %+v", final)
	}
}

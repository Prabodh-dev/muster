package proc

import (
	"testing"
	"time"

	"github.com/Prabodh-dev/muster/internal/config"
)

func newStatsTest(t *testing.T, interval time.Duration, procs ...config.Process) *Supervisor {
	t.Helper()
	s := newSupervisor(&config.Config{BufferLines: 200, StatsInterval: interval, Processes: procs}, defaultTiming)
	t.Cleanup(s.Shutdown)
	return s
}

func TestStatsCoverWholeTree(t *testing.T) {
	s := newStatsTest(t, 100*time.Millisecond, helperProc(t, "tree", "tree", nil))
	st := s.States()[0]
	var got StatsEvent
	run(t, s, func(ev Event) bool {
		se, ok := ev.(StatsEvent)
		// shell (or the helper itself), the helper, and its sleeping child
		if ok && se.Procs >= 2 {
			got = se
		}
		return ok && se.Procs >= 2
	})
	if got.RSS == 0 {
		t.Errorf("tree reports no memory: %+v", got)
	}
	if !st.HasStats || st.Procs < 2 || st.RSS == 0 {
		t.Errorf("state not updated: %+v", st)
	}
}

func TestStatsReportCPU(t *testing.T) {
	s := newStatsTest(t, 200*time.Millisecond, helperProc(t, "spin", "spin", nil))
	var best float64
	run(t, s, func(ev Event) bool {
		if se, ok := ev.(StatsEvent); ok && se.CPU > best {
			best = se.CPU
		}
		return best > 30 // a spinning process should read well above idle
	})
}

func TestStatsStopWithProcess(t *testing.T) {
	s := newStatsTest(t, 100*time.Millisecond, helperProc(t, "sleeper", "sleep", nil))
	st := s.States()[0]
	run(t, s, func(ev Event) bool { _, ok := ev.(StatsEvent); return ok })
	if !st.HasStats {
		t.Fatal("expected stats while running")
	}
	s.Stop("sleeper")
	waitExit(t, s, "sleeper")
	settle(s, 400*time.Millisecond) // any late sample must be ignored
	if st.HasStats {
		t.Error("a stopped process must not keep showing stats")
	}
}

func TestStatsCanBeDisabled(t *testing.T) {
	s := newStatsTest(t, 0, helperProc(t, "sleeper", "sleep", nil))
	for _, ev := range settle(s, 700*time.Millisecond) {
		if _, ok := ev.(StatsEvent); ok {
			t.Fatal("sampling is off but a StatsEvent arrived")
		}
	}
}

func TestUptimeResetsOnRestart(t *testing.T) {
	s := newStatsTest(t, 0, helperProc(t, "sleeper", "sleep", nil))
	st := s.States()[0]
	waitStart(t, s, "sleeper")
	first := st.StartedAt
	if first.IsZero() {
		t.Fatal("StartedAt not set")
	}
	time.Sleep(600 * time.Millisecond)
	before := st.Uptime(time.Now())
	if before < 500*time.Millisecond {
		t.Fatalf("uptime %v should have grown while running", before)
	}

	s.Restart("sleeper")
	waitStart(t, s, "sleeper")
	if !st.StartedAt.After(first) {
		t.Errorf("StartedAt did not move forward on restart: %v -> %v", first, st.StartedAt)
	}
	if after := st.Uptime(time.Now()); after >= before {
		t.Errorf("uptime %v after restart should be below the old %v", after, before)
	}

	s.Stop("sleeper")
	waitExit(t, s, "sleeper")
	if up := st.Uptime(time.Now()); up != 0 {
		t.Errorf("a stopped process has no uptime, got %v", up)
	}
}

func TestUptimeResetsOnAutomaticRestart(t *testing.T) {
	p := exitProc(t, config.RestartAlways, "0", 5)
	s := newTest(t, fastTiming, p)
	st := s.States()[0]
	waitStart(t, s, "job")
	first := st.StartedAt
	waitExit(t, s, "job")
	waitStart(t, s, "job")
	if !st.StartedAt.After(first) {
		t.Errorf("StartedAt did not move forward on automatic restart")
	}
}

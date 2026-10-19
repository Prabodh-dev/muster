package proc

import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Prabodh-dev/muster/internal/config"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh-style commands")
	}
}

// TestFanInConcurrent drives several fake processes (in-memory pipes) at once
// into one channel and checks that every line arrives, per-process order is
// preserved, and a single consumer sees them all.
func TestFanInConcurrent(t *testing.T) {
	const procs, lines = 8, 2000
	out := make(chan Event, 16)
	quit := make(chan struct{})

	var producers sync.WaitGroup
	for i := 0; i < procs; i++ {
		r, w := io.Pipe()
		name := fmt.Sprintf("p%d", i)
		producers.Add(2)
		go func() {
			defer producers.Done()
			for n := 0; n < lines; n++ {
				fmt.Fprintf(w, "%d\n", n)
			}
			w.Close()
		}()
		go func() {
			defer producers.Done()
			pump(r, name, out, quit)
		}()
	}
	go func() { producers.Wait(); close(out) }()

	next := map[string]int{}
	for ev := range out {
		le := ev.(LineEvent)
		if want := fmt.Sprint(next[le.Proc]); le.Text != want {
			t.Fatalf("%s: got line %q, want %q", le.Proc, le.Text, want)
		}
		next[le.Proc]++
	}
	for i := 0; i < procs; i++ {
		if n := next[fmt.Sprintf("p%d", i)]; n != lines {
			t.Errorf("p%d: got %d lines, want %d", i, n, lines)
		}
	}
}

// TestPumpAbandonsOnQuit: a reader stuck on a full channel must be released
// when the supervisor shuts down.
func TestPumpAbandonsOnQuit(t *testing.T) {
	out := make(chan Event) // never read
	quit := make(chan struct{})
	r, w := io.Pipe()
	done := make(chan struct{})
	go func() { pump(r, "x", out, quit); close(done) }()
	go func() { fmt.Fprintln(w, "a"); fmt.Fprintln(w, "b"); w.Close() }()
	time.Sleep(20 * time.Millisecond)
	close(quit)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pump did not return after quit")
	}
}

func TestPumpSplitsLongLinesAndTrimsCR(t *testing.T) {
	out := make(chan Event, 16)
	quit := make(chan struct{})
	long := strings.Repeat("x", maxLine+10)
	go func() {
		pump(strings.NewReader("hi\r\n"+long+"\ntail"), "x", out, quit)
		close(out)
	}()
	var got []string
	for ev := range out {
		got = append(got, ev.(LineEvent).Text)
	}
	if len(got) != 4 || got[0] != "hi" || got[3] != "tail" {
		t.Fatalf("unexpected lines: %d", len(got))
	}
	if len(got[1])+len(got[2]) != len(long) {
		t.Errorf("long line lost bytes")
	}
}

func collect(t *testing.T, s *Supervisor, until func() bool) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for !until() {
		select {
		case ev := <-s.Events():
			s.Apply(ev)
		case <-timeout:
			t.Fatal("timed out waiting for events")
		}
	}
}

func TestSupervisorCapturesOutputAndExitStatus(t *testing.T) {
	skipOnWindows(t)
	cfg := &config.Config{
		BufferLines: 5,
		Processes: []config.Process{
			{Name: "ok", Command: "echo one; echo two >&2; echo three; echo four; echo five", Cwd: t.TempDir()},
			{Name: "bad", Command: "echo boom; exit 7", Cwd: t.TempDir()},
			{Name: "env", Command: "echo $GREETING", Cwd: t.TempDir(), Env: map[string]string{"GREETING": "hello"}},
			{Name: "missing", Command: "echo x", Cwd: "/definitely/not/a/dir"},
		},
	}
	s := New(cfg)
	defer s.Shutdown()
	st := s.States()
	collect(t, s, func() bool {
		for _, x := range st {
			if x.Status == Running {
				return false
			}
		}
		return true
	})

	if st[0].Status != Exited || st[1].Status != Crashed || st[1].Code != 7 {
		t.Errorf("statuses: %v %v code=%d", st[0].Status, st[1].Status, st[1].Code)
	}
	if st[3].Status != Crashed {
		t.Errorf("start failure should be crashed, got %v", st[3].Status)
	}
	// Ring buffer of 5 keeps only the tail: the start note and "one"
	// have been evicted by the time the exit note arrives.
	if got := st[0].Lines(); len(got) != 5 || got[0] != "two" || got[3] != "five" {
		t.Errorf("ok lines = %q", got)
	}
	if st[0].Dropped() == 0 {
		t.Error("expected dropped lines")
	}
	if !hasLine(st[2], "hello") {
		t.Errorf("env lines = %q", st[2].Lines())
	}
}

func TestStopTerminatesProcessTree(t *testing.T) {
	skipOnWindows(t)
	// The shell spawns a long-running child and a backgrounded grandchild;
	// both must die with the supervisor. Each prints its pid.
	cfg := &config.Config{
		BufferLines: 10,
		Processes: []config.Process{{
			Name:    "tree",
			Command: "sleep 300 & echo bg=$!; echo fg=$$; sleep 300",
			Cwd:     t.TempDir(),
		}},
	}
	s := New(cfg)
	st := s.States()[0]
	var pids []int
	collect(t, s, func() bool {
		pids = pids[:0]
		for _, l := range st.Lines() {
			var n int
			if _, err := fmt.Sscanf(l, "bg=%d", &n); err == nil {
				pids = append(pids, n)
			}
			if _, err := fmt.Sscanf(l, "fg=%d", &n); err == nil {
				pids = append(pids, n)
			}
		}
		return len(pids) == 2
	})

	s.Shutdown()
	s.Shutdown() // idempotent

	deadline := time.Now().Add(5 * time.Second)
	for _, pid := range pids {
		for processAlive(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("pid %d still alive after Stop", pid)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func hasLine(st *State, want string) bool {
	for _, l := range st.Lines() {
		if l == want {
			return true
		}
	}
	return false
}

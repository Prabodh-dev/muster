package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Prabodh-dev/muster/internal/config"
)

func newLogTest(t *testing.T, dir string, bufferLines int, procs ...config.Process) *Supervisor {
	t.Helper()
	cfg := &config.Config{
		BufferLines: bufferLines,
		Logs:        config.LogConfig{Enabled: true, Dir: dir},
		Processes:   procs,
	}
	s := newSupervisor(cfg, fastTiming)
	t.Cleanup(s.Shutdown)
	return s
}

func readLog(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, logFileName(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLogFileCapturesOutputAndLifecycle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "logs") // created on demand
	s := newLogTest(t, dir, 100, exitProc(t, config.RestartNever, "3", 0))
	waitExit(t, s, "job")

	got := readLog(t, dir, "job")
	for _, want := range []string{"running\n", "] started (pid ", "] exited with code 3\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("log file missing %q:\n%s", want, got)
		}
	}
	// Muster's own entries are timestamped; process output is untouched.
	if !strings.Contains(got, "[muster 20") {
		t.Errorf("notes should carry a timestamp:\n%s", got)
	}
	if !strings.HasPrefix(got, "[muster ") || strings.Index(got, "running") < strings.Index(got, "started") {
		t.Errorf("start note should come before the process output:\n%s", got)
	}
	if !hasLinePrefix(s.States()[0], "[muster] logging to ") {
		t.Errorf("the pane should say where the log goes: %q", s.States()[0].Lines())
	}
}

func hasLinePrefix(st *State, prefix string) bool {
	for _, l := range st.Lines() {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

func TestLogFileKeepsEverythingPastTheBuffer(t *testing.T) {
	dir := t.TempDir()
	const n = 500
	p := helperProc(t, "chatty", "lines", map[string]string{"MUSTER_TEST_LINES": fmt.Sprint(n)})
	s := newLogTest(t, dir, 10, p)
	st := s.States()[0]
	waitLine(t, s, st, fmt.Sprintf("line %d", n))
	s.Shutdown()

	if st.Dropped() == 0 || len(st.Lines()) > 10 {
		t.Fatalf("the in-memory buffer should have overflowed (dropped=%d, kept=%d)", st.Dropped(), len(st.Lines()))
	}
	got := readLog(t, dir, "chatty")
	for i := 1; i <= n; i++ {
		if !strings.Contains(got, fmt.Sprintf("line %d\n", i)) {
			t.Fatalf("log file lost line %d", i)
		}
	}
}

func TestLogFileSurvivesRestartAndAppendsAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	s := newLogTest(t, dir, 100, helperProc(t, "svc", "graceful", nil))
	waitLine(t, s, s.States()[0], "ready")
	s.Restart("svc")
	waitExit(t, s, "svc")
	waitStart(t, s, "svc")
	s.Shutdown()

	first := readLog(t, dir, "svc")
	if got := strings.Count(first, "] started (pid "); got != 2 {
		t.Errorf("want 2 start notes for a restart, got %d:\n%s", got, first)
	}
	if !strings.Contains(first, "] stopped\n") {
		t.Errorf("missing stop note:\n%s", first)
	}

	// A later muster run appends to the same file rather than replacing it.
	s2 := newLogTest(t, dir, 100, helperProc(t, "svc", "exit", nil))
	waitExit(t, s2, "svc")
	second := readLog(t, dir, "svc")
	if !strings.HasPrefix(second, first) || len(second) <= len(first) {
		t.Errorf("second run should append to the first run's log")
	}
}

func TestLogFileWritesLongLinesVerbatim(t *testing.T) {
	dir := t.TempDir()
	fl, err := openFileLog(dir, "x")
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("y", 3*maxLine+17)
	out := make(chan Event, 16)
	pump(strings.NewReader(long+"\nnext\n"), "x", out, make(chan struct{}), fl)
	fl.close()

	if got := readLog(t, dir, "x"); got != long+"\nnext\n" {
		t.Errorf("file content differs from the process output (len %d vs %d)", len(got), len(long)+6)
	}
}

func TestLogFileNames(t *testing.T) {
	want := map[string]string{
		"web":     "web.log",
		"api-v2":  "api-v2.log",
		"a/b":     "a%2Fb.log",
		`a\b`:     "a%5Cb.log",
		"a:b":     "a%3Ab.log",
		".hidden": "%2Ehidden.log",
		"é":       "%C3%A9.log",
	}
	seen := map[string]string{}
	for name, w := range want {
		got := logFileName(name)
		if got != w {
			t.Errorf("logFileName(%q) = %q, want %q", name, got, w)
		}
		if other, dup := seen[got]; dup {
			t.Errorf("%q and %q share a file", name, other)
		}
		seen[got] = name
	}
	if got := logFileName("a_b"); got == logFileName("a/b") {
		t.Error("distinct names must map to distinct files")
	}
}

func TestLogFilesOffByDefault(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	cfg := &config.Config{
		BufferLines: 10,
		Logs:        config.LogConfig{Enabled: false, Dir: dir},
		Processes:   []config.Process{exitProc(t, config.RestartNever, "0", 0)},
	}
	s := newSupervisor(cfg, fastTiming)
	defer s.Shutdown()
	waitExit(t, s, "job")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("log directory must not be created when logging is off (stat err: %v)", err)
	}
}

func TestUnwritableLogDirDoesNotStopTheProcess(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A path below a regular file can never be created.
	s := newLogTest(t, filepath.Join(blocker, "logs"), 10, exitProc(t, config.RestartNever, "0", 0))
	ee := waitExit(t, s, "job")
	if ee.Crashed() {
		t.Errorf("the process itself must still run: %+v", ee)
	}
	st := s.States()[0]
	if !hasLinePrefix(st, "[muster] cannot write log file") || !hasLine(st, "running") {
		t.Errorf("expected a warning plus normal output: %q", st.Lines())
	}
}

func TestLogFileFlushedWhileProcessKeepsRunning(t *testing.T) {
	dir := t.TempDir()
	s := newLogTest(t, dir, 100, helperProc(t, "live", "graceful", nil))
	waitLine(t, s, s.States()[0], "ready")
	// The process is still alive and has printed one line; a crash right now
	// must not lose it, so it has to be on disk already.
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(readLog(t, dir, "live"), "ready\n") {
		if time.Now().After(deadline) {
			t.Fatal("output not flushed to disk while the process is running")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestLogFileNotesShutdown(t *testing.T) {
	dir := t.TempDir()
	s := newLogTest(t, dir, 100, helperProc(t, "svc", "sleep", nil))
	waitStart(t, s, "svc")
	s.Shutdown()
	if got := readLog(t, dir, "svc"); !strings.Contains(got, "] stopped (muster exiting)\n") {
		t.Errorf("shutdown should be recorded:\n%s", got)
	}
}

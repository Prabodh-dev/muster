package stats

import (
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestDescendants(t *testing.T) {
	children := map[int32][]int32{
		1:  {10, 11},
		10: {100, 101},
		11: {110},
		50: {51}, // unrelated tree
		// a recycled pid can make the table cyclic
		110: {11},
	}
	got := descendants(children, 1)
	want := []int32{1, 10, 11, 100, 101, 110}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := descendants(children, 999); !reflect.DeepEqual(got, []int32{999}) {
		t.Errorf("unknown root should be a tree of one, got %v", got)
	}
}

func TestSampleSelf(t *testing.T) {
	c := NewCollector()
	pid := os.Getpid()
	s, ok := c.Sample([]int{pid})[pid]
	if !ok {
		t.Fatal("no sample for the current process")
	}
	if s.RSS == 0 || s.Procs < 1 {
		t.Errorf("implausible sample: %+v", s)
	}
}

func TestSampleSkipsMissingRoots(t *testing.T) {
	c := NewCollector()
	// Start and reap a process so its pid is certainly free.
	cmd := exec.Command(os.Args[0], "-test.run=NONE")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	got := c.Sample([]int{cmd.Process.Pid})
	if len(got) != 0 {
		t.Errorf("a dead process should produce no sample, got %v", got)
	}
	if got := c.Sample(nil); len(got) != 0 {
		t.Errorf("no roots, got %v", got)
	}
}

func TestSampleCountsChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses the test binary's sleep mode via the shell")
	}
	cmd := exec.Command("/bin/sh", "-c", "sleep 30 & sleep 30; wait")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	c := NewCollector()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s := c.Sample([]int{cmd.Process.Pid})[cmd.Process.Pid]
		if s.Procs >= 3 { // sh and two sleeps
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("tree never reached 3 processes: %+v", s)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

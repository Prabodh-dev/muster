package main

import (
	"bytes"
	"net"
	"strings"
	"testing"

	"github.com/Prabodh-dev/muster/internal/config"
)

func busyProcs(t *testing.T) []config.Process {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	port := l.Addr().(*net.TCPAddr).Port
	return []config.Process{
		{Name: "frontend", Ports: []int{port}},
		{Name: "worker"},
	}
}

func TestCheckPortsWarnsBeforeStarting(t *testing.T) {
	procs := busyProcs(t)
	var out bytes.Buffer
	err := checkPorts(procs, false, false, strings.NewReader(""), &out)
	if err != errPortConflicts {
		t.Fatalf("err = %v, want errPortConflicts", err)
	}
	for _, want := range []string{"already in use", "Nothing has been started yet", "frontend: port", "-ignore-port-conflicts"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "worker") {
		t.Errorf("undeclared process should not be mentioned:\n%s", out.String())
	}
}

func TestCheckPortsInteractive(t *testing.T) {
	procs := busyProcs(t)
	for answer, wantErr := range map[string]bool{"y\n": false, "yes\n": false, "n\n": true, "\n": true, "": true} {
		var out bytes.Buffer
		err := checkPorts(procs, false, true, strings.NewReader(answer), &out)
		if (err != nil) != wantErr {
			t.Errorf("answer %q: err = %v, want error = %v", answer, err, wantErr)
		}
		if !strings.Contains(out.String(), "Start anyway? [y/N]") {
			t.Errorf("answer %q: no prompt shown:\n%s", answer, out.String())
		}
	}
}

func TestCheckPortsForce(t *testing.T) {
	var out bytes.Buffer
	if err := checkPorts(busyProcs(t), true, false, strings.NewReader(""), &out); err != nil {
		t.Fatalf("force should continue, got %v", err)
	}
	if !strings.Contains(out.String(), "already in use") {
		t.Errorf("the warning must still be printed:\n%s", out.String())
	}
}

func TestCheckPortsQuietWhenFree(t *testing.T) {
	var out bytes.Buffer
	if err := checkPorts([]config.Process{{Name: "x"}}, false, false, strings.NewReader(""), &out); err != nil || out.Len() != 0 {
		t.Errorf("err=%v output=%q", err, out.String())
	}
}

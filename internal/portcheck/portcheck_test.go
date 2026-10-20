package portcheck

import (
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Prabodh-dev/muster/internal/config"
)

func listen(t *testing.T, addr string) int {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("cannot listen on %s: %v", addr, err)
	}
	t.Cleanup(func() { l.Close() })
	return l.Addr().(*net.TCPAddr).Port
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func procs(ports map[string][]int, order ...string) []config.Process {
	var out []config.Process
	for _, name := range order {
		out = append(out, config.Process{Name: name, Ports: ports[name]})
	}
	return out
}

func TestDetectsRealConflicts(t *testing.T) {
	for _, addr := range []string{":0", "127.0.0.1:0", "[::1]:0"} {
		t.Run(addr, func(t *testing.T) {
			port := listen(t, addr)
			got := Check(procs(map[string][]int{"web": {port}}, "web"))
			if len(got) != 1 || got[0].Process != "web" || got[0].Port != port {
				t.Fatalf("conflict on %s not detected: %v", addr, got)
			}
		})
	}
}

func TestReportsOnlyTheBusyPorts(t *testing.T) {
	busy := listen(t, "127.0.0.1:0")
	free := freePort(t)
	got := Check(procs(map[string][]int{
		"web":    {free},
		"api":    {free + 0, busy}, // the free one is skipped, the busy one reported
		"worker": nil,
	}, "web", "api", "worker"))
	if len(got) != 1 || got[0].Process != "api" || got[0].Port != busy {
		t.Fatalf("got %v, want only api:%d", got, busy)
	}
}

func TestNoConflictForFreeOrUndeclaredPorts(t *testing.T) {
	if got := Check(procs(map[string][]int{"web": {freePort(t)}}, "web", "worker")); len(got) != 0 {
		t.Errorf("unexpected conflicts: %v", got)
	}
}

func TestHolderIsIdentified(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("owner lookup is only asserted on Linux; other systems may need elevated rights")
	}
	port := listen(t, "127.0.0.1:0")
	got := Check(procs(map[string][]int{"web": {port}}, "web"))
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if want := "pid " + strconv.Itoa(os.Getpid()); !strings.Contains(got[0].Holder, want) {
		t.Errorf("holder %q should name %q", got[0].Holder, want)
	}
	if s := got[0].String(); !strings.Contains(s, "web: port "+strconv.Itoa(port)+" is already in use by ") {
		t.Errorf("String() = %q", s)
	}
}

func TestConflictStringWithoutHolder(t *testing.T) {
	c := Conflict{Process: "web", Port: 3000}
	if got := c.String(); got != "web: port 3000 is already in use" {
		t.Errorf("got %q", got)
	}
}

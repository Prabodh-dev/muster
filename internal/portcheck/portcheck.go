// Package portcheck finds TCP ports that are already taken before muster
// starts processes that expect to listen on them.
package portcheck

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Prabodh-dev/muster/internal/config"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// Conflict is a declared port that something else is already listening on.
type Conflict struct {
	Process string
	Port    int
	// Holder describes what owns the port, e.g. "node (pid 4242)". It is
	// empty when the owner could not be determined (commonly because it
	// belongs to another user).
	Holder string
}

func (c Conflict) String() string {
	s := fmt.Sprintf("%s: port %d is already in use", c.Process, c.Port)
	if c.Holder != "" {
		s += " by " + c.Holder
	}
	return s
}

// Check tests every port declared by procs and returns the ones in use, in
// config order. A port that cannot be tested (for instance a privileged port
// when not running as root) is not reported as a conflict.
func Check(procs []config.Process) []Conflict {
	var out []Conflict
	for _, p := range procs {
		for _, port := range p.Ports {
			if inUse(port) {
				out = append(out, Conflict{Process: p.Name, Port: port, Holder: holder(port)})
			}
		}
	}
	return out
}

// inUse reports whether a TCP listener already holds port. It tries to bind
// the port on all interfaces, which fails if any address already has a
// listener on it, then falls back to connecting for platforms whose bind
// errors are less specific.
func inUse(port int) bool {
	l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err == nil {
		l.Close()
		return false
	}
	if errors.Is(err, syscall.EADDRINUSE) || looksInUse(err) {
		return true
	}
	if errors.Is(err, syscall.EACCES) {
		return false // cannot bind here; nothing says it is taken
	}
	c, derr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
	if derr == nil {
		c.Close()
		return true
	}
	return false
}

func looksInUse(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "address already in use") || // unix
		strings.Contains(msg, "only one usage of each socket address") // windows
}

// holder names the process listening on port, best effort.
func holder(port int) string {
	conns, err := gnet.Connections("tcp")
	if err != nil {
		return ""
	}
	for _, c := range conns {
		if c.Status != "LISTEN" || int(c.Laddr.Port) != port || c.Pid == 0 {
			continue
		}
		name := ""
		if p, err := process.NewProcess(c.Pid); err == nil {
			name, _ = p.Name()
		}
		if name == "" {
			return fmt.Sprintf("pid %d", c.Pid)
		}
		return fmt.Sprintf("%s (pid %d)", name, c.Pid)
	}
	return ""
}

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

// inUse reports whether a TCP listener already holds port.
//
// Binding the port on all interfaces is the first test, and is conclusive
// when it fails with "address in use". It is not conclusive when it succeeds:
// Linux refuses to bind the wildcard address while any specific address
// listens on the port, but macOS and Windows allow it, so a server bound only
// to 127.0.0.1 or [::1] would go unnoticed. The second test therefore tries
// to connect to the port on loopback and on this machine's own addresses,
// which succeeds whenever something is listening there.
func inUse(port int) bool {
	l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err == nil {
		l.Close()
	} else if errors.Is(err, syscall.EADDRINUSE) || looksInUse(err) {
		return true
	}
	return answers(port)
}

// answers reports whether something accepts connections on port at any of
// this machine's addresses.
func answers(port int) bool {
	for _, host := range localHosts() {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 250*time.Millisecond)
		if err == nil {
			c.Close()
			return true
		}
	}
	return false
}

// localHosts lists the loopback addresses plus every global unicast address
// of this machine's interfaces. Link-local IPv6 addresses are skipped because
// they cannot be dialled without a zone.
func localHosts() []string {
	hosts := []string{"127.0.0.1", "::1"}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return hosts
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || !ipnet.IP.IsGlobalUnicast() {
			continue
		}
		hosts = append(hosts, ipnet.IP.String())
	}
	return hosts
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

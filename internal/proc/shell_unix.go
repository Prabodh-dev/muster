//go:build !windows

package proc

import (
	"errors"
	"os/exec"
	"syscall"
)

func shellCommand(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", command)
}

// configureProcAttr puts the child in its own process group so the whole
// tree (the shell and everything it spawned) can be signalled together.
func configureProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// group is the set of processes started by one command: the shell leader and
// every descendant that stayed in its process group.
type group struct{ pgid int }

func newGroup(cmd *exec.Cmd) (*group, error) {
	return &group{pgid: cmd.Process.Pid}, nil
}

// terminate sends SIGTERM to every process in the group.
func (g *group) terminate() error {
	return ignoreGone(syscall.Kill(-g.pgid, syscall.SIGTERM))
}

// kill sends SIGKILL to every process in the group.
func (g *group) kill() { _ = syscall.Kill(-g.pgid, syscall.SIGKILL) }

// empty reports whether no process is left in the group.
func (g *group) empty() bool {
	return errors.Is(syscall.Kill(-g.pgid, 0), syscall.ESRCH)
}

func (g *group) close() {}

func ignoreGone(err error) error {
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

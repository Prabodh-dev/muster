//go:build !windows

package proc

import (
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

func terminate(cmd *exec.Cmd) { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
func kill(cmd *exec.Cmd)      { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }

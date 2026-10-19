//go:build windows

package proc

import (
	"os/exec"
	"strconv"
	"syscall"
)

func shellCommand(command string) *exec.Cmd {
	cmd := exec.Command("cmd")
	// cmd.exe does its own argument parsing, so pass the line through verbatim.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd /C " + command}
	return cmd
}

func configureProcAttr(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
}

// Windows has no signal that politely stops an arbitrary console process, so
// both steps walk the tree with taskkill; the first is non-forced.
func terminate(cmd *exec.Cmd) {
	_ = exec.Command("taskkill", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}

func kill(cmd *exec.Cmd) {
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}

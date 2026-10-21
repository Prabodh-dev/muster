//go:build windows

package proc

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func shellCommand(command string) *exec.Cmd {
	cmd := exec.Command("cmd")
	// cmd.exe does its own argument parsing, so pass the line through
	// verbatim; /S makes it strip exactly the outer pair of quotes.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /S /C "` + command + `"`}
	return cmd
}

// configureProcAttr gives the child its own console process group, which is
// what lets us deliver CTRL_BREAK to it (and its console children) later.
func configureProcAttr(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
}

// group is a job object holding the child and everything it spawns. Unlike
// walking the process tree from the parent pid, a job still reaches
// grandchildren after their parent has exited.
type group struct {
	pid uint32

	mu  sync.Mutex
	job windows.Handle // zero once closed
}

func newGroup(cmd *exec.Cmd) (*group, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("creating job object: %w", err)
	}
	// Closing the last handle to the job (including if muster itself dies)
	// kills everything in it.
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configuring job object: %w", err)
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("opening process: %w", err)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("assigning process to job: %w", err)
	}
	return &group{pid: uint32(cmd.Process.Pid), job: job}, nil
}

// terminate delivers CTRL_BREAK, the closest thing Windows has to SIGTERM for
// console programs, to the child's console process group.
func (g *group) terminate() error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, g.pid)
}

// kill terminates every process in the job.
func (g *group) kill() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job != 0 {
		_ = windows.TerminateJobObject(g.job, 1)
	}
}

// jobAccounting mirrors the Win32 JOBOBJECT_BASIC_ACCOUNTING_INFORMATION
// structure, which golang.org/x/sys/windows does not define.
type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func (g *group) empty() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job == 0 {
		return true
	}
	var info jobAccounting
	err := windows.QueryInformationJobObject(g.job, windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
	return err == nil && info.ActiveProcesses == 0
}

func (g *group) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job != 0 {
		_ = windows.CloseHandle(g.job)
		g.job = 0
	}
}

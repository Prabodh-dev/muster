//go:build windows

package proc

func processAlive(pid int) bool { return false }

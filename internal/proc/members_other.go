//go:build !linux && !windows

package proc

func liveMembers(pgid int) (live, ok bool) { return false, false }

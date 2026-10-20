//go:build linux

package proc

import (
	"os"
	"strconv"
	"strings"
)

// liveMembers reports whether any non-zombie process is in process group
// pgid. A zombie still answers kill(-pgid, 0) until its parent (or init, for
// orphans) reaps it, which on init-less containers can take a while, so
// /proc is consulted instead. ok is false if /proc could not be read.
func liveMembers(pgid int) (live, ok bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, false
	}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue // exited while we were looking
		}
		// Format: pid (comm) state ppid pgrp ... ; comm may contain spaces
		// and parentheses, so split after the last ')'.
		s := string(data)
		rest := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
		if len(rest) < 3 {
			continue
		}
		if g, err := strconv.Atoi(rest[2]); err == nil && g == pgid && rest[0] != "Z" {
			return true, true
		}
	}
	return false, true
}

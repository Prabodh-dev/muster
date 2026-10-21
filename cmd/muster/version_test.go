package main

import (
	"strings"
	"testing"
)

func TestVersionStringUsesInjectedValues(t *testing.T) {
	oldV, oldC, oldD := version, commit, date
	defer func() { version, commit, date = oldV, oldC, oldD }()

	version, commit, date = "1.2.3", "0123456789abcdef", "2026-01-02T03:04:05Z"
	if got, want := versionString(), "muster 1.2.3 (0123456, 2026-01-02T03:04:05Z)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	version, commit, date = "1.2.3", "", ""
	if got := versionString(); !strings.HasPrefix(got, "muster 1.2.3") {
		t.Errorf("got %q", got)
	}
}

func TestVersionStringNeverEmpty(t *testing.T) {
	oldV := version
	defer func() { version = oldV }()
	version = ""
	if got := versionString(); !strings.HasPrefix(got, "muster ") || len(got) <= len("muster ") {
		t.Errorf("got %q", got)
	}
}

package main

import (
	"fmt"
	"runtime/debug"
)

// Set at release time by the linker (see .goreleaser.yaml).
var (
	version = ""
	commit  = ""
	date    = ""
)

// versionString describes this build. Release binaries carry the values
// injected by the linker; for a `go install` or a plain `go build` it falls
// back to the module version and VCS information Go embeds in the binary.
func versionString() string {
	v, c, d := version, commit, date
	if info, ok := debug.ReadBuildInfo(); ok {
		if v == "" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = info.Main.Version
		}
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if c == "" {
					c = s.Value
				}
			case "vcs.time":
				if d == "" {
					d = s.Value
				}
			}
		}
	}
	if v == "" {
		v = "dev"
	}
	out := "muster " + v
	if c != "" {
		if len(c) > 7 {
			c = c[:7]
		}
		out += fmt.Sprintf(" (%s", c)
		if d != "" {
			out += ", " + d
		}
		out += ")"
	}
	return out
}

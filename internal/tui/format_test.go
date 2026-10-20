package tui

import (
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	tests := map[uint64]string{
		0:                 "0B",
		1023:              "1023B",
		2048:              "2KB",
		145 * 1024 * 1024: "145MB",
		1<<30 + 1<<29:     "1.5GB",
	}
	for n, want := range tests {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFormatCPU(t *testing.T) {
	for in, want := range map[float64]string{0: "0.0%", 2.34: "2.3%", 99.95: "100.0%", 250: "250.0%", 1500: "1500%"} {
		if got := formatCPU(in); got != want {
			t.Errorf("formatCPU(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatUptime(t *testing.T) {
	tests := map[time.Duration]string{
		0:                                     "0s",
		7 * time.Second:                       "7s",
		59*time.Second + 600*time.Millisecond: "1m00s",
		3*time.Minute + 12*time.Second:        "3m12s",
		2*time.Hour + 5*time.Minute:           "2h05m",
		3*24*time.Hour + 4*time.Hour:          "3d4h",
	}
	for in, want := range tests {
		if got := formatUptime(in); got != want {
			t.Errorf("formatUptime(%v) = %q, want %q", in, got, want)
		}
	}
}

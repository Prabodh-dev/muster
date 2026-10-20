package tui

import (
	"fmt"
	"time"
)

// formatBytes renders a byte count compactly: 512B, 12KB, 145MB, 1.2GB.
func formatBytes(n uint64) string {
	const unit = 1024
	switch {
	case n < unit:
		return fmt.Sprintf("%dB", n)
	case n < unit*unit:
		return fmt.Sprintf("%dKB", n/unit)
	case n < unit*unit*unit:
		return fmt.Sprintf("%dMB", n/(unit*unit))
	default:
		return fmt.Sprintf("%.1fGB", float64(n)/(unit*unit*unit))
	}
}

// formatCPU renders a CPU percentage, one decimal below 1000%.
func formatCPU(p float64) string {
	if p >= 1000 {
		return fmt.Sprintf("%.0f%%", p)
	}
	return fmt.Sprintf("%.1f%%", p)
}

// formatUptime renders a duration in its two most significant units:
// 7s, 3m12s, 2h05m, 3d4h.
func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

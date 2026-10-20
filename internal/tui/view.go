package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Prabodh-dev/muster/internal/proc"
	"github.com/charmbracelet/lipgloss"
)

const (
	minSidebar = 28
	maxSidebar = 40
)

var (
	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(lipgloss.Color("240")).
			PaddingRight(1)
	selectedStyle = lipgloss.NewStyle().Bold(true).Reverse(true)
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	headerStyle   = lipgloss.NewStyle().Bold(true)

	runningDot = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("●")
	exitedDot  = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("○")
	crashedDot = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗")
	stoppedDot = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("■")
	retryDot   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("↻")
)

func indicator(s proc.Status) string {
	switch s {
	case proc.Running:
		return runningDot
	case proc.Exited:
		return exitedDot
	case proc.Stopped:
		return stoppedDot
	case proc.Restarting:
		return retryDot
	default:
		return crashedDot
	}
}

// sidebarWidth is the outer width of the sidebar including its border.
func (m Model) sidebarWidth() int {
	w := minSidebar
	for _, st := range m.states {
		w = max(w, lipgloss.Width(st.Name)+4)
	}
	return min(w, maxSidebar) + 2 // padding + border
}

// logSize is the size of the log viewport. One row goes to the help line
// and one to the log header.
func (m Model) logSize() (int, int) {
	return max(m.width-m.sidebarWidth()-1, 1), max(m.height-2, 1)
}

func renderView(m Model) string {
	inner := m.sidebarWidth() - 2
	rows := []string{titleStyle.Render("muster"), ""}
	now := time.Now()
	for i, st := range m.states {
		name := fmt.Sprintf("%s %s", indicator(st.Status), st.Name)
		detail := "  " + detailLine(st, now)
		name = lipgloss.NewStyle().MaxWidth(inner).Render(name)
		detail = lipgloss.NewStyle().MaxWidth(inner).Render(detail)
		if i == m.selected {
			name = selectedStyle.Width(inner).Render(name)
			detail = selectedStyle.Width(inner).Render(detail)
		} else {
			detail = helpStyle.Render(detail)
		}
		rows = append(rows, name, detail)
	}
	sidebar := sidebarStyle.
		Width(m.sidebarWidth() - 1).
		Height(m.height - 1).
		MaxHeight(m.height - 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, rows...))

	cur := m.states[m.selected]
	status := cur.Status.String()
	if cur.Status == proc.Crashed && cur.Code != 0 {
		status = fmt.Sprintf("crashed (exit %d)", cur.Code)
	}
	header := headerStyle.Render(cur.Name) + helpStyle.Render("  "+status)
	if len(m.ports[cur.Name]) > 0 {
		header += helpStyle.Render("  ports: " + joinInts(m.ports[cur.Name]))
	}
	if cur.Status == proc.Running && cur.HasStats && cur.Procs > 1 {
		header += helpStyle.Render(fmt.Sprintf("  (%d processes)", cur.Procs))
	}
	if cur.Restarts > 0 {
		header += helpStyle.Render(fmt.Sprintf("  restarts: %d", cur.Restarts))
	}
	if d := cur.Dropped(); d > 0 {
		header += helpStyle.Render(fmt.Sprintf("  (%d older lines dropped)", d))
	}
	main := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().MaxWidth(m.width-m.sidebarWidth()-1).Render(header),
		m.vp.View(),
	)
	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", main)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.footer())
}

// footer is the bottom line: the search prompt, the active search, or help.
func (m Model) footer() string {
	switch {
	case m.prompting:
		return m.input.View() + helpStyle.Render("  "+m.matchSummary()+"  enter keep · esc cancel")
	case m.query != "":
		return helpStyle.Render(fmt.Sprintf("/%s  %s  n/N next/prev · / edit · esc clear", m.query, m.matchSummary()))
	}
	return helpStyle.Render("j/k select · r restart · s stop · enter start · / search · pgup/pgdn scroll · q quit")
}

func (m Model) matchSummary() string {
	if m.query == "" {
		return ""
	}
	if len(m.log.matches) == 0 {
		return "no matches"
	}
	return fmt.Sprintf("%d/%d", m.cur+1, len(m.log.matches))
}

// detailLine is the second sidebar line for a process: resource use and
// uptime while it runs, otherwise what state it is in.
func detailLine(st *proc.State, now time.Time) string {
	switch st.Status {
	case proc.Running:
		up := formatUptime(st.Uptime(now))
		if !st.HasStats {
			return up
		}
		return fmt.Sprintf("%s · %s · %s", formatCPU(st.CPU), formatBytes(st.RSS), up)
	case proc.Exited:
		return "exited"
	case proc.Crashed:
		return fmt.Sprintf("crashed (exit %d)", st.Code)
	case proc.Restarting:
		return "restarting…"
	default:
		return st.Status.String()
	}
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

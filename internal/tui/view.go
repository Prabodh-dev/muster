package tui

import (
	"fmt"

	"github.com/Prabodh-dev/muster/internal/proc"
	"github.com/charmbracelet/lipgloss"
)

const (
	minSidebar = 16
	maxSidebar = 32
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
)

func indicator(s proc.Status) string {
	switch s {
	case proc.Running:
		return runningDot
	case proc.Exited:
		return exitedDot
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
	for i, st := range m.states {
		label := fmt.Sprintf("%s %s", indicator(st.Status), st.Name)
		label = lipgloss.NewStyle().MaxWidth(inner).Render(label)
		if i == m.selected {
			label = selectedStyle.Width(inner).Render(label)
		}
		rows = append(rows, label)
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
	if d := cur.Dropped(); d > 0 {
		header += helpStyle.Render(fmt.Sprintf("  (%d older lines dropped)", d))
	}
	main := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().MaxWidth(m.width-m.sidebarWidth()-1).Render(header),
		m.vp.View(),
	)
	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", main)
	help := helpStyle.Render("↑/↓ j/k select · pgup/pgdn scroll · g/G top/bottom · q quit")
	return lipgloss.JoinVertical(lipgloss.Left, body, help)
}

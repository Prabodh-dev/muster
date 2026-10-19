// Package tui is the Bubble Tea front end: a process sidebar and a log pane.
package tui

import (
	"strings"

	"github.com/Prabodh-dev/muster/internal/proc"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// maxBatch bounds how many queued events are folded into one Update, so a
// chatty process costs one re-render per batch instead of one per line.
const maxBatch = 512

// eventsMsg is a batch of process events pulled from the fan-in channel.
type eventsMsg []proc.Event

// Model is the root Bubble Tea model.
type Model struct {
	sup      *proc.Supervisor
	states   []*proc.State
	selected int

	vp            viewport.Model
	width, height int
	ready         bool
}

// New returns a model rendering the given supervisor.
func New(sup *proc.Supervisor) Model {
	return Model{sup: sup, states: sup.States()}
}

// Init starts consuming the fan-in channel.
func (m Model) Init() tea.Cmd { return waitForEvents(m.sup.Events()) }

// waitForEvents blocks until at least one event is available, then drains
// whatever else is already queued. It is the single consumer of the channel.
func waitForEvents(ch <-chan proc.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		batch := eventsMsg{ev}
		for len(batch) < maxBatch {
			select {
			case ev, ok := <-ch:
				if !ok {
					return batch
				}
				batch = append(batch, ev)
			default:
				return batch
			}
		}
		return batch
	}
}

// Update handles input, resizes and process events.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.refresh(true)
		return m, nil

	case eventsMsg:
		touched := false
		for _, ev := range msg {
			m.sup.Apply(ev)
			if ev.ProcName() == m.states[m.selected].Name {
				touched = true
			}
		}
		if touched {
			m.refresh(false)
		}
		return m, waitForEvents(m.sup.Events())

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.selectProc(m.selected - 1)
			return m, nil
		case "down", "j":
			m.selectProc(m.selected + 1)
			return m, nil
		case "g", "home":
			m.vp.GotoTop()
			return m, nil
		case "G", "end":
			m.vp.GotoBottom()
			return m, nil
		}
	}

	if m.ready {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) selectProc(i int) {
	if i < 0 || i >= len(m.states) || i == m.selected {
		return
	}
	m.selected = i
	m.refresh(true)
}

// layout sizes the viewport for the current terminal dimensions.
func (m *Model) layout() {
	w, h := m.logSize()
	if !m.ready {
		m.vp = viewport.New(w, h)
		m.vp.MouseWheelEnabled = true
		m.ready = true
		return
	}
	m.vp.Width, m.vp.Height = w, h
}

// refresh reloads the selected process's log into the viewport. Unless
// forceBottom is set, the view only follows the tail if it was already there.
func (m *Model) refresh(forceBottom bool) {
	if !m.ready {
		return
	}
	atBottom := m.vp.AtBottom()
	lines := m.states[m.selected].Lines()
	for i, l := range lines {
		l = strings.ReplaceAll(l, "\t", "    ")
		lines[i] = ansi.Hardwrap(l, max(m.vp.Width, 1), true)
	}
	m.vp.SetContent(strings.Join(lines, "\n"))
	if forceBottom || atBottom {
		m.vp.GotoBottom()
	}
}

// View renders the sidebar, log pane and help line.
func (m Model) View() string {
	if !m.ready {
		return "starting…"
	}
	return renderView(m)
}

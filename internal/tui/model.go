// Package tui is the Bubble Tea front end: a process sidebar and a log pane.
package tui

import (
	"time"

	"github.com/Prabodh-dev/muster/internal/proc"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// maxBatch bounds how many queued events are folded into one Update, so a
// chatty process costs one re-render per batch instead of one per line.
const maxBatch = 512

// tickMsg re-renders once a second so uptimes keep counting.
type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// eventsMsg is a batch of process events pulled from the fan-in channel.
type eventsMsg []proc.Event

// Model is the root Bubble Tea model.
type Model struct {
	sup      *proc.Supervisor
	states   []*proc.State
	selected int
	ports    map[string][]int // declared ports, shown in the header

	vp            viewport.Model
	width, height int
	ready         bool

	// log search
	prompting bool // the search prompt is open and has the keyboard
	input     textinput.Model
	query     string
	cur       int // index of the current match
	log       rendered
}

// New returns a model rendering the given supervisor. ports maps process
// names to the ports they declare, for display; it may be nil.
func New(sup *proc.Supervisor, ports map[string][]int) Model {
	in := textinput.New()
	in.Prompt = "/"
	in.CharLimit = 256
	return Model{sup: sup, states: sup.States(), ports: ports, input: in}
}

// Init starts consuming the fan-in channel.
func (m Model) Init() tea.Cmd { return tea.Batch(waitForEvents(m.sup.Events()), tick()) }

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

	case tickMsg:
		return m, tick()

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
		if m.prompting {
			return m.updatePrompt(msg)
		}
		switch msg.String() {
		case "/":
			m.prompting = true
			m.input.SetValue(m.query)
			m.input.CursorEnd()
			return m, m.input.Focus()
		case "esc":
			if m.query != "" {
				m.setQuery("")
			}
			return m, nil
		case "n":
			m.stepMatch(1)
			return m, nil
		case "N":
			m.stepMatch(-1)
			return m, nil
		case "q", "ctrl+c":
			// Children must not outlive muster: stop them before quitting.
			m.sup.Shutdown()
			return m, tea.Quit
		case "up", "k":
			m.selectProc(m.selected - 1)
			return m, nil
		case "down", "j":
			m.selectProc(m.selected + 1)
			return m, nil
		case "r":
			m.sup.Restart(m.states[m.selected].Name)
			return m, nil
		case "s":
			m.sup.Stop(m.states[m.selected].Name)
			return m, nil
		case "enter":
			m.sup.Start(m.states[m.selected].Name)
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
	m.log = renderLog(m.states[m.selected].Lines(), m.vp.Width, m.query, m.cur)
	if m.cur >= len(m.log.matches) {
		m.cur = max(len(m.log.matches)-1, 0)
		m.log = renderLog(m.states[m.selected].Lines(), m.vp.Width, m.query, m.cur)
	}
	m.vp.SetContent(m.log.content)
	if forceBottom || atBottom {
		m.vp.GotoBottom()
	}
}

// updatePrompt handles a key while the search prompt is open. The query is
// applied as it is typed.
func (m Model) updatePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.prompting = false
		m.input.Blur()
		return m, nil
	case tea.KeyEsc:
		m.prompting = false
		m.input.Blur()
		m.setQuery("")
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if v := m.input.Value(); v != m.query {
		m.setQuery(v)
	}
	return m, cmd
}

// setQuery changes the search text and jumps to the first match at or below
// the top of the view.
func (m *Model) setQuery(q string) {
	m.query = q
	m.cur = 0
	m.refresh(false)
	if len(m.log.matches) == 0 {
		return
	}
	top := m.vp.YOffset
	for i, mt := range m.log.matches {
		if m.log.rowStart[mt.line] >= top {
			m.cur = i
			break
		}
	}
	m.refresh(false)
	m.revealMatch()
}

// stepMatch moves to the next (+1) or previous (-1) match, wrapping around.
func (m *Model) stepMatch(delta int) {
	n := len(m.log.matches)
	if n == 0 {
		return
	}
	m.cur = ((m.cur+delta)%n + n) % n
	m.refresh(false)
	m.revealMatch()
}

// revealMatch scrolls the viewport so the current match is visible.
func (m *Model) revealMatch() {
	if m.cur >= len(m.log.matches) {
		return
	}
	row := m.log.rowStart[m.log.matches[m.cur].line]
	if row < m.vp.YOffset || row >= m.vp.YOffset+m.vp.Height {
		m.vp.SetYOffset(max(row-m.vp.Height/3, 0))
	}
}

// View renders the sidebar, log pane and help line.
func (m Model) View() string {
	if !m.ready {
		return "starting…"
	}
	return renderView(m)
}

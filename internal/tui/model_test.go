//go:build !windows

package tui

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Prabodh-dev/muster/internal/config"
	"github.com/Prabodh-dev/muster/internal/proc"
	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func newModel(t *testing.T, procs ...config.Process) (Model, *proc.Supervisor) {
	t.Helper()
	for i := range procs {
		procs[i].Cwd = t.TempDir()
	}
	sup := proc.New(&config.Config{BufferLines: 100, Processes: procs})
	t.Cleanup(sup.Shutdown)
	m := New(sup)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return next.(Model), sup
}

// pumpUntil feeds supervisor events through Update until done reports true.
func pumpUntil(t *testing.T, m Model, sup *proc.Supervisor, done func(Model) bool) Model {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for !done(m) {
		select {
		case ev := <-sup.Events():
			next, _ := m.Update(eventsMsg{ev})
			m = next.(Model)
		case <-timeout:
			t.Fatal("timed out waiting for process events")
		}
	}
	return m
}

func TestSelectionKeys(t *testing.T) {
	m, _ := newModel(t,
		config.Process{Name: "a", Command: "sleep 30"},
		config.Process{Name: "b", Command: "sleep 30"},
		config.Process{Name: "c", Command: "sleep 30"},
	)
	steps := []struct {
		key  string
		want int
	}{
		{"j", 1}, {"down", 2}, {"j", 2}, // clamps at the end
		{"k", 1}, {"up", 0}, {"k", 0}, // clamps at the start
	}
	for _, s := range steps {
		next, _ := m.Update(key(s.key))
		m = next.(Model)
		if m.selected != s.want {
			t.Fatalf("after %q selected = %d, want %d", s.key, m.selected, s.want)
		}
	}
}

func TestLogPaneFollowsSelection(t *testing.T) {
	m, sup := newModel(t,
		config.Process{Name: "a", Command: "echo from-a; sleep 30"},
		config.Process{Name: "b", Command: "echo from-b; sleep 30"},
	)
	m = pumpUntil(t, m, sup, func(m Model) bool {
		return hasLine(m.states[0], "from-a") && hasLine(m.states[1], "from-b")
	})
	next, _ := m.Update(key("j"))
	m = next.(Model)
	if v := m.View(); !contains(v, "from-b") || contains(v, "from-a") {
		t.Errorf("view should show only b's log:\n%s", v)
	}
	next, _ = m.Update(key("k"))
	m = next.(Model)
	if v := m.View(); !contains(v, "from-a") || contains(v, "from-b") {
		t.Errorf("view should show only a's log:\n%s", v)
	}
}

func TestStatusIndicators(t *testing.T) {
	m, sup := newModel(t,
		config.Process{Name: "ok", Command: "exit 0"},
		config.Process{Name: "bad", Command: "exit 2"},
		config.Process{Name: "live", Command: "sleep 30"},
	)
	m = pumpUntil(t, m, sup, func(m Model) bool {
		return m.states[0].Status != proc.Running && m.states[1].Status != proc.Running
	})
	if m.states[0].Status != proc.Exited || m.states[1].Status != proc.Crashed || m.states[2].Status != proc.Running {
		t.Errorf("statuses = %v %v %v", m.states[0].Status, m.states[1].Status, m.states[2].Status)
	}
}

func TestQuitTerminatesChildren(t *testing.T) {
	for _, quitKey := range []string{"q", "ctrl+c"} {
		t.Run(quitKey, func(t *testing.T) {
			m, sup := newModel(t, config.Process{
				Name:    "sleeper",
				Command: "echo pid=$$; exec sleep 300",
			})
			var pid int
			m = pumpUntil(t, m, sup, func(m Model) bool {
				for _, l := range m.states[0].Lines() {
					if _, err := fmt.Sscanf(l, "pid=%d", &pid); err == nil {
						return true
					}
				}
				return false
			})
			if !alive(pid) {
				t.Fatalf("child %d should be running before quit", pid)
			}

			_, cmd := m.Update(key(quitKey))
			if cmd == nil {
				t.Fatal("quit key returned no command")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("quit key did not produce tea.QuitMsg")
			}
			// Stop is synchronous: by the time Update returns the child is gone.
			deadline := time.Now().Add(3 * time.Second)
			for alive(pid) {
				if time.Now().After(deadline) {
					t.Fatalf("child %d still alive after quit", pid)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func hasLine(st *proc.State, want string) bool {
	for _, l := range st.Lines() {
		if l == want {
			return true
		}
	}
	return false
}

func TestSearchFlow(t *testing.T) {
	m, sup := newModel(t, config.Process{
		Name:    "log",
		Command: "echo alpha; echo needle one; echo beta; echo NEEDLE two; echo gamma; sleep 30",
	})
	m = pumpUntil(t, m, sup, func(m Model) bool { return hasLine(m.states[0], "gamma") })

	send := func(msgs ...tea.Msg) {
		for _, msg := range msgs {
			next, _ := m.Update(msg)
			m = next.(Model)
		}
	}
	typeText := func(s string) {
		for _, r := range s {
			send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}

	send(key("/"))
	if !m.prompting {
		t.Fatal("/ should open the search prompt")
	}
	typeText("needle") // 'n' and 'q'-like letters must go to the prompt, not act as commands
	if len(m.log.matches) != 2 {
		t.Fatalf("matches = %d, want 2 (case-insensitive)", len(m.log.matches))
	}
	if !strings.Contains(m.View(), "1/2") {
		t.Errorf("footer should show the match position:\n%s", m.View())
	}

	send(tea.KeyMsg{Type: tea.KeyEnter})
	if m.prompting || m.query != "needle" {
		t.Fatalf("enter should keep the query and close the prompt (prompting=%v query=%q)", m.prompting, m.query)
	}

	first := m.cur
	send(key("n"))
	if m.cur == first {
		t.Error("n should move to the next match")
	}
	send(key("n"))
	if m.cur != first {
		t.Error("n should wrap around")
	}
	send(key("N"))
	if m.cur == first {
		t.Error("N should move back (wrapping)")
	}

	send(tea.KeyMsg{Type: tea.KeyEsc})
	if m.query != "" || len(m.log.matches) != 0 {
		t.Errorf("esc should clear the search, query=%q", m.query)
	}

	// A query with no hits reports it.
	send(key("/"))
	typeText("absent")
	if !strings.Contains(m.View(), "no matches") {
		t.Errorf("expected a no-matches note:\n%s", m.View())
	}
	send(tea.KeyMsg{Type: tea.KeyEsc})
	if m.prompting {
		t.Error("esc should close the prompt")
	}
}

func TestSearchFollowsNewOutput(t *testing.T) {
	m, sup := newModel(t, config.Process{
		Name:    "log",
		Command: "echo hit 1; sleep 0.3; echo hit 2; sleep 30",
	})
	m = pumpUntil(t, m, sup, func(m Model) bool { return hasLine(m.states[0], "hit 1") })
	next, _ := m.Update(key("/"))
	m = next.(Model)
	for _, r := range "hit" {
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	if len(m.log.matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(m.log.matches))
	}
	m = pumpUntil(t, m, sup, func(m Model) bool { return hasLine(m.states[0], "hit 2") })
	if len(m.log.matches) != 2 {
		t.Errorf("new output should be searched too, matches = %d", len(m.log.matches))
	}
}

func TestControlKeysAffectOnlySelectedProcess(t *testing.T) {
	m, sup := newModel(t,
		config.Process{Name: "a", Command: "sleep 30", GracePeriod: 2 * time.Second},
		config.Process{Name: "b", Command: "sleep 30", GracePeriod: 2 * time.Second},
	)
	m = pumpUntil(t, m, sup, func(m Model) bool {
		return hasLinePrefix(m.states[0], "[muster] started") && hasLinePrefix(m.states[1], "[muster] started")
	})

	send := func(k string) {
		next, _ := m.Update(key(k))
		m = next.(Model)
	}
	enter := tea.KeyMsg{Type: tea.KeyEnter}

	send("j") // select b
	send("s")
	m = pumpUntil(t, m, sup, func(m Model) bool { return m.states[1].Status == proc.Stopped })
	if m.states[0].Status != proc.Running {
		t.Errorf("a = %v; stopping b must not touch it", m.states[0].Status)
	}

	next, _ := m.Update(enter)
	m = next.(Model)
	m = pumpUntil(t, m, sup, func(m Model) bool { return m.states[1].Status == proc.Running })

	send("r")
	m = pumpUntil(t, m, sup, func(m Model) bool { return m.states[1].Restarts == 1 && m.states[1].Status == proc.Running })
	if m.states[0].Restarts != 0 || m.states[0].Status != proc.Running {
		t.Errorf("a = %v restarts=%d; restarting b must not touch it", m.states[0].Status, m.states[0].Restarts)
	}
}

func hasLinePrefix(st *proc.State, prefix string) bool {
	for _, l := range st.Lines() {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

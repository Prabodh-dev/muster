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
	t.Cleanup(sup.Stop)
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
		return len(m.states[0].Lines()) > 0 && len(m.states[1].Lines()) > 0
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

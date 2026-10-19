package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestFindMatches(t *testing.T) {
	lines := []string{
		"GET /index 200",
		"nothing here",
		"get /a 404 get /b 200",
		"\x1b[31mred get\x1b[0m",
		"tab\tget",
	}
	got := findMatches(lines, "get")
	want := []match{
		{0, 0, 3},
		{2, 0, 3}, {2, 11, 14},
		{3, 4, 7}, // offsets are into the text without colour codes
		{4, 7, 10},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	if m := findMatches(lines, ""); m != nil {
		t.Errorf("empty query should match nothing, got %v", m)
	}
	if m := findMatches(lines, "zzz"); len(m) != 0 {
		t.Errorf("unexpected matches %v", m)
	}
}

func TestFindMatchesIsLiteral(t *testing.T) {
	lines := []string{"cost (a+b) [x]", "aab"}
	if got := findMatches(lines, "(a+b)"); len(got) != 1 || got[0].line != 0 {
		t.Errorf("regex metacharacters must be literal, got %v", got)
	}
}

func TestHighlight(t *testing.T) {
	// Force colour so the styles produce escape codes under `go test`.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	ms := findMatches([]string{"one two one"}, "one")
	out := highlight("one two one", ms, 1)
	if got := ansi.Strip(out); got != "one two one" {
		t.Fatalf("text changed: %q", got)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected styled output, got %q", out)
	}
	if cur := highlight("one two one", ms, 0); cur == out {
		t.Error("the current match should be styled differently from the others")
	}
}

func TestRenderLogMapsRowsAndWraps(t *testing.T) {
	lines := []string{"short", strings.Repeat("x", 25) + " needle", "needle again"}
	r := renderLog(lines, 10, "needle", 0)
	if want := []int{0, 1, 5}; !reflect.DeepEqual(r.rowStart, want) {
		t.Errorf("rowStart = %v, want %v", r.rowStart, want)
	}
	if len(r.matches) != 2 || r.matches[0].line != 1 || r.matches[1].line != 2 {
		t.Errorf("matches = %v", r.matches)
	}
	if got := ansi.Strip(strings.ReplaceAll(r.content, "\n", "")); got != strings.Join(lines, "") {
		t.Errorf("wrapping lost text: %q", got)
	}
}

func TestRenderLogWithoutQueryKeepsLines(t *testing.T) {
	r := renderLog([]string{"a\tb", "c"}, 20, "", -1)
	if r.content != "a    b\nc" || len(r.matches) != 0 {
		t.Errorf("content = %q", r.content)
	}
}

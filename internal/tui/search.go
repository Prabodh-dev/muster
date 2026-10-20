package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	matchStyle   = lipgloss.NewStyle().Background(lipgloss.Color("58")).Foreground(lipgloss.Color("230"))
	currentStyle = lipgloss.NewStyle().Background(lipgloss.Color("214")).Foreground(lipgloss.Color("0")).Bold(true)
)

// match is one occurrence of the query. start and end are byte offsets into
// the line's plain text (tabs expanded, colour codes removed).
type match struct {
	line       int
	start, end int
}

// plainText is the form of a log line that searching operates on.
func plainText(s string) string {
	return ansi.Strip(strings.ReplaceAll(s, "\t", "    "))
}

// compileQuery builds the case-insensitive literal matcher for query, or nil
// when the query is empty.
func compileQuery(query string) *regexp.Regexp {
	if query == "" {
		return nil
	}
	return regexp.MustCompile("(?i)" + regexp.QuoteMeta(query))
}

// findMatches returns every occurrence of query in lines, in order.
func findMatches(lines []string, query string) []match {
	re := compileQuery(query)
	if re == nil {
		return nil
	}
	var out []match
	for i, l := range lines {
		for _, loc := range re.FindAllStringIndex(plainText(l), -1) {
			out = append(out, match{line: i, start: loc[0], end: loc[1]})
		}
	}
	return out
}

// highlight renders line with the given matches (all on that line) styled.
// current is the index into ms of the match to emphasise, or -1. Colour codes
// in a matching line are dropped so highlights can never split an escape
// sequence.
func highlight(line string, ms []match, current int) string {
	text := plainText(line)
	var b strings.Builder
	pos := 0
	for i, m := range ms {
		b.WriteString(text[pos:m.start])
		style := matchStyle
		if i == current {
			style = currentStyle
		}
		b.WriteString(style.Render(text[m.start:m.end]))
		pos = m.end
	}
	b.WriteString(text[pos:])
	return b.String()
}

// rendered is a log laid out for the viewport.
type rendered struct {
	content  string
	rowStart []int // first viewport row of each log line
	matches  []match
}

// renderLog wraps lines to width and highlights query matches. current is the
// index into the returned matches of the match to emphasise (-1 for none).
func renderLog(lines []string, width int, query string, current int) rendered {
	width = max(width, 1)
	matches := findMatches(lines, query)

	byLine := map[int][]match{}
	firstIdx := map[int]int{} // log line -> index in matches of its first match
	for i, m := range matches {
		if _, ok := firstIdx[m.line]; !ok {
			firstIdx[m.line] = i
		}
		byLine[m.line] = append(byLine[m.line], m)
	}

	out := rendered{matches: matches, rowStart: make([]int, len(lines))}
	rows := make([]string, 0, len(lines))
	for i, l := range lines {
		out.rowStart[i] = len(rows)
		if ms, ok := byLine[i]; ok {
			l = highlight(l, ms, current-firstIdx[i])
		} else {
			l = strings.ReplaceAll(l, "\t", "    ")
		}
		rows = append(rows, strings.Split(ansi.Hardwrap(l, width, true), "\n")...)
	}
	out.content = strings.Join(rows, "\n")
	return out
}

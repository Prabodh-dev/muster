// Package ringbuf provides a fixed-capacity buffer of text lines that
// discards the oldest entries once full.
package ringbuf

// Buffer holds at most cap lines. It is not safe for concurrent use.
type Buffer struct {
	lines   []string
	start   int // index of the oldest line
	n       int // number of lines stored
	dropped int
}

// New returns a buffer that retains the last capacity lines. A capacity
// below 1 is treated as 1.
func New(capacity int) *Buffer {
	if capacity < 1 {
		capacity = 1
	}
	return &Buffer{lines: make([]string, capacity)}
}

// Push appends a line, evicting the oldest one if the buffer is full.
func (b *Buffer) Push(line string) {
	if b.n < len(b.lines) {
		b.lines[(b.start+b.n)%len(b.lines)] = line
		b.n++
		return
	}
	b.lines[b.start] = line
	b.start = (b.start + 1) % len(b.lines)
	b.dropped++
}

// Len reports how many lines are currently stored.
func (b *Buffer) Len() int { return b.n }

// Cap reports the maximum number of lines retained.
func (b *Buffer) Cap() int { return len(b.lines) }

// Dropped reports how many lines have been evicted so far.
func (b *Buffer) Dropped() int { return b.dropped }

// Lines returns a copy of the stored lines, oldest first.
func (b *Buffer) Lines() []string {
	out := make([]string, b.n)
	for i := range out {
		out[i] = b.lines[(b.start+i)%len(b.lines)]
	}
	return out
}

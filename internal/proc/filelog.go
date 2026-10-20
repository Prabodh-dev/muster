package proc

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// fileLog appends one process's output to a file. All methods are safe for
// concurrent use and are no-ops on a nil *fileLog, so callers need no
// "is logging on?" checks.
type fileLog struct {
	path string

	mu  sync.Mutex
	f   *os.File
	w   *bufio.Writer
	err error // first write error; logging stops after it
}

// openFileLog opens (creating it and its directory if needed) the log file
// for the named process in dir. Existing content is kept: each muster run
// appends.
func openFileLog(dir, name string) (*fileLog, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, logFileName(name))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &fileLog{path: path, f: f, w: bufio.NewWriterSize(f, 32*1024)}, nil
}

// logFileName maps a process name to a file name. Characters that are not
// safe in file names on every platform are percent-encoded, so distinct
// process names always get distinct files.
func logFileName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.' && b.Len() > 0:
			b.WriteRune(r)
		default:
			for _, c := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", c)
			}
		}
	}
	return b.String() + ".log"
}

// write appends raw output; newline ends the current line. It returns an
// error only the first time a write fails.
func (l *fileLog) write(text string, newline bool) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil
	}
	if _, err := l.w.WriteString(text); err != nil {
		l.err = err
		return err
	}
	if newline {
		if err := l.w.WriteByte('\n'); err != nil {
			l.err = err
			return err
		}
	}
	return nil
}

// note records a muster event (start, exit, ...) with a timestamp, and
// flushes so the entry survives a crash right after.
func (l *fileLog) note(text string) {
	if l == nil {
		return
	}
	_ = l.write(fmt.Sprintf("[muster %s] %s", time.Now().UTC().Format(time.RFC3339), text), true)
	l.flush()
}

func (l *fileLog) flush() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err == nil {
		if err := l.w.Flush(); err != nil {
			l.err = err
		}
	}
}

func (l *fileLog) close() {
	if l == nil {
		return
	}
	l.flush()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.f.Close()
}

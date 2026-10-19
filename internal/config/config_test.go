package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	src := `
buffer_lines: 100
processes:
  frontend:
    command: npm run dev
    cwd: ./web
    env:
      PORT: "3000"
  backend:
    command: go run ./cmd/api
`
	cfg, err := Parse([]byte(src), "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BufferLines != 100 {
		t.Errorf("BufferLines = %d, want 100", cfg.BufferLines)
	}
	if len(cfg.Processes) != 2 {
		t.Fatalf("got %d processes", len(cfg.Processes))
	}
	fe, be := cfg.Processes[0], cfg.Processes[1]
	if fe.Name != "frontend" || be.Name != "backend" {
		t.Errorf("order not preserved: %s, %s", fe.Name, be.Name)
	}
	if want := filepath.Join("/proj", "web"); fe.Cwd != want {
		t.Errorf("cwd = %q, want %q", fe.Cwd, want)
	}
	if fe.Env["PORT"] != "3000" {
		t.Errorf("env = %v", fe.Env)
	}
	if be.Cwd != filepath.Clean("/proj") {
		t.Errorf("default cwd = %q", be.Cwd)
	}
}

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse([]byte("processes:\n  a:\n    command: echo hi\n"), "/x")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BufferLines != DefaultBufferLines {
		t.Errorf("BufferLines = %d", cfg.BufferLines)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"empty", "", "empty"},
		{"bad yaml", "processes: [", "invalid yaml"},
		{"no processes", "buffer_lines: 5\n", `missing required "processes"`},
		{"processes not map", "processes: [a, b]\n", "must be a mapping"},
		{"no entries", "processes: {}\n", "at least one process"},
		{"missing command", "processes:\n  web:\n    cwd: .\n", `"web" is missing a command`},
		{"process not map", "processes:\n  web: npm start\n", `"web" must be a mapping`},
		{"duplicate", "processes:\n  a:\n    command: x\n  a:\n    command: y\n", "duplicate"},
		{"unknown field", "processes:\n  a:\n    command: x\n    cmd: y\n", `unknown field "cmd"`},
		{"unknown top-level", "procesess: {}\n", "invalid yaml"},
		{"negative buffer", "buffer_lines: -1\nprocesses:\n  a:\n    command: x\n", "buffer_lines"},
		{"bad env type", "processes:\n  a:\n    command: x\n    env: [1]\n", "process"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.src), "/x")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "muster.yaml")
	if err := os.WriteFile(p, []byte("processes:\n  a:\n    command: x\n    cwd: sub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "sub"); cfg.Processes[0].Cwd != want {
		t.Errorf("cwd = %q, want %q", cfg.Processes[0].Cwd, want)
	}
	if _, err := Load(filepath.Join(dir, "nope.yaml")); err == nil {
		t.Error("expected error for missing file")
	}
}

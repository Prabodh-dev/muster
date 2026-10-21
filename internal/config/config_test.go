package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		{"unknown top-level", "bogus_key: {}\n", `line 1: unknown field "bogus_key"`},
		{"negative buffer", "buffer_lines: -1\nprocesses:\n  a:\n    command: x\n", "line 1: buffer_lines must be at least 1"},
		{"bad env type", "processes:\n  a:\n    command: x\n    env: [1]\n", "line 4: env must be a mapping"},
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

func TestGracePeriod(t *testing.T) {
	src := `
grace_period_seconds: 2.5
processes:
  a:
    command: x
  b:
    command: y
    grace_period_seconds: 0
`
	cfg, err := Parse([]byte(src), "/x")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Processes[0].GracePeriod; got != 2500*time.Millisecond {
		t.Errorf("global grace = %v", got)
	}
	if got := cfg.Processes[1].GracePeriod; got != 0 {
		t.Errorf("per-process grace = %v, want 0", got)
	}

	cfg, err = Parse([]byte("processes:\n  a:\n    command: x\n"), "/x")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Processes[0].GracePeriod != DefaultGracePeriod {
		t.Errorf("default grace = %v", cfg.Processes[0].GracePeriod)
	}

	for _, bad := range []string{
		"grace_period_seconds: -1\nprocesses:\n  a:\n    command: x\n",
		"processes:\n  a:\n    command: x\n    grace_period_seconds: 99999\n",
	} {
		if _, err := Parse([]byte(bad), "/x"); err == nil || !strings.Contains(err.Error(), "grace_period_seconds") {
			t.Errorf("expected grace error for %q, got %v", bad, err)
		}
	}
}

func TestRestartPolicy(t *testing.T) {
	src := `
processes:
  a:
    command: x
  b:
    command: y
    restart: on-failure
    max_restarts: 2
  c:
    command: z
    restart: always
    max_restarts: 0
`
	cfg, err := Parse([]byte(src), "/x")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		policy RestartPolicy
		max    int
	}{{RestartNever, DefaultMaxRestarts}, {RestartOnFailure, 2}, {RestartAlways, 0}}
	for i, w := range want {
		p := cfg.Processes[i]
		if p.Restart != w.policy || p.MaxRestarts != w.max {
			t.Errorf("%s: got %q/%d, want %q/%d", p.Name, p.Restart, p.MaxRestarts, w.policy, w.max)
		}
	}

	for _, bad := range []string{
		"processes:\n  a:\n    command: x\n    restart: sometimes\n",
		"processes:\n  a:\n    command: x\n    max_restarts: -1\n",
	} {
		if _, err := Parse([]byte(bad), "/x"); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestObservabilityFields(t *testing.T) {
	src := `
stats_interval_seconds: 1.5
logs:
  enabled: true
  dir: out/logs
processes:
  web:
    command: x
    ports: [3000, 8080]
  api:
    command: y
`
	cfg, err := Parse([]byte(src), "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StatsInterval != 1500*time.Millisecond {
		t.Errorf("StatsInterval = %v", cfg.StatsInterval)
	}
	if !cfg.Logs.Enabled || cfg.Logs.Dir != filepath.Join("/proj", "out", "logs") {
		t.Errorf("Logs = %+v", cfg.Logs)
	}
	if got := cfg.Processes[0].Ports; len(got) != 2 || got[0] != 3000 || got[1] != 8080 {
		t.Errorf("ports = %v", got)
	}
	if len(cfg.Processes[1].Ports) != 0 {
		t.Errorf("ports should be optional, got %v", cfg.Processes[1].Ports)
	}
}

func TestObservabilityDefaults(t *testing.T) {
	cfg, err := Parse([]byte("processes:\n  a:\n    command: x\n"), "/proj")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StatsInterval != DefaultStatsInterval {
		t.Errorf("StatsInterval = %v", cfg.StatsInterval)
	}
	if cfg.Logs.Enabled {
		t.Error("log files should be off by default")
	}
	if want := filepath.Join("/proj", ".muster", "logs"); cfg.Logs.Dir != want {
		t.Errorf("default log dir = %q, want %q", cfg.Logs.Dir, want)
	}

	cfg, err = Parse([]byte("stats_interval_seconds: 0\nprocesses:\n  a:\n    command: x\n"), "/proj")
	if err != nil || cfg.StatsInterval != 0 {
		t.Errorf("0 should switch sampling off: %v %v", cfg.StatsInterval, err)
	}
}

func TestObservabilityErrors(t *testing.T) {
	proc := "processes:\n  web:\n    command: x\n"
	tests := []struct {
		name, src, want string
	}{
		{"port zero", "processes:\n  web:\n    command: x\n    ports: [0]\n", "line 4: port must be between 1 and 65535, got 0"},
		{"port too big", "processes:\n  web:\n    command: x\n    ports: [3000, 70000]\n", "line 4: port must be between 1 and 65535, got 70000"},
		{"port not a number", "processes:\n  web:\n    command: x\n    ports: [http]\n", `port must be a whole number, got "http"`},
		{"port fractional", "processes:\n  web:\n    command: x\n    ports: [80.5]\n", "port must be a whole number"},
		{"ports not a list", "processes:\n  web:\n    command: x\n    ports: 3000\n", "line 4: ports must be a list"},
		{"port repeated", "processes:\n  web:\n    command: x\n    ports:\n      - 3000\n      - 3000\n", "line 6: port 3000 is listed twice"},
		{"port shared", "processes:\n  a:\n    command: x\n    ports: [3000]\n  b:\n    command: y\n    ports: [3000]\n", `line 6: process "b": port 3000 is already declared by process "a"`},
		{"interval text", "stats_interval_seconds: soon\n" + proc, `line 1: stats_interval_seconds must be a number, got "soon"`},
		{"interval negative", "stats_interval_seconds: -2\n" + proc, "line 1: stats_interval_seconds must be 0"},
		{"interval too fast", "stats_interval_seconds: 0.1\n" + proc, "line 1: stats_interval_seconds must be 0"},
		{"interval too slow", "stats_interval_seconds: 99999\n" + proc, "stats_interval_seconds must be 0"},
		{"buffer text", "buffer_lines: lots\n" + proc, `line 1: buffer_lines must be a whole number, got "lots"`},
		{"buffer zero", "buffer_lines: 0\n" + proc, "line 1: buffer_lines must be at least 1"},
		{"logs not a map", "logs: yes\n" + proc, `line 1: "logs" must be a mapping`},
		{"logs enabled text", "logs:\n  enabled: maybe\n" + proc, `line 2: logs.enabled must be true or false, got "maybe"`},
		{"logs unknown key", "logs:\n  path: x\n" + proc, `line 2: unknown field "path"`},
		{"logs empty dir", "logs:\n  dir: \"\"\n" + proc, "line 2: logs.dir must not be empty"},
		{"grace text", "processes:\n  web:\n    command: x\n    grace_period_seconds: soon\n", "line 4: grace_period_seconds must be a number"},
		{"restart policy line", "processes:\n  web:\n    command: x\n    restart: sometimes\n", "line 4: restart must be never"},
		{"max restarts text", "processes:\n  web:\n    command: x\n    max_restarts: many\n", "line 4: max_restarts must be a whole number"},
		{"top level not a map", "- a\n- b\n", "line 1: config must be a mapping"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.src), "/x")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

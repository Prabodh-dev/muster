// Package config loads and validates muster.yaml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultFile is the config file muster looks for when none is given.
const DefaultFile = "muster.yaml"

// DefaultBufferLines is the per-process log history kept when the config
// does not say otherwise.
const DefaultBufferLines = 5000

// DefaultGracePeriod is how long a process gets to exit after the polite
// termination signal before it is force-killed.
const DefaultGracePeriod = 5 * time.Second

// RestartPolicy says when an exited process is relaunched automatically.
type RestartPolicy string

const (
	RestartNever     RestartPolicy = "never"      // default
	RestartOnFailure RestartPolicy = "on-failure" // only after a non-zero exit
	RestartAlways    RestartPolicy = "always"     // after any exit
)

// DefaultMaxRestarts is how many consecutive automatic restarts are attempted
// before giving up.
const DefaultMaxRestarts = 5

// DefaultStatsInterval is how often CPU and memory are sampled.
const DefaultStatsInterval = 2 * time.Second

// MinStatsInterval is the shortest allowed sampling interval; sampling walks
// the whole process table, so going faster than this just burns CPU.
const MinStatsInterval = 500 * time.Millisecond

// DefaultLogDir is where log files go, relative to the config file, when log
// persistence is on and no directory is given.
const DefaultLogDir = ".muster/logs"

// Process describes one supervised process.
type Process struct {
	Name    string
	Command string
	Cwd     string // absolute, or empty for the config file's directory
	Env     map[string]string

	// GracePeriod is how long a stop waits for the process group to exit
	// before force-killing it. Zero means kill immediately.
	GracePeriod time.Duration

	Restart RestartPolicy
	// MaxRestarts caps consecutive automatic restarts; a run that stays up
	// long enough resets the count.
	MaxRestarts int

	// Ports lists the TCP ports the process is expected to listen on. They
	// are checked for conflicts before anything is started.
	Ports []int
}

// LogConfig controls writing each process's output to disk.
type LogConfig struct {
	Enabled bool
	Dir     string // absolute
}

// Config is a parsed and validated muster.yaml.
type Config struct {
	BufferLines int
	// StatsInterval is the CPU/memory sampling period; zero disables sampling.
	StatsInterval time.Duration
	Logs          LogConfig
	Processes     []Process // in file order
}

// Load reads and parses the file at path. Relative cwd values are resolved
// against the directory containing the file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(data, filepath.Dir(abs))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

var (
	topLevelKeys = []string{"buffer_lines", "grace_period_seconds", "stats_interval_seconds", "logs", "processes"}
	processKeys  = []string{"command", "cwd", "env", "grace_period_seconds", "restart", "max_restarts", "ports"}
	logKeys      = []string{"enabled", "dir"}
)

// Parse parses config data. baseDir is used to resolve relative paths.
// Every error names the line it came from.
func Parse(data []byte, baseDir string) (*Config, error) {
	var doc yaml.Node
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("config is empty")
		}
		return nil, fmt.Errorf("invalid yaml: %w", err)
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: config must be a mapping of settings", root.Line)
	}
	top, err := fields(root, topLevelKeys)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		BufferLines:   DefaultBufferLines,
		StatsInterval: DefaultStatsInterval,
		Logs:          LogConfig{Dir: filepath.Clean(filepath.Join(baseDir, DefaultLogDir))},
	}
	if n := top["buffer_lines"]; n != nil {
		v, err := intValue(n, "buffer_lines")
		if err != nil {
			return nil, err
		}
		if v < 1 {
			return nil, atLine(n, "buffer_lines must be at least 1, got %d", v)
		}
		cfg.BufferLines = v
	}
	grace := DefaultGracePeriod
	if n := top["grace_period_seconds"]; n != nil {
		if grace, err = graceValue(n); err != nil {
			return nil, err
		}
	}
	if n := top["stats_interval_seconds"]; n != nil {
		if cfg.StatsInterval, err = statsInterval(n); err != nil {
			return nil, err
		}
	}
	if n := top["logs"]; n != nil {
		if cfg.Logs, err = parseLogs(n, baseDir); err != nil {
			return nil, err
		}
	}

	pn := top["processes"]
	if pn == nil {
		return nil, errors.New(`missing required "processes" section`)
	}
	if pn.Kind != yaml.MappingNode {
		return nil, atLine(pn, `"processes" must be a mapping of name to process`)
	}
	if len(pn.Content) == 0 {
		return nil, atLine(pn, `"processes" must define at least one process`)
	}

	seen := map[string]bool{}
	portOwner := map[int]string{}
	for i := 0; i < len(pn.Content); i += 2 {
		keyNode, valNode := pn.Content[i], pn.Content[i+1]
		name := keyNode.Value
		if name == "" {
			return nil, atLine(keyNode, "process name must not be empty")
		}
		if seen[name] {
			return nil, atLine(keyNode, "duplicate process %q", name)
		}
		seen[name] = true

		proc, err := parseProcess(name, valNode, baseDir, grace)
		if err != nil {
			return nil, err
		}
		for _, port := range proc.Ports {
			if other, ok := portOwner[port]; ok {
				return nil, atLine(valNode, "process %q: port %d is already declared by process %q", name, port, other)
			}
			portOwner[port] = name
		}
		cfg.Processes = append(cfg.Processes, proc)
	}
	return cfg, nil
}

func parseProcess(name string, n *yaml.Node, baseDir string, grace time.Duration) (Process, error) {
	if n.Kind != yaml.MappingNode {
		return Process{}, atLine(n, "process %q must be a mapping with a command", name)
	}
	f, err := fields(n, processKeys)
	if err != nil {
		return Process{}, fmt.Errorf("process %q: %w", name, err)
	}
	fail := func(err error) (Process, error) { return Process{}, fmt.Errorf("process %q: %w", name, err) }

	p := Process{
		Name:        name,
		GracePeriod: grace,
		Restart:     RestartNever,
		MaxRestarts: DefaultMaxRestarts,
		Cwd:         baseDir,
	}
	if c := f["command"]; c != nil {
		if p.Command, err = stringValue(c, "command"); err != nil {
			return fail(err)
		}
	}
	if p.Command == "" {
		return Process{}, atLine(n, "process %q is missing a command", name)
	}
	if c := f["cwd"]; c != nil {
		cwd, err := stringValue(c, "cwd")
		if err != nil {
			return fail(err)
		}
		if cwd != "" {
			p.Cwd = cwd
			if !filepath.IsAbs(cwd) {
				p.Cwd = filepath.Join(baseDir, cwd)
			}
		}
	}
	p.Cwd = filepath.Clean(p.Cwd)
	if e := f["env"]; e != nil {
		if e.Kind != yaml.MappingNode {
			return fail(atLine(e, "env must be a mapping of KEY: value"))
		}
		if err := e.Decode(&p.Env); err != nil {
			return fail(atLine(e, "env values must be strings, numbers or booleans: %v", err))
		}
	}
	if g := f["grace_period_seconds"]; g != nil {
		if p.GracePeriod, err = graceValue(g); err != nil {
			return fail(err)
		}
	}
	if r := f["restart"]; r != nil {
		v, err := stringValue(r, "restart")
		if err != nil {
			return fail(err)
		}
		switch pol := RestartPolicy(v); pol {
		case RestartNever, RestartOnFailure, RestartAlways:
			p.Restart = pol
		default:
			return fail(atLine(r, "restart must be never, on-failure or always, got %q", v))
		}
	}
	if m := f["max_restarts"]; m != nil {
		v, err := intValue(m, "max_restarts")
		if err != nil {
			return fail(err)
		}
		if v < 0 {
			return fail(atLine(m, "max_restarts must not be negative, got %d", v))
		}
		p.MaxRestarts = v
	}
	if pn := f["ports"]; pn != nil {
		if p.Ports, err = parsePorts(pn); err != nil {
			return fail(err)
		}
	}
	return p, nil
}

func parsePorts(n *yaml.Node) ([]int, error) {
	if n.Kind != yaml.SequenceNode {
		return nil, atLine(n, "ports must be a list of port numbers, e.g. [3000, 8080]")
	}
	var ports []int
	seen := map[int]bool{}
	for _, item := range n.Content {
		v, err := intValue(item, "port")
		if err != nil {
			return nil, err
		}
		if v < 1 || v > 65535 {
			return nil, atLine(item, "port must be between 1 and 65535, got %d", v)
		}
		if seen[v] {
			return nil, atLine(item, "port %d is listed twice", v)
		}
		seen[v] = true
		ports = append(ports, v)
	}
	return ports, nil
}

func parseLogs(n *yaml.Node, baseDir string) (LogConfig, error) {
	if n.Kind != yaml.MappingNode {
		return LogConfig{}, atLine(n, `"logs" must be a mapping with enabled and dir`)
	}
	f, err := fields(n, logKeys)
	if err != nil {
		return LogConfig{}, fmt.Errorf("logs: %w", err)
	}
	var lc LogConfig
	dir := DefaultLogDir
	if e := f["enabled"]; e != nil {
		if e.Kind != yaml.ScalarNode || (e.Value != "true" && e.Value != "false") {
			return LogConfig{}, atLine(e, "logs.enabled must be true or false, got %q", e.Value)
		}
		lc.Enabled = e.Value == "true"
	}
	if d := f["dir"]; d != nil {
		if dir, err = stringValue(d, "logs.dir"); err != nil {
			return LogConfig{}, err
		}
		if dir == "" {
			return LogConfig{}, atLine(d, "logs.dir must not be empty")
		}
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(baseDir, dir)
	}
	lc.Dir = filepath.Clean(dir)
	return lc, nil
}

// fields indexes a mapping node by key, rejecting keys not in allowed.
func fields(n *yaml.Node, allowed []string) (map[string]*yaml.Node, error) {
	out := map[string]*yaml.Node{}
next:
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i]
		for _, a := range allowed {
			if k.Value == a {
				out[k.Value] = n.Content[i+1]
				continue next
			}
		}
		return nil, atLine(k, "unknown field %q", k.Value)
	}
	return out, nil
}

// atLine formats an error that points at the line of node n.
func atLine(n *yaml.Node, format string, args ...any) error {
	return fmt.Errorf("line %d: %s", n.Line, fmt.Sprintf(format, args...))
}

func stringValue(n *yaml.Node, what string) (string, error) {
	if n.Kind != yaml.ScalarNode {
		return "", atLine(n, "%s must be a string", what)
	}
	return n.Value, nil
}

func intValue(n *yaml.Node, what string) (int, error) {
	if n.Kind != yaml.ScalarNode {
		return 0, atLine(n, "%s must be a whole number", what)
	}
	v, err := strconv.Atoi(n.Value)
	if err != nil {
		return 0, atLine(n, "%s must be a whole number, got %q", what, n.Value)
	}
	return v, nil
}

func floatValue(n *yaml.Node, what string) (float64, error) {
	if n.Kind != yaml.ScalarNode {
		return 0, atLine(n, "%s must be a number", what)
	}
	v, err := strconv.ParseFloat(n.Value, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, atLine(n, "%s must be a number, got %q", what, n.Value)
	}
	return v, nil
}

func graceValue(n *yaml.Node) (time.Duration, error) {
	v, err := floatValue(n, "grace_period_seconds")
	if err != nil {
		return 0, err
	}
	if v < 0 || v > 3600 {
		return 0, atLine(n, "grace_period_seconds must be between 0 and 3600, got %v", v)
	}
	return time.Duration(v * float64(time.Second)), nil
}

func statsInterval(n *yaml.Node) (time.Duration, error) {
	v, err := floatValue(n, "stats_interval_seconds")
	if err != nil {
		return 0, err
	}
	d := time.Duration(v * float64(time.Second))
	if v != 0 && (d < MinStatsInterval || v > 3600) {
		return 0, atLine(n, "stats_interval_seconds must be 0 (off) or between %v and 3600, got %v",
			MinStatsInterval.Seconds(), v)
	}
	return d, nil
}

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
	"sort"
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
}

// Config is a parsed and validated muster.yaml.
type Config struct {
	BufferLines int
	Processes   []Process // in file order
}

type rawConfig struct {
	BufferLines        int       `yaml:"buffer_lines"`
	GracePeriodSeconds *float64  `yaml:"grace_period_seconds"`
	Processes          yaml.Node `yaml:"processes"`
}

type rawProcess struct {
	Command            string            `yaml:"command"`
	Cwd                string            `yaml:"cwd"`
	Env                map[string]string `yaml:"env"`
	GracePeriodSeconds *float64          `yaml:"grace_period_seconds"`
	Restart            string            `yaml:"restart"`
	MaxRestarts        *int              `yaml:"max_restarts"`
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

// Parse parses config data. baseDir is used to resolve relative cwd values.
func Parse(data []byte, baseDir string) (*Config, error) {
	var raw rawConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("config is empty")
		}
		return nil, fmt.Errorf("invalid yaml: %w", err)
	}

	cfg := &Config{BufferLines: raw.BufferLines}
	switch {
	case cfg.BufferLines < 0:
		return nil, fmt.Errorf("buffer_lines must be positive, got %d", cfg.BufferLines)
	case cfg.BufferLines == 0:
		cfg.BufferLines = DefaultBufferLines
	}

	grace := DefaultGracePeriod
	if raw.GracePeriodSeconds != nil {
		g, err := graceDuration(*raw.GracePeriodSeconds)
		if err != nil {
			return nil, err
		}
		grace = g
	}

	node := raw.Processes
	if node.Kind == 0 {
		return nil, errors.New(`missing required "processes" section`)
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf(`line %d: "processes" must be a mapping of name to process`, node.Line)
	}
	if len(node.Content) == 0 {
		return nil, errors.New(`"processes" must define at least one process`)
	}

	seen := map[string]bool{}
	for i := 0; i < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
		name := keyNode.Value
		if name == "" {
			return nil, fmt.Errorf("line %d: process name must not be empty", keyNode.Line)
		}
		if seen[name] {
			return nil, fmt.Errorf("line %d: duplicate process %q", keyNode.Line, name)
		}
		seen[name] = true

		var rp rawProcess
		if valNode.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("line %d: process %q must be a mapping with a command", valNode.Line, name)
		}
		if err := decodeStrict(valNode, &rp); err != nil {
			return nil, fmt.Errorf("process %q: %w", name, err)
		}
		if rp.Command == "" {
			return nil, fmt.Errorf("line %d: process %q is missing a command", valNode.Line, name)
		}
		procGrace := grace
		if rp.GracePeriodSeconds != nil {
			g, err := graceDuration(*rp.GracePeriodSeconds)
			if err != nil {
				return nil, fmt.Errorf("process %q: %w", name, err)
			}
			procGrace = g
		}
		policy := RestartPolicy(rp.Restart)
		switch policy {
		case "":
			policy = RestartNever
		case RestartNever, RestartOnFailure, RestartAlways:
		default:
			return nil, fmt.Errorf("process %q: restart must be never, on-failure or always, got %q", name, rp.Restart)
		}
		maxRestarts := DefaultMaxRestarts
		if rp.MaxRestarts != nil {
			if *rp.MaxRestarts < 0 {
				return nil, fmt.Errorf("process %q: max_restarts must not be negative, got %d", name, *rp.MaxRestarts)
			}
			maxRestarts = *rp.MaxRestarts
		}
		cwd := rp.Cwd
		if cwd != "" && !filepath.IsAbs(cwd) {
			cwd = filepath.Join(baseDir, cwd)
		}
		if cwd == "" {
			cwd = baseDir
		}
		cfg.Processes = append(cfg.Processes, Process{
			Name:    name,
			Command: rp.Command,
			Cwd:     filepath.Clean(cwd),
			Env:     rp.Env,

			GracePeriod: procGrace,
			Restart:     policy,
			MaxRestarts: maxRestarts,
		})
	}
	return cfg, nil
}

func graceDuration(seconds float64) (time.Duration, error) {
	if math.IsNaN(seconds) || seconds < 0 || seconds > 3600 {
		return 0, fmt.Errorf("grace_period_seconds must be between 0 and 3600, got %v", seconds)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func decodeStrict(n *yaml.Node, out any) error {
	// Node.Decode has no KnownFields option, so check keys by hand.
	allowed := map[string]bool{"command": true, "cwd": true, "env": true, "grace_period_seconds": true, "restart": true, "max_restarts": true}
	var unknown []string
	for i := 0; i < len(n.Content); i += 2 {
		if k := n.Content[i]; !allowed[k.Value] {
			unknown = append(unknown, fmt.Sprintf("line %d: unknown field %q", k.Line, k.Value))
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return errors.New(unknown[0])
	}
	if err := n.Decode(out); err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	return nil
}

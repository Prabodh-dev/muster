// Command muster runs and supervises the processes listed in muster.yaml.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Prabodh-dev/muster/internal/config"
	"github.com/Prabodh-dev/muster/internal/proc"
	"github.com/Prabodh-dev/muster/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "muster:", err)
		os.Exit(1)
	}
}

func run() error {
	path := flag.String("f", config.DefaultFile, "path to the config file")
	force := flag.Bool("ignore-port-conflicts", false, "start even if a declared port is already in use")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: muster [-f %s] [-ignore-port-conflicts]\n", config.DefaultFile)
		flag.PrintDefaults()
	}
	flag.Parse()

	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}

	// Before spawning anything, so a clash is explained rather than left to
	// show up as two confusing crashes.
	if err := checkPorts(cfg.Processes, *force, isTerminal(os.Stdin), os.Stdin, os.Stderr); err != nil {
		os.Exit(1)
	}

	ports := map[string][]int{}
	for _, p := range cfg.Processes {
		if len(p.Ports) > 0 {
			ports[p.Name] = p.Ports
		}
	}

	sup := proc.New(cfg)
	// Covers every exit path, including the program being killed by a signal.
	defer sup.Shutdown()
	p := tea.NewProgram(tui.New(sup, ports), tea.WithAltScreen(), tea.WithMouseCellMotion())

	// Bubble Tea handles SIGINT/SIGTERM itself; a closed terminal sends
	// SIGHUP, which would otherwise kill us and orphan the children.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		<-hup
		p.Quit()
	}()

	_, err = p.Run()
	return err
}

func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

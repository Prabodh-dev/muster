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
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "muster:", err)
		os.Exit(1)
	}
}

func run() error {
	path := flag.String("f", config.DefaultFile, "path to the config file")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: muster [-f %s]\n", config.DefaultFile)
		flag.PrintDefaults()
	}
	flag.Parse()

	cfg, err := config.Load(*path)
	if err != nil {
		return err
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

// Command muster runs and supervises the processes listed in muster.yaml.
package main

import (
	"flag"
	"fmt"
	"os"

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

	sup := proc.New(cfg)
	p := tea.NewProgram(tui.New(sup), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

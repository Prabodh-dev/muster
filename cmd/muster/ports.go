package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Prabodh-dev/muster/internal/config"
	"github.com/Prabodh-dev/muster/internal/portcheck"
)

var errPortConflicts = errors.New("not starting because of port conflicts")

// checkPorts looks for declared ports that are already taken and, if there
// are any, explains them before anything has been spawned. With force the
// warning is printed and startup continues. Otherwise an interactive session
// is asked whether to carry on, and a non-interactive one is refused.
func checkPorts(procs []config.Process, force, interactive bool, in io.Reader, out io.Writer) error {
	conflicts := portcheck.Check(procs)
	if len(conflicts) == 0 {
		return nil
	}

	fmt.Fprintln(out, "muster: some declared ports are already in use. Nothing has been started yet.")
	fmt.Fprintln(out)
	for _, c := range conflicts {
		fmt.Fprintf(out, "  %s\n", c)
	}
	fmt.Fprintln(out)

	switch {
	case force:
		fmt.Fprintln(out, "Starting anyway (-ignore-port-conflicts).")
		return nil
	case !interactive:
		fmt.Fprintln(out, "Free the ports, or run with -ignore-port-conflicts to start anyway.")
		return errPortConflicts
	}
	fmt.Fprint(out, "Those processes will probably fail to bind. Start anyway? [y/N] ")
	answer, _ := bufio.NewReader(in).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a == "y" || a == "yes" {
		return nil
	}
	return errPortConflicts
}

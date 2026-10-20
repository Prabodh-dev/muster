// Command demo is a tiny, dependency-free program used by the example
// muster.yaml. Running it through `go run` gives the example the same
// behaviour on Linux, macOS and Windows, with no shell syntax involved.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	name := flag.String("name", "demo", "label printed in front of every line")
	every := flag.Duration("every", time.Second, "delay between lines")
	count := flag.Int("count", 0, "number of lines to print before exiting (0 = run until stopped)")
	exit := flag.Int("exit", 0, "exit code to use once -count lines have been printed")
	listen := flag.String("listen", "", "TCP address to hold open while running, e.g. 127.0.0.1:47001")
	flag.Parse()

	if *listen != "" {
		l, err := net.Listen("tcp", *listen)
		if err != nil {
			fmt.Printf("%s: cannot listen on %s: %v\n", *name, *listen, err)
			os.Exit(1)
		}
		defer l.Close()
		fmt.Printf("%s: listening on %s\n", *name, l.Addr())
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	greeting := os.Getenv("GREETING")
	if greeting == "" {
		greeting = "hello"
	}
	fmt.Printf("%s: %s from pid %d\n", *name, greeting, os.Getpid())

	tick := time.NewTicker(*every)
	defer tick.Stop()
	for n := 1; *count == 0 || n <= *count; n++ {
		select {
		case <-tick.C:
			fmt.Printf("%s: tick %d\n", *name, n)
		case <-stop:
			fmt.Printf("%s: shutting down\n", *name)
			return
		}
	}
	fmt.Printf("%s: finished, exiting with code %d\n", *name, *exit)
	os.Exit(*exit)
}

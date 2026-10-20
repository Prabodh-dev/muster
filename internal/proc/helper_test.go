package proc

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// The tests launch this very test binary as the child process, selected by
// an environment variable. That gives every platform the same fake
// "dev server": one that spawns children, handles signals, or exits on cue,
// without relying on shell builtins.
const helperEnv = "MUSTER_TEST_HELPER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		runHelper(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runHelper(mode string) {
	switch mode {
	case "sleep":
		time.Sleep(10 * time.Minute)

	case "tree":
		// Like `npm run dev`: a parent that spawns a long-lived child.
		fmt.Println("self=" + strconv.Itoa(os.Getpid()))
		fmt.Println("child=" + strconv.Itoa(spawnSelf("sleep")))
		time.Sleep(10 * time.Minute)

	case "orphan":
		// Spawns a child and exits straight away, leaving the child behind.
		fmt.Println("child=" + strconv.Itoa(spawnSelf("sleep")))

	case "graceful":
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		fmt.Println("ready")
		<-sig
		fmt.Println("graceful-exit")

	case "stubborn":
		signal.Notify(make(chan os.Signal, 1), os.Interrupt, syscall.SIGTERM)
		fmt.Println("ready")
		time.Sleep(10 * time.Minute)

	case "spin":
		// Burns one core.
		fmt.Println("spinning")
		x := 0
		for {
			x++
			if x < 0 {
				return
			}
		}

	case "lines":
		n, _ := strconv.Atoi(os.Getenv("MUSTER_TEST_LINES"))
		for i := 1; i <= n; i++ {
			fmt.Println("line " + strconv.Itoa(i))
		}
		time.Sleep(10 * time.Minute)

	case "exit":
		code, _ := strconv.Atoi(os.Getenv("MUSTER_TEST_EXIT"))
		fmt.Println("running")
		os.Exit(code)
	}
}

func spawnSelf(mode string) int {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"="+mode)
	if err := cmd.Start(); err != nil {
		fmt.Println("spawn failed:", err)
		os.Exit(2)
	}
	return cmd.Process.Pid
}

// helperCmd is a muster command line that runs this binary in the given mode.
// The path is quoted so it survives both /bin/sh and cmd.exe.
func helperCmd(mode string) (string, map[string]string) {
	return fmt.Sprintf("%q", os.Args[0]), map[string]string{helperEnv: mode}
}

func awaitDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("pid %d still alive", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

# muster

Run your whole local dev stack from one terminal window.

`muster` reads a config file, starts every process listed in it (frontend dev
server, API, worker, ...), and shows them in a small TUI: a sidebar with each
process and its status, and a log pane for whichever one you have selected.
Quit and everything it started goes away with it.

## Why

You already have options for this; muster is a different set of trade-offs.

- **Multiple terminal tabs** – works until you have five of them and forget
  which one died.
- **[concurrently](https://github.com/open-cli-tools/concurrently) /
  [foreman](https://github.com/ddollar/foreman)** – start everything and
  interleave all output into one stream, prefixed by name. Fine for three
  quiet processes, hard to read when two are chatty and one is the one you
  care about.
- **[overmind](https://github.com/DarthSim/overmind)** – gives each process
  its own pane, but does it by running them inside tmux, so you need tmux
  installed and it is Unix-only.

muster keeps each process's output separate (like overmind) without tmux, in a
single static Go binary, and tells you at a glance which process is running,
exited cleanly, or crashed.

## Install

Requires Go 1.24 or newer.

```sh
go install github.com/Prabodh-dev/muster/cmd/muster@latest
```

Or from a checkout:

```sh
go build -o muster ./cmd/muster
```

## Quickstart

1. Create a `muster.yaml` in your project:

   ```yaml
   processes:
     frontend:
       command: npm run dev
       cwd: ./web
     backend:
       command: go run ./cmd/api
       env:
         PORT: "8080"
       restart: on-failure
   ```

2. Run it:

   ```sh
   muster            # uses ./muster.yaml
   muster -f path/to/other.yaml
   ```

   This repository's own `muster.yaml` is a runnable example:
   `go run ./cmd/muster` from a checkout starts three demo processes.

### Keys

| Key                          | Action                                         |
| ---------------------------- | ---------------------------------------------- |
| `↑` / `k`                    | select previous process                        |
| `↓` / `j`                    | select next process                            |
| `r`                          | restart the selected process (starts it if it is stopped) |
| `s`                          | stop the selected process                      |
| `enter`                      | start the selected process if it is not running |
| `/`                          | search the selected process's log              |
| `n` / `N`                    | next / previous match                          |
| `esc`                        | clear the search                               |
| `pgup` / `pgdn`, mouse wheel | scroll the log                                 |
| `g` / `G`                    | jump to top / bottom of the log                |
| `q` / `ctrl+c`               | stop every process and quit                    |

`r`, `s` and `enter` only ever act on the selected process; the others keep
running untouched. The log follows new output while you are at the bottom;
scroll up and it stays put until you jump back down with `G`.

**Searching.** Press `/` and type: matches are highlighted as you type
(case-insensitive, literal text, not a regex) and the view jumps to the first
one. `enter` closes the prompt and keeps the search active so `n` / `N` can
step through matches (wrapping around); `esc` clears it. New output is
searched as it arrives, and the search carries over when you select another
process. While the prompt is open every key goes to it, so `q` types a `q`.
A line containing a match is shown without its colour codes so highlights
cannot be split by them.

Status indicators: `●` running, `○` exited with code 0, `✗` crashed (non-zero
exit code, killed by a signal, or failed to start), `■` stopped by you, `↻`
waiting to be restarted automatically.

## Config format

muster uses **`muster.yaml`**. Unknown keys are rejected, so typos are caught
instead of silently ignored.

```yaml
buffer_lines: 5000          # optional: log lines kept per process (default 5000)
grace_period_seconds: 5     # optional: default shutdown grace period (default 5)

processes:                  # required, at least one entry; order is the sidebar order
  <name>:
    command: <shell command>     # required
    cwd: <directory>             # optional, relative to the config file; default: its directory
    env:                         # optional, added on top of muster's own environment
      KEY: value
    grace_period_seconds: 10     # optional: overrides the global grace period
    restart: never               # optional: never (default) | on-failure | always
    max_restarts: 5              # optional: consecutive automatic restarts (default 5)
```

- stdout and stderr are merged into one stream per process.
- Each process keeps only its last `buffer_lines` lines, so a chatty
  long-running process cannot grow memory without bound.

### Commands and the shell

`command` is handed to the operating system's default shell exactly as
written: `/bin/sh -c` on Linux and macOS, `cmd /C` on Windows. muster does not
translate anything, so pipes, `&&`, `$VAR` expansion, `while` loops and so on
work only where that shell supports them, and a config that uses them is not
portable across platforms. `env` values are placed in the process
environment, so a program can read them on any platform, but `$NAME` /
`%NAME%` expansion inside the command string is up to the shell.

If you want one config to work everywhere, keep commands to plain
program invocations (`npm run dev`, `go run ./cmd/api`, `python -m http.server`)
and read settings from the environment inside the program. The example
`muster.yaml` in this repository does exactly that: it runs a small Go program
(`examples/demo`) through `go run`, so it behaves the same on Linux, macOS and
Windows.

### Stopping and the grace period

Stopping a process (`s`, `r`, quitting muster) is done in two steps:

1. muster asks the process and **everything it started** to terminate. On
   Linux and macOS each process is launched in its own process group and the
   whole group receives `SIGTERM`. On Windows each process is placed in a job
   object and its console process group receives `CTRL_BREAK`.
2. muster waits up to `grace_period_seconds` for the whole group to be gone.
   If anything is still running after that, the entire group is force-killed
   (`SIGKILL`, or terminating the job object on Windows).

Signalling the group rather than only the top-level process matters for
things like `npm run dev`, whose child is the one actually holding the port.
A process that handles the signal and exits quickly is not made to wait out
the full grace period. `grace_period_seconds: 0` skips the polite step and
kills immediately.

If a command exits on its own while leaving children behind, muster kills
those leftovers right away rather than letting them linger as orphans. A
program that deliberately daemonizes into a new session or process group is
outside muster's control and will not be touched.

### Restart policies

`restart` decides what happens when a process exits by itself (stopping it
with `s` or `r` never triggers an automatic restart):

| Policy       | Behaviour                                            |
| ------------ | ---------------------------------------------------- |
| `never`      | default; the process stays down                      |
| `on-failure` | restart only after a non-zero exit, a signal, or a failure to start |
| `always`     | restart after any exit, including a clean one        |

Restarts back off exponentially so a crash-looping process cannot spam: the
delays are 1s, 2s, 4s, 8s, 16s and then 30s for every further attempt. After
`max_restarts` consecutive attempts muster gives up and leaves the process
crashed (or exited); pressing `r` or `enter` brings it back and resets the
count. A run that stays up for 10 seconds counts as healthy and also resets
it. The restart is announced in the process's log (`[muster] exited with code
1; restart 2/5 in 2s`) and the sidebar shows `↻` while it waits.

## How it works

Every process gets a goroutine that reads its output pipe line by line. All of
those goroutines send into one channel, which the Bubble Tea `Update` loop
consumes, so one process's output can never block another's or the UI. A
separate goroutine per process owns its lifecycle (start, stop, restart, the
restart policy), so a command from the UI never races with the process
exiting.

## Status and limitations

- Windows support is implemented with job objects and `CTRL_BREAK`, and is
  cross-compiled and unit-tested on Windows in CI, but it has had less
  hands-on use than Linux. `CTRL_BREAK` needs a console, so the test that a
  process can shut itself down gracefully runs on Linux and macOS only; the
  process-tree tests run everywhere.
- Log search is a plain case-insensitive substring match. A filter mode that
  hides non-matching lines is not implemented yet.
- Restart backoff timings (1s doubling to 30s, 10s to count as healthy) are
  fixed rather than configurable.

## Development

```sh
go vet ./...
go test -race ./...
```

The process-control tests re-run the test binary as a stand-in dev server
(one that spawns children, ignores signals, or exits with a chosen code), so
they need no shell tools and run the same on every platform.

## License

MIT

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
   ```

2. Run it:

   ```sh
   muster            # uses ./muster.yaml
   muster -f path/to/other.yaml
   ```

### Keys

| Key                 | Action                          |
| ------------------- | ------------------------------- |
| `↑` / `k`           | select previous process         |
| `↓` / `j`           | select next process             |
| `pgup` / `pgdn`, mouse wheel | scroll the log         |
| `g` / `G`           | jump to top / bottom of the log |
| `q` / `ctrl+c`      | quit and stop all processes     |

The log follows new output while you are at the bottom; scroll up and it stays
put until you jump back down with `G`.

Status indicators: `●` running, `○` exited with code 0, `✗` crashed (non-zero
exit code, killed by a signal, or failed to start).

## Config format

muster uses **`muster.yaml`**. Unknown keys are rejected, so typos are caught
instead of silently ignored.

```yaml
buffer_lines: 5000        # optional: log lines kept per process (default 5000)

processes:                # required, at least one entry; order is the sidebar order
  <name>:
    command: <shell command>   # required
    cwd: <directory>           # optional, relative to the config file; default: its directory
    env:                       # optional, added on top of muster's own environment
      KEY: value
```

- `command` is run through the shell (`/bin/sh -c` on Unix, `cmd /C` on
  Windows), so pipes, `&&`, and environment expansion work.
- stdout and stderr are merged into one stream per process.
- Each process keeps only its last `buffer_lines` lines, so a chatty
  long-running process cannot grow memory without bound.

## How it works

Every process gets a goroutine that reads its output pipe line by line. All of
those goroutines send into one channel, which the Bubble Tea `Update` loop
consumes, so one process's output can never block another's or the UI. Each
child runs in its own process group, and on quit muster signals the whole
group (SIGTERM, then SIGKILL after two seconds), so children and anything
they spawned do not outlive muster. A terminal hangup (SIGHUP) is handled the
same way.

## Status and limitations

This is the first phase: spawning, output capture, and the basic TUI.

- No restart, stop or start of individual processes yet.
- Shutdown uses a fixed two-second grace period before force-killing.
- Windows support is implemented (process trees are stopped with
  `taskkill /T`) but only cross-compiled and unit-tested there in CI, not
  exercised by hand. The test that checks child processes are really gone
  after quit runs on Linux and macOS only.

## Development

```sh
go vet ./...
go test -race ./...
```

## License

MIT

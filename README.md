# muster

**Run your whole local dev stack from one terminal.** One config file, a log
pane per process, and live control over each of them: restart, stop, search,
and see what is using your CPU, without a pile of terminal tabs.

[![ci](https://github.com/Prabodh-dev/muster/actions/workflows/ci.yml/badge.svg)](https://github.com/Prabodh-dev/muster/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/Prabodh-dev/muster?include_prereleases)](https://github.com/Prabodh-dev/muster/releases)
[![license](https://img.shields.io/github/license/Prabodh-dev/muster)](LICENSE)

![muster running four processes: browsing logs, a crash being caught and restarted, searching, and stopping and starting a process](docs/demo.gif)

```yaml
# muster.yaml
processes:
  frontend:
    command: npm run dev
    cwd: ./web
  backend:
    command: go run ./cmd/api
    ports: [8080]
    restart: on-failure
  worker:
    command: go run ./cmd/worker
```

```sh
muster
```

> **muster runs the commands in `muster.yaml`.** Treat that file like a
> `Makefile` or an npm install script: never run one from a repository you do
> not trust. See [SECURITY.md](SECURITY.md).

## Why muster

You already have options; muster makes different trade-offs.

- **Multiple terminal tabs** work until you have five of them and forget which
  one died.
- **[concurrently](https://github.com/open-cli-tools/concurrently) /
  [foreman](https://github.com/ddollar/foreman)** start everything and
  interleave all output into one stream, prefixed by name. Fine for three
  quiet processes, hard to read when two are chatty and one is the one you
  care about.
- **[overmind](https://github.com/DarthSim/overmind)** gives each process its
  own pane, but does it by running them inside tmux, so you need tmux
  installed and it is Unix-only.

muster keeps each process's output separate (like overmind) without tmux, as a
single static binary for Linux, macOS and Windows, and tells you at a glance
which process is running, exited cleanly, or crashed.

## Features

- **A log pane per process**, with a sidebar showing status at a glance
  (running, exited, crashed, stopped, restarting).
- **Restart, stop or start any one process** with a key press, without
  touching the others.
- **Graceful shutdown of the whole process tree**, not just the top-level
  process, so a dev server's children do not linger holding your port.
  Configurable grace period, then a force kill. Quitting muster leaves nothing
  behind.
- **Restart policies** (`never`, `on-failure`, `always`) with exponential
  backoff and a cap, so a crash loop cannot spam.
- **CPU, memory and uptime** for each process, summed over its whole tree.
- **Log search** with highlighting and next/previous match.
- **Port-conflict detection**: declare a process's `ports` and muster warns
  you *before* starting anything if one is already taken.
- **Optional log files** per process, for debugging after a crash.
- **Config errors with line numbers**, and a config format that is a short
  YAML file.
- One dependency-free binary; no tmux, no runtime.

## Install

### Download a release binary

Grab the archive for your platform from the
[Releases page](https://github.com/Prabodh-dev/muster/releases). Builds are
published for Linux, macOS and Windows on `amd64` and `arm64`, together with a
`muster_<version>_checksums.txt` file.

**Linux and macOS** (replace the version, OS and architecture):

```sh
VERSION=1.0.0 OS=linux ARCH=amd64      # OS: linux | darwin   ARCH: amd64 | arm64
BASE=https://github.com/Prabodh-dev/muster/releases/download/v$VERSION
curl -LO $BASE/muster_${VERSION}_${OS}_${ARCH}.tar.gz
curl -LO $BASE/muster_${VERSION}_checksums.txt

sha256sum -c --ignore-missing muster_${VERSION}_checksums.txt   # macOS: shasum -a 256 -c --ignore-missing ...
tar xzf muster_${VERSION}_${OS}_${ARCH}.tar.gz
sudo mv muster /usr/local/bin/
muster -version
```

**Windows**: download `muster_<version>_windows_amd64.zip` (or `arm64`),
unzip it, and put `muster.exe` somewhere on your `PATH`. To verify it, compare
`Get-FileHash muster_<version>_windows_amd64.zip` with the checksums file.

The binaries are not code-signed. macOS may block the first run; allow it in
*System Settings → Privacy & Security*, or run
`xattr -d com.apple.quarantine /usr/local/bin/muster`. Windows SmartScreen may
warn about an unrecognised publisher.

### With Go

Requires Go 1.24 or newer.

```sh
go install github.com/Prabodh-dev/muster/cmd/muster@latest
```

### From source

```sh
git clone https://github.com/Prabodh-dev/muster
cd muster
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
   muster                         # uses ./muster.yaml
   muster -f path/to/other.yaml
   muster -ignore-port-conflicts  # start even if a declared port is taken
   muster -version
   ```

3. Press `j`/`k` to move between processes, `r` to restart the selected one,
   `s` to stop it, `/` to search its log, and `q` to quit (which stops
   everything).

   Want to see it work before writing a config? This repository's own
   `muster.yaml` is a runnable demo (it needs Go installed, because it runs
   a tiny program through `go run`): clone the repo and run `go run
   ./cmd/muster`, or unpack a release archive, which includes the same demo
   config, and run `./muster` in the extracted folder.

## Using muster

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

### The sidebar

Each process takes two lines in the sidebar:

```
● backend
  2.3% · 145MB · 1m23s
```

While a process runs, the second line shows its CPU use, resident memory and
uptime. For anything that is not running it says what happened instead:
`exited`, `crashed (exit 3)`, `stopped` or `restarting…`. The header above the
log repeats the status, shows the ports the process declared, and notes when
the figures cover more than one process (`3 processes`).

Status indicators: `●` running, `○` exited with code 0, `✗` crashed (non-zero
exit code, killed by a signal, or failed to start), `■` stopped by you, `↻`
waiting to be restarted automatically.

## Configuration

muster uses **`muster.yaml`**. Unknown keys are rejected, so typos are caught
instead of silently ignored.

```yaml
buffer_lines: 5000          # optional: log lines kept per process (default 5000)
grace_period_seconds: 5     # optional: default shutdown grace period (default 5)
stats_interval_seconds: 2   # optional: CPU/memory sampling period; 0 turns it off (default 2)

logs:                       # optional: write each process's output to disk
  enabled: true             #   default false
  dir: .muster/logs         #   default .muster/logs, relative to this file

processes:                  # required, at least one entry; order is the sidebar order
  <name>:
    command: <shell command>     # required
    cwd: <directory>             # optional, relative to the config file; default: its directory
    env:                         # optional, added on top of muster's own environment
      KEY: value
    grace_period_seconds: 10     # optional: overrides the global grace period
    restart: never               # optional: never (default) | on-failure | always
    max_restarts: 5              # optional: consecutive automatic restarts (default 5)
    ports: [3000, 8080]          # optional: TCP ports it listens on, checked before startup
```

Every problem in the file is reported with its line number, for example
`line 7: port must be between 1 and 65535, got 70000`. Unknown keys, duplicate
process names, a port declared twice (in one process or two), a
`stats_interval_seconds` that is neither `0` nor between 0.5 and 3600, a
non-boolean `logs.enabled` and so on are all rejected before anything starts.

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

### Resource usage and uptime

CPU and memory are sampled every `stats_interval_seconds` (default 2; `0`
disables sampling; the minimum is 0.5 because each sample reads the system's
process table).

**The figures cover the whole process tree, not just the top-level process.**
A dev server such as `npm run dev` does its work in children, so measuring
only the process muster launched (usually a shell) would report almost
nothing. Each sample takes the launched process plus every descendant:

- *CPU* is the sum over the tree, where 100% is one fully busy core, so a
  multi-threaded tree can read above 100%, as in `top`. It is the average
  since the previous sample, so the first reading after a (re)start is 0.0%.
- *Memory* is the sum of resident set size. Memory shared between processes
  in the tree (for example by forked workers) is counted once per process, so
  the total can overstate real use.
- *Uptime* counts from the most recent start, and goes back to zero on a
  restart, whether you pressed `r` or a restart policy did it.

How the tree is found: by following parent pids from the launched process.
That is portable (Linux, macOS and Windows all expose it) but means a
grandchild whose parent has already exited, and so was re-parented to init, is
no longer counted, even though it is still in the process group or job object
that muster stops. Doing it by process group or job membership would catch
those, but the library used for sampling does not expose either on every
platform. On Windows, walking the process table can be slower than
on Linux, which is one more reason not to sample faster than you need.

### Port conflicts

If a process listens on a port, declare it:

```yaml
processes:
  frontend:
    command: npm run dev
    ports: [3000]
  backend:
    command: go run ./cmd/api
    ports: [8080, 8081]
  worker:
    command: go run ./cmd/worker   # not a listener, no ports needed
```

Before spawning anything, muster tries to bind every declared port. If one is
taken, it stops and says so (naming the owner when the OS lets it find out):

```
muster: some declared ports are already in use. Nothing has been started yet.

  frontend: port 3000 is already in use by node (pid 4242)
  backend: port 8081 is already in use

Those processes will probably fail to bind. Start anyway? [y/N]
```

In a terminal you are asked whether to continue (the default is no). When
input is not a terminal, such as in a script, muster refuses to start. Either
way `muster -ignore-port-conflicts` prints the same warning and starts
anyway.

Notes: only TCP is checked. The test is "can I bind this port on all
interfaces right now", so it also notices a listener bound to just
`127.0.0.1`. A privileged port (below 1024) that muster is not allowed to
test is not reported as a conflict. The check happens once, at startup, not
when you restart a process. Declaring the same port on two processes is a
config error.

### Log files

With `logs.enabled: true` every process's output is also appended to
`<dir>/<process-name>.log` (default `.muster/logs/`, relative to the config
file; the directory is created if needed). The in-memory buffer only keeps the
last `buffer_lines` lines, and is gone when muster exits, so the file is what
you read after a crash or after output has scrolled out of the buffer.

- Output is written exactly as the process produced it (stdout and stderr
  merged), including very long lines the log pane would split.
- muster adds its own timestamped entries for starts, exits, restarts and
  shutdown, such as `[muster 2026-10-20T10:15:02Z] exited with code 1;
  restart 1/5 in 1s`, so you can see where each run begins and ends.
- Files are appended to across runs and are never rotated or truncated by
  muster. Delete them when you want a clean slate. `.muster/` is a sensible
  entry for your `.gitignore`.
- Process names are made safe for file names by percent-encoding anything
  other than letters, digits, `-`, `_` and `.` (so `api/v2` is written to
  `api%2Fv2.log`).
- If a log file cannot be opened or written, the process keeps running; the
  problem is shown once in that process's log pane and saving stops.

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
- Resource figures follow parent pids, so a grandchild whose parent has
  exited is not counted (see "Resource usage and uptime").
- Port checks are TCP only and happen once, at startup, not when a process
  is restarted ([#5](https://github.com/Prabodh-dev/muster/issues/5)).
- Log files are not rotated ([#4](https://github.com/Prabodh-dev/muster/issues/4))
  and are created with default permissions
  ([#6](https://github.com/Prabodh-dev/muster/issues/6)).
- Restart backoff timings (1s doubling to 30s, 10s to count as healthy) are
  fixed rather than configurable.

## Contributing

Contributions are welcome, from typo fixes to new features. See
[CONTRIBUTING.md](CONTRIBUTING.md) for how to build, test and lint, the
conventions the project follows, and good places to start. Please also read the
[Code of Conduct](CODE_OF_CONDUCT.md). To report a security problem, follow
[SECURITY.md](SECURITY.md).

## License

MIT, see [LICENSE](LICENSE).

# Contributing to muster

Thanks for taking a look. muster is a small, focused tool, and contributions of
every size are welcome: a typo fix, a bug report with a reproduction, a test
for something that is not covered, or a whole feature.

By taking part you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).
To report a security problem, see [SECURITY.md](SECURITY.md) and **do not**
open a public issue.

## Building and testing

You need Go 1.24 or newer. Nothing else is required.

```sh
git clone https://github.com/Prabodh-dev/muster
cd muster

go build -o muster ./cmd/muster   # build
go test -race ./...               # the full test suite
go vet ./...
```

Try your build against the example config in the repository root (it runs a
tiny demo program through `go run`, so it works on every platform):

```sh
./muster
```

### Lint

CI enforces `gofmt` and [golangci-lint](https://golangci-lint.run/) (the
config is in `.golangci.yml`). Run them before you push:

```sh
gofmt -l .            # should print nothing
golangci-lint run     # v2.x
```

muster has Windows- and macOS-specific code, and `golangci-lint run` only
compiles the files for your own OS. CI lints the other two as well; you can
too:

```sh
GOOS=windows golangci-lint run
GOOS=darwin  golangci-lint run
```

CI also runs [`govulncheck`](https://go.dev/doc/security/vuln/) and checks that
`go.mod` is tidy (`go mod tidy -diff`). If you add or update a dependency, keep
it deliberate: pin an exact version, run `go mod tidy`, and mention why in the
PR. The module targets the Go version in `go.mod`; please don't raise it unless
you need to.

### Testing notes

- Tests that need a child process do not use `sleep`, `echo` or other shell
  tools. `internal/proc/helper_test.go` re-runs the test binary itself as a
  stand-in dev server (one that spawns children, ignores signals, exits with a
  chosen code, and so on). That keeps the tests identical on Linux, macOS and
  Windows. If you need a new kind of fake process, add a mode there.
- Process-lifecycle tests wait for events rather than sleeping a fixed time.
  When you have to assert on timing (backoff, grace periods), assert a lower
  bound, which timers guarantee, and keep upper bounds generous so a busy CI
  machine does not make the test flaky.
- The test suite runs on all three operating systems in CI. If something can
  only be tested on one, skip the others with a clear reason, as the existing
  tests do, and say so in your PR.

## Project layout

```
cmd/muster/       the command-line entry point (flags, port-check prompt)
internal/config/  muster.yaml parsing and line-numbered validation
internal/proc/    spawning, process groups/job objects, restart policy, log files
internal/stats/   CPU and memory sampling for a process tree
internal/portcheck/ detecting ports that are already in use
internal/ringbuf/ the bounded in-memory log buffer
internal/tui/     the Bubble Tea interface (model, view, search)
examples/demo/    the small program the example config runs
```

The central idea is in `internal/proc`: every child's output is read by its
own goroutine and fanned in to a single channel, which the Bubble Tea
`Update` loop consumes. No process can block another or the UI. Keep that
property when changing anything there.

## Branches and commits

- Branch from `main`, and name the branch for what it does, with a prefix:
  `feature/restart-backoff-config`, `fix/windows-job-handle`,
  `docs/readme-install`.
- Make small commits that each do one thing and leave the tests passing, in an
  order that tells the story of the change (for example: config parsing, then
  the behaviour, then tests, then docs).
- Write commit subjects as a short imperative sentence in sentence case, with
  no trailing period: `Add log search with highlighting`, `Fix restart count
  after a failed start`. Use the body to explain *why* when it is not obvious.
- Don't force-push over commits that someone has already reviewed; add new
  commits instead. We squash or merge as appropriate when the PR lands.

## What a good pull request looks like

- **One purpose.** A bug fix, a feature, or a refactor, not all three. Smaller
  PRs get reviewed faster.
- **A description that says what and why.** What problem does this solve, and
  what did you consider and reject? Screenshots or a short recording help for
  anything visible in the UI.
- **Tests.** New behaviour needs a test, and a bug fix needs a test that fails
  without the fix. If something genuinely can't be tested, say why.
- **Docs.** If a user can see the change, update the README (and
  `CONTRIBUTING.md` or `SECURITY.md` if they are affected).
- **Green CI.** That means tests on Linux, macOS and Windows, lint, and
  `govulncheck`.
- **Honest scope.** If you could only test on one platform, or you know about
  a rough edge, say so. That is far more useful to a reviewer than a polished
  description that hides it.

For a large change, open an issue first so we can agree on the approach before
you spend time on it. For a small one, just send the PR.

## Reporting bugs

Use the bug report template. The most useful reports include the output of
`muster -version`, your operating system and terminal, the `muster.yaml` (with
anything sensitive removed), and what you expected versus what happened. If the
config has `logs.enabled: true`, the relevant log file is often the fastest way
to see what went wrong.

## Where to start

Issues labelled **good first issue** are chosen to be self-contained. Two known
gaps are written up in detail, with pointers to the code and the tests to write:

- [#4 Rotate persisted log files](https://github.com/Prabodh-dev/muster/issues/4)
  log files grow without bound today.
- [#5 Re-check declared ports when a process is restarted](https://github.com/Prabodh-dev/muster/issues/5)
  port conflicts are only detected at startup.
- [#6 Create log files and directories with private permissions](https://github.com/Prabodh-dev/muster/issues/6)
  a small, well-scoped hardening change.

Other ideas that are not issues yet (open one if you want to take them):

- A **filter mode** for the log pane that hides lines that do not match the
  search text (search and highlighting exist; filtering does not).
- **Configurable restart backoff** (the 1s doubling to 30s schedule is fixed
  today).
- **Terminal escape-sequence filtering** for child output.
- A real **Windows run-through**: the Windows code path (job objects,
  `CTRL_BREAK`) is covered by CI but has had less hands-on use than Linux, so
  careful bug reports there are very valuable.

Not sure where something fits? Open an issue and ask.

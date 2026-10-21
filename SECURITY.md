# Security

## The one thing to understand before you run muster

**muster executes the shell commands written in its config file. That is its
entire purpose, and it means a `muster.yaml` is a program.**

Running `muster` in a directory runs whatever `muster.yaml` is in that
directory, with your user's permissions, your environment variables and your
network access. It does not sandbox, restrict or inspect those commands in any
way, and it never asks for confirmation before starting them.

> **Never run a `muster.yaml` you have not read, from a repository you do not
> trust.**

This is the same trust model as:

- a `Makefile` (`make` runs its recipes),
- the install scripts in a `package.json` (`npm install` runs them),
- `docker compose up` on someone else's compose file,
- a shell script somebody hands you.

If you would not run `make` or `npm install` in a freshly cloned repository
before looking at it, do not run `muster` there either. Read the `command:`
lines first. A command can be anything: `curl ... | sh`, `rm -rf`, a program
that reads your SSH keys. muster adds no protection against any of it, and
cannot.

Because the config file is looked up in the current directory by default, be
particularly careful about running a bare `muster` after `cd`-ing into a
directory you did not create. Use `muster -f path/to/known-good.yaml` when in
doubt.

## What is and is not a vulnerability

Running commands from the config file is **intended behaviour, not a
vulnerability**. Reports along the lines of "a malicious `muster.yaml` can run
arbitrary commands" will be closed with a pointer to this document.

These **are** in scope, and welcome:

- muster running something the config did not ask for, or running a command as
  a different user or with a different environment than documented.
- Escaping the documented behaviour of `cwd`, `env` or the shell invocation in
  a way that lets data (rather than the config author) choose what runs, for
  example through process names, output from a child process, or log content.
- Child process output (including terminal escape sequences) that can make the
  terminal UI execute commands, write files, or otherwise do more than draw
  text.
- Log files written outside the configured directory, or through a symlink the
  user did not intend to follow.
- Processes left running after muster exits in a way that is not documented
  (a leak that keeps a port or resource held).
- Memory-safety or crash bugs reachable with an unremarkable config or
  unremarkable child output.
- A vulnerable dependency that muster actually uses in an exploitable way.

## Things worth knowing about how muster handles your data

These are current behaviours, listed so you can make informed choices.

- **Environment.** Every child process inherits muster's full environment,
  plus whatever `env:` adds. Secrets in your shell environment are visible to
  every process in the config.
- **Log files.** With `logs.enabled: true`, everything a process prints is
  written to disk in plain text, including any secrets it prints. Files are
  created with mode `0644` in a directory created with mode `0755`, so on a
  shared machine other users may be able to read them. Keep the log directory
  somewhere private, add it to `.gitignore` (the default `.muster/` is
  already ignored by this repository's own `.gitignore`), and do not commit
  it. Logs are off by default.
- **Port checks.** muster tries to bind, and to connect to, the ports you
  declare under `ports:` on this machine's own addresses before starting
  anything. It makes no connections to other hosts.
- **Network.** muster itself opens no listening sockets, makes no outbound
  connections of its own, sends no telemetry and does not update itself.
- **Terminal output.** Child output is shown in your terminal. muster does not
  currently filter terminal escape sequences from it.

## Reporting a vulnerability

Please report suspected vulnerabilities **privately**, not in a public issue.

1. Preferred: use GitHub's private vulnerability reporting. On the repository
   page choose **Security → Report a vulnerability**.
2. Otherwise, email **prabodhrajmow@gmail.com** with "muster security" in the
   subject.

Please include the muster version (`muster -version`), your operating system,
a minimal config or steps to reproduce, and what you expected to happen
instead.

This is a small, volunteer-run project, so there is no formal SLA, but you can
expect an acknowledgement within a few days and an honest assessment after
that. A fix will be released as a new version, and if you would like to be
credited in the release notes, say so in your report.

## Supported versions

Only the latest release receives security fixes.

## Verifying a download

Each release includes a `muster_<version>_checksums.txt` file with the SHA-256
of every archive. After downloading, check the archive against it:

```sh
sha256sum -c --ignore-missing muster_<version>_checksums.txt
```

Release binaries are built by the workflow in `.github/workflows/release.yml`
from the tagged commit. They are not code-signed or notarised, so macOS may
ask you to approve the first run, and Windows SmartScreen may warn about an
unrecognised publisher.

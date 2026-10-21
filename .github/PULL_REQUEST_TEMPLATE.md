## What and why

<!-- What does this change, and what problem does it solve? Link the issue if there is one: "Fixes #123". -->

## How it was tested

<!-- Tests you added or ran. For anything visible in the UI, a screenshot or short recording helps. -->

## Platforms

<!-- Which operating systems did you test on yourself? CI covers Linux, macOS and Windows, but say if something is only verified on one. -->

## Checklist

- [ ] `go test -race ./...` passes
- [ ] `gofmt -l .` prints nothing and `golangci-lint run` is clean
- [ ] New behaviour has a test (a bug fix has a test that fails without the fix)
- [ ] README or other docs updated if users can see the change
- [ ] Commits are small and each one explains itself (see [CONTRIBUTING.md](../CONTRIBUTING.md))

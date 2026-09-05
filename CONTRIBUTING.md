# Contributing to Local-WEB

Thank you for your interest in contributing! This document outlines the workflow.

## Prerequisites

- Go 1.26+
- Git
- `make` (or `go build` / `go test` directly)
- On Windows: MSYS2 + mingw64 for CGO (race detector, platform backends)

## Development Workflow (TDD)

This project follows **test-driven development**:

1. **Plan** — Add a todo to the issue tracker (or comment on an existing issue)
2. **Write tests first** — Create a failing test that captures the desired behavior
3. **Implement** — Write the minimum code to pass the test
4. **Refactor** — Clean up, keeping tests green
5. **Verify** — Run the full verification suite (see below)

## Verification Commands

```bash
# Build
go build ./...

# Lint
go vet ./...
gofmt -l .

# Unit tests
go test ./... -count=1
go test -race ./... -count=1  # requires CGO_ENABLED=1

# Coverage
go test -coverprofile=coverage.out ./...

# Integration tests
go test -tags=integration ./test/integration/... -v -count=1
```

## Commit Convention

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add X feature
fix: correct Y behavior
refactor: simplify Z without changing behavior
test: add coverage for W
docs: update README status table
chore: bump dependencies
```

Scope prefixes are encouraged (e.g., `feat(link): implement BLE adapter`).

## Pull Request Checklist

- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] `gofmt` is clean
- [ ] Unit tests pass (`go test ./... -count=1`)
- [ ] Race-detector tests pass (`go test -race ./...`)
- [ ] Integration tests pass (`go test -tags=integration ./test/integration/...`)
- [ ] New code has meaningful test coverage
- [ ] Private keys are never logged or printed to stdout
- [ ] New dependencies are listed in `go.mod`
- [ ] README status table is updated if layer status changed

## Code Review

- All PRs require at least one review
- Security-sensitive changes (crypto, transport, store encryption) require a
  security review from the maintainer
- Self-review by the author is strongly encouraged before requesting review

## Platform Support

Local-WEB supports Linux, macOS, and Windows. Platform-specific code should be
gated using `runtime.GOOS` checks, not `uname`/`ver` shell commands.

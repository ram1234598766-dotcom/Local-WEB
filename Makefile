.PHONY: all build build-node build-cli clean run-node run-cli quickstart \
        test test-unit test-integration test-chaos test-race test-cover cover-func \
        bench lint vet fmt fmt-check staticcheck govulncheck security \
        cross-compile generate tidy deps release-dry clean-test ci help \
        deps-wintun msi nsis

VERSION ?= 1.0.1
DISTDIR ?= dist

GO      := go
BINDIR  := bin
NODEBIN := $(BINDIR)/localweb-node
CLIBIN  := $(BINDIR)/localweb-cli
# Packages that are safe to exercise on a developer machine. Integration tests
# are excluded from the default run because they bind sockets and spawn nodes.
TESTPKGS := $(shell $(GO) list ./... | grep -v '/test/integration')

all: build

$(BINDIR):
	mkdir -p $(BINDIR)

## ---------------------------------------------------------------- build ----

build: build-node build-cli

build-node: $(NODEBIN)

$(NODEBIN): $(BINDIR)
	$(GO) build -trimpath -o $(NODEBIN) ./cmd/node

build-cli: $(CLIBIN)

$(CLIBIN): $(BINDIR)
	$(GO) build -trimpath -o $(CLIBIN) ./cmd/cli

## ----------------------------------------------------------------- test ----

# Unit tests only. Fast enough to run on every save.
test: test-unit

test-unit:
	$(GO) test -count=1 $(TESTPKGS)

# Race detector over the unit suite.
test-race:
	$(GO) test -race -count=1 $(TESTPKGS)

# Integration tests are build-tagged so they never run implicitly.
test-integration:
	$(GO) test -tags=integration -count=1 -timeout=10m ./test/integration/...

test-chaos:
	$(GO) test -race -count=1 ./pkg/chaos/...

# Coverage over the unit suite only; integration coverage is measured
# separately by test-integration so the two numbers are not conflated.
test-cover:
	$(GO) test -count=1 -coverprofile=coverage.out $(TESTPKGS)
	$(GO) tool cover -func=coverage.out | tail -n 1

cover-func:
	$(GO) tool cover -func=coverage.out

## ----------------------------------------------------------------- lint ----

lint: fmt-check vet staticcheck

vet:
	$(GO) vet ./...

# staticcheck is not vendored; it is skipped with a clear message rather than
# failing the build when the tool is absent.
staticcheck:
	@command -v staticcheck >/dev/null 2>&1 \
		&& staticcheck ./... \
		|| echo "staticcheck not installed (go install honnef.co/go/tools/cmd/staticcheck@latest); skipped"

govulncheck:
	@command -v govulncheck >/dev/null 2>&1 \
		&& govulncheck ./... \
		|| echo "govulncheck not installed (go install golang.org/x/vuln/cmd/govulncheck@latest); skipped"

security: govulncheck

fmt:
	$(GO) fmt ./...

# Fails when anything is unformatted, unlike `go fmt` which rewrites silently.
fmt-check:
	@out=$$(gofmt -s -l .); \
	if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi

## ------------------------------------------------------------------ run ----

run-node:
	$(GO) run ./cmd/node

run-cli:
	$(GO) run ./cmd/cli

quickstart:
	bash scripts/quickstart.sh

## -------------------------------------------------------------- release ----

# Five targets, matching .goreleaser.yml. riscv64, FreeBSD and OpenBSD are
# documented as unsupported because nothing builds or tests them.
cross-compile: $(BINDIR)
	GOOS=linux   GOARCH=amd64 $(GO) build -trimpath -o $(BINDIR)/localweb-linux-amd64      ./cmd/node
	GOOS=linux   GOARCH=amd64 $(GO) build -trimpath -o $(BINDIR)/localweb-cli-linux-amd64   ./cmd/cli
	GOOS=linux   GOARCH=arm64 $(GO) build -trimpath -o $(BINDIR)/localweb-linux-arm64      ./cmd/node
	GOOS=linux   GOARCH=arm64 $(GO) build -trimpath -o $(BINDIR)/localweb-cli-linux-arm64   ./cmd/cli
	GOOS=darwin  GOARCH=amd64 $(GO) build -trimpath -o $(BINDIR)/localweb-darwin-amd64     ./cmd/node
	GOOS=darwin  GOARCH=amd64 $(GO) build -trimpath -o $(BINDIR)/localweb-cli-darwin-amd64  ./cmd/cli
	GOOS=darwin  GOARCH=arm64 $(GO) build -trimpath -o $(BINDIR)/localweb-darwin-arm64     ./cmd/node
	GOOS=darwin  GOARCH=arm64 $(GO) build -trimpath -o $(BINDIR)/localweb-cli-darwin-arm64  ./cmd/cli
	GOOS=windows GOARCH=amd64 $(GO) build -trimpath -o $(BINDIR)/localweb-windows-amd64.exe ./cmd/node
	GOOS=windows GOARCH=amd64 $(GO) build -trimpath -o $(BINDIR)/localweb-cli-windows-amd64.exe ./cmd/cli

# Fetch and SHA-256 verify the Wintun kernel driver that the Windows VPN service
# loads. wintun.dll is gitignored (*.dll), so without this a fresh clone cannot
# build either installer. No-op when the driver is already present and correct.
deps-wintun:
	@bash scripts/fetch-wintun.sh

# Windows installer targets. Both fetch the Wintun driver first, stage the
# payload their script references, and run the real toolchain (WiX / NSIS).
#   make msi    -> $(DISTDIR)/localweb_$(VERSION)_x64_en-US.msi
#   make nsis   -> $(DISTDIR)/localweb-$(VERSION)-setup.exe
msi:
	@bash scripts/build-msi.sh $(DISTDIR)

nsis:
	@bash scripts/build-nsis.sh $(DISTDIR)

bench:
	$(GO) test -bench=. -benchmem -benchtime=1s -run=XXX ./pkg/crdt/ ./pkg/dht/ ./pkg/crypto/ ./pkg/chaos/ ./pkg/store/ ./pkg/security/

## ----------------------------------------------------------------- deps ----

generate:
	$(GO) generate ./...

tidy:
	$(GO) mod tidy

deps:
	$(GO) mod download

# Dry run only. goreleaser v2 also produces the real release, so it is not
# invoked by any target here.
release-dry:
	@goreleaser release --snapshot --clean --skip=publish

## ------------------------------------------------------------------- ci ----

# The same gate CI runs, so a local failure is a real CI failure.
ci: fmt-check vet build test-race test-integration govulncheck

clean-test:
	rm -f coverage.out

clean:
	rm -rf $(BINDIR) coverage.out

help:
	@echo "LocalWEB make targets:"
	@echo ""
	@echo "  build              build node + cli binaries into bin/"
	@echo "  build-node         build the node daemon only"
	@echo "  build-cli          build the CLI client only"
	@echo "  test / test-unit   run unit tests (no race, no integration)"
	@echo "  test-race          run unit tests under the race detector"
	@echo "  test-integration   run build-tagged integration tests"
	@echo "  test-chaos         run chaos scenarios under the race detector"
	@echo "  test-cover         write coverage.out and print the total"
	@echo "  lint               fmt-check + vet + staticcheck"
	@echo "  security           govulncheck ./..."
	@echo "  cross-compile      build the 5 supported release targets"
	@echo "  bench              run benchmarks"
	@echo "  run-node           start the daemon"
	@echo "  run-cli            start the CLI"
	@echo "  quickstart         build, generate an identity, print the node ID"
	@echo "  ci                 the same gate CI runs"
	@echo "  clean              remove build output"

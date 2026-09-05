.PHONY: build test lint cross-compile generate clean run-node run-cli quickstart

GO := go
BINDIR := bin
NODEBIN := $(BINDIR)/localweb-node
CLIBIN := $(BINDIR)/localweb

build: $(NODEBIN) $(CLIBIN)

$(BINDIR):
	mkdir -p $(BINDIR)

$(NODEBIN): $(BINDIR)
	$(GO) build -o $(NODEBIN) ./cmd/node

$(CLIBIN): $(BINDIR)
	$(GO) build -o $(CLIBIN) ./cmd/cli

test:
	$(GO) test ./... -v -count=1
	$(GO) test -race ./...
	$(GO) test -coverprofile=coverage.out ./...

bench:
	$(GO) test -bench=. -benchmem ./...

lint:
	golangci-lint run
	$(GO) vet ./...
	$(GO) fmt ./...

bench:
	go test -bench=. -benchmem -benchtime=1s -run=. ./pkg/crdt/ ./pkg/dht/ ./pkg/crypto/
	go test -bench=. -benchmem -benchtime=1s -run=. ./pkg/chaos/

test-chaos:
	$(GO) test -race -v ./pkg/chaos/...

run-node:
	$(GO) run ./cmd/node

run-cli:
	$(GO) run ./cmd/cli

cross-compile:
	GOOS=linux   GOARCH=amd64   $(GO) build -o $(BINDIR)/localweb-linux-amd64   ./cmd/node
	GOOS=linux   GOARCH=arm64   $(GO) build -o $(BINDIR)/localweb-linux-arm64   ./cmd/node
	GOOS=darwin  GOARCH=arm64   $(GO) build -o $(BINDIR)/localweb-macos-arm64  ./cmd/node
	GOOS=darwin  GOARCH=amd64   $(GO) build -o $(BINDIR)/localweb-macos-amd64  ./cmd/node
	GOOS=windows GOARCH=amd64   $(GO) build -o $(BINDIR)/localweb-windows-amd64.exe ./cmd/node

generate:
	protoc --go_out=. --go_opt=paths=source_relative api/proto/messages.proto

clean:
	rm -rf $(BINDIR)/ coverage.out data/

deps:
	$(GO) mod tidy
	$(GO) mod download

quickstart:
	bash scripts/quickstart.sh

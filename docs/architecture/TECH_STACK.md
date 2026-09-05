# TECH STACK

This document describes the **actual** technology stack of the Local-WEB Go P2P networking stack as implemented in `pkg/` and `cmd/`. It supersedes the previous version which described a planned/aspirational stack.

## Module

- **Module path**: `github.com/ram1234598766-dotcom/Local-WEB`
- **Language**: Go 1.26.0
- **Build system**: GNU Make (`Makefile` with `build`, `test`, `bench`, `lint`, `run-node`, `run-cli`, `cross-compile`, `generate`, `clean`, `deps` targets)
- **Code generation**: `protoc` for `pkg/proto/api/proto/messages.proto`

## Core Dependencies

| Dependency | Version | Purpose |
|---|---|---|
| `github.com/quic-go/quic-go` | v0.62.0 | QUIC transport layer (Layer L5) |
| `github.com/dgraph-io/badger/v3` | v3.2103.5 | Embedded key-value store (store layer) |
| `github.com/ipfs/go-cid` | v0.4.0 | Content-addressed identifiers (CID v1) |
| `github.com/klauspost/compress` | v1.18.0 | zstd compression (files service) |
| `github.com/rs/zerolog` | v1.33.0 | Structured JSON logging |
| `github.com/spf13/cobra` | v1.8.1 | CLI framework (`cmd/cli/`) |
| `github.com/stretchr/testify` | v1.12.1 | Test assertions and mocking |
| `golang.org/x/crypto` | (latest) | Ed25519, X25519, SHA3-256 (crypto primitives) |
| `go.yaml.in/yaml/v3` | (latest) | YAML parsing (registry service manifests) |
| `google.golang.org/protobuf` | (latest) | Protobuf marshaling (proto layer) |

## Security & Cryptography (`pkg/security/`, `pkg/crypto/`)

- **Identity**: Ed25519 keypair generation, signing, and verification
- **Key exchange**: X25519 for ephemeral key exchange
- **Hashing**: SHA3-256 for content addressing and audit trails
- **Transport encryption**: Noise XX handshake (implemented in `pkg/crypto/noise.go`)
- **Store encryption**: AES-GCM authenticated encryption at rest (BadgerDB + `pkg/store/store.go`)
- **Capability tokens**: Ed25519-signed tokens with canonical JSON serialization (`pkg/security/capability.go`)
- **Proof of Work**: SHA3-based PoW challenge/verify (`pkg/security/pow.go`)
- **Audit log**: Append-only hash chain with SHA3-256 (`pkg/security/audit.go`)

## Networking (`pkg/transport/`, `pkg/discovery/`)

- **Transport protocol**: QUIC (RFC 9001) via `quic-go`
- **Link types supported**: 6 physical/adaptation layers in `pkg/link/`:
  - WiFi Station, WiFi Direct, Ad-hoc, USB Tether, BLE, Acoustic
- **Discovery modes**: WiFi, BLE, mDNS (multicast DNS) — merged by `Orchestrator` in `pkg/discovery/discovery.go`
- **NAT traversal**: UDP hole punching with relay fallback
- **Circuit relay**: QUIC-based circuit relay for NAT traversal
- **Stream multiplexing**: 1-byte ServiceID-based routing over QUIC streams

## Data Layer (`pkg/store/`, `pkg/dht/`)

- **Primary store**: BadgerDB (embedded, LSM-tree based)
- **Block store**: Content-addressed block storage with CID support (`pkg/store/block_store.go`)
- **Peer store**: Peer metadata, pubkeys, and connection state (`pkg/store/peer_store.go`)
- **DHT**: Kademlia-style distributed hash table:
  - KBucket size: 20
  - α (concurrency): 3
  - XOR-based routing (SHA3-256 of public key)
  - Operations: FindNode, Store, Lookup, RegisterNode, Ping
- **CRDT**: Two conflict-free replicated data types:
  - ORSet (add-wins set with tombstones)
  - RGA (Replicated Growable Array for collaborative text editing)

## Services Layer (`pkg/services/`)

Nine implemented P2P services, each with its own subpackage:

| Service | Port/Protocol | Key Features |
|---|---|---|
| DNS | UDP 5353 (mDNS) | `.localweb` TLD, signed zone records |
| HTTP | TCP 8080 | Per-site mux, `/health` endpoint, logging middleware |
| Email | SMTP + IMAP | Maildir storage, PoW antispam |
| Docs | Custom over messaging | RGA-backed collaborative text, presence, cursors, selections |
| Files | Bitswap-like protocol | BlockStore + FileStore, zstd compression, Merkle DAG sync |
| Messaging | Custom pub/sub | Signed messages, offline queue |
| Registry | HTTP + DHT | LWPKG packages (tar.gz + sig), YAML manifest validation |
| Voice | Signaling + media | Call state machine, ICE candidates, Opus/VP9 codec profiles |
| VPN | TUN interface | Tunnel creation via SHA3-256, route distribution |

## Testing & Quality

- **Integration tests**: `test/integration/` — covers DHT, discovery, DNS, messaging, transport, full-stack
- **Test framework**: `github.com/stretchr/testify`
- **Linting**: `golangci-lint`, `go vet`, `gofmt`
- **Race detection**: `go test -race`
- **Coverage**: `go test -coverprofile`

## Runtime & Deployment

- **Target platforms**: Linux, macOS, Windows (cross-compile via Makefile)
- **Entry points**: `cmd/node` (full node daemon), `cmd/cli` (CLI client)
- **No external dependencies required**: fully self-contained Go binary
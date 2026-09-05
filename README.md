# Local-WEB

**Real working P2P internet stack. Zero infrastructure required. Better than centralized.**

Local-WEB is a production-grade peer-to-peer networking stack written in Go that enables device-to-device communication without any centralized infrastructure. It combines multiple transport modes, discovery mechanisms, and link layers into a unified platform.

## Core Principle

```
Mode 1: NO WiFi (zero infrastructure)
  → BLE advertising + scanning
  → WiFi Direct (peer-to-peer)
  → Ad-hoc WiFi network
  → USB tethering
  → Acoustic coupling (audio FSK)

Mode 2: WITH WiFi (standard networking)
  → mDNS-SD (subnet discovery)
  → ARP scan (subnet sweep)
  → Internet relay (cross-subnet)

Mode 3: HYBRID (best of both)
  → BLE discovers peer → exchanges WiFi Direct credentials → high-bandwidth transfer
  → mDNS fails → BLE fallback → escalate to direct connection
```

## Architecture (9-Layer)

```
┌─────────────────────────────────────────────────────────────────────┐
│  L9: APPLICATION                                                     │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ Node     │ CLI      │          │          │                   │  │
│  │ Daemon   │ Client   │          │          │                   │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  L8: SERVICES                                                        │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ DNS      │ HTTP     │ Email    │ Docs     │ Files / Messaging /│  │
│  │ (.localweb)│ Gateway │ (SMTP/  │ (CRDT   │ Registry / Voice /│  │
│  │          │          │ IMAP)   │ text)   │ VPN                │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  L7: SYNC ENGINE                                                     │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ OR-Set   │ RGA      │ Merkle   │ Merkle   │ Encrypted         │  │
│  │ (CRDT)   │ (CRDT)   │ DAG      │ Sync     │ Store             │  │
│  │          │ (text)   │ diff     │ diff     │ (BadgerDB)        │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  L6: STORE                                                           │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ Peer     │ Block    │ Encrypted│ Key-Value│ Content-Addressed │  │
│  │ Store    │ Store    │ Blocks   │ (Badger) │ Storage           │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  L5: SECURITY                                                        │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ Key Mgmt │ Noise    │ Capability│ Spam    │ Audit             │  │
│  │ (Ed25519)│ XX       │ Access   │ (PoW)   │ Log               │  │
│  │          │ handshake│ Control   │         │ (hash chain)      │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  L4: ROUTING (DHT)                                                   │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ Kademlia │ KBucket  │ XOR      │ Iterative│ PoW               │  │
│  │ DHT      │ (k=20)   │ Routing  │ Lookup   │ (Sybil resist)   │  │
│  │          │          │          │          │                  │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  L3: DISCOVERY                                                       │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ mDNS-SD  │ BLE      │ WiFi     │ WiFi     │ Orchestrator      │  │
│  │ (WiFi)   │ (no WiFi)│ Direct   │ Ad-hoc   │ (merge + score)   │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  L2: LINK                                                            │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ WiFi     │ WiFi     │ BLE      │ USB      │ Acoustic          │  │
│  │ Station  │ Direct   │ (GATT)   │ Tether   │ (FSK modem)       │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  L1: TRANSPORT                                                       │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ QUIC     │ Stream   │ Circuit  │ NAT      │ Flow Control      │  │
│  │ (v0.62)  │ Mux      │ Relay    │ Traversal│ + Backpressure    │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
```

## Key Features

- **Noise XX Protocol**: X25519 + SHA3-256 handshake for identity-authenticated key exchange
- **QUIC Transport**: `quic-go` v0.62.0 with stream multiplexing (1-byte ServiceID routing), circuit relay, UDP hole-punching NAT traversal
- **Multi-Modal Discovery**: mDNS, BLE, WiFi Direct, ad-hoc — merged and scored by an orchestrator
- **Adaptive Link Layer**: 6 link types with automatic failover and BLE→WiFi Direct escalation
- **Kademlia DHT**: 256-bit XOR routing, KBucket k=20, α=3, PoW anti-Sybil
- **CRDT Sync**: OR-Set (add-wins) + RGA (collaborative text) with Merkle DAG diff sync
- **BadgerDB Store**: Encrypted at rest (AES-GCM via Badger's `WithEncryptionKey`), content-addressed blocks, peer metadata
- **9 P2P Services**: DNS (.localweb), HTTP, Email (SMTP/IMAP), Messaging, Files, Collaborative Docs, App Registry, Voice, Mesh VPN
- **Capability Tokens**: Ed25519-signed, canonical JSON for fine-grained access control
- **Proof of Work**: SHA3-based challenge/verify for spam and Sybil resistance
- **Audit Trail**: Append-only SHA3-256 hash chain for tamper-evident logging

## Project Structure

```
Local-WEB/
├── cmd/
│   ├── node/              # Node daemon entry point (full component wiring)
│   └── cli/               # CLI client (cobra: node, id, peers)
├── pkg/
│   ├── crypto/            # Noise XX handshake, Ed25519/X25519 keys, SHA3-256
│   ├── transport/         # QUIC server, stream mux, circuit relay, NAT traversal
│   ├── link/              # Adaptive link layer (WiFi, WiFi Direct, BLE, USB, ad-hoc, acoustic)
│   ├── discovery/         # mDNS, BLE discovery, orchestrator, peer database
│   ├── dht/               # Kademlia DHT (XOR routing, FindNode/Store/Lookup/PoW)
│   ├── security/          # Capability tokens, PoW, append-only audit log
│   ├── store/             # BadgerDB with AES-GCM encryption, peer store, block store
│   ├── crdt/              # ORSet + RGA with serialization
│   ├── proto/             # Protobuf definitions + Go conversions
│   └── services/
│       ├── dns/           # .localweb TLD, UDP 5353, signed records
│       ├── http/          # HTTP gateway, per-site mux, health checks
│       ├── email/         # SMTP + IMAP server, maildir, PoW antispam
│       ├── docs/          # RGA collaborative text, presence, cursors
│       ├── files/         # BlockStore + FileStore, zstd, Merkle DAG sync
│       ├── messaging/     # Pub/sub, signed messages, offline queue
│       ├── registry/      # LWPKG format, YAML manifests, DHT distribution
│       ├── voice/         # Call state machine, ICE, Opus/VP9 codec
│       └── vpn/           # TUN interface, SHA3-256 tunnels
├── test/integration/      # Integration tests (DHT, discovery, DNS, messaging, full-stack)
├── docs/architecture/     # Architecture v3, roadmap, tech stack
├── Makefile               # build, test, bench, lint, cross-compile, generate
├── go.mod
└── README.md
```

## Getting Started

### Prerequisites

- Go 1.26+
- Git
- `protoc` (only needed for proto code generation)
- `make` (for Makefile targets)

### Clone

```bash
git clone https://github.com/ram1234598766-dotcom/Local-WEB.git
cd Local-WEB
```

### Build

```bash
# Build all binaries
make build

# Or build directly
go build ./...
go build -o bin/node ./cmd/node
go build -o bin/cli ./cmd/cli
```

### Test

```bash
# Run all tests with race detector and coverage
make test

# Run integration tests only
go test ./test/integration/... -v

# Run benchmarks
make bench
```

### Run

```bash
# Start node daemon (listens on UDP 4443 for QUIC)
make run-node
# or: go run ./cmd/node

# Use CLI client
make run-cli
# or: go run ./cmd/cli

# CLI commands:
#   cli id          — generate/display node identity
#   cli peers       — list discovered peers
#   cli node        — start the node daemon
```

## Services

All 9 services are fully implemented and integrated. Each runs as a handler on a QUIC stream (identified by 1-byte ServiceID):

| Service | Package | Protocol | Port |
|---|---|---|---|
| DNS | `pkg/services/dns/` | UDP mDNS (5353) | 5353 |
| HTTP | `pkg/services/http/` | HTTP/3 over QUIC | 8080 |
| Email | `pkg/services/email/` | SMTP + IMAP | 587/993 |
| Messaging | `pkg/services/messaging/` | Custom pub/sub | 9090 |
| Files | `pkg/services/files/` | Bitswap-like | — |
| Docs | `pkg/services/docs/` | CRDT over messaging | 9091 |
| Registry | `pkg/services/registry/` | HTTP API + DHT | 9092 |
| Voice | `pkg/services/voice/` | Signaling + media | 9093 |
| VPN | `pkg/services/vpn/` | TUN interface | 9094 |

## Author

Mrityunjay K — architect and core contributor of Local-WEB.

## Usage

### Starting a Node Daemon

```bash
# Build the node binary
go build -o bin/node ./cmd/node

# Start the node daemon (listens on UDP 4443 for QUIC transport)
./bin/node --name "my-laptop" --listen :4443

# Available flags:
#   --name       Human-readable node name
#   --listen     Address to bind QUIC transport (default :4443)
#   --storage    Path to BadgerDB storage directory (default ./data)
#   --data-dir   Path to store node identity and keys (default ./keys)
```

### Using the CLI

```bash
# Build the CLI
go build -o bin/cli ./cmd/cli

# Generate or display your node identity
./bin/cli id

# List discovered peers on the local network
./bin/cli peers

# Start the node daemon from CLI
./bin cli node
```

### Running Services

Services start automatically when the node daemon runs. Each service listens on its own QUIC stream (identified by 1-byte ServiceID):

| Service | How to Use |
|---|---|
| **DNS** | `nslookup host.localweb 127.0.0.1 -port=5353` |
| **HTTP Gateway** | `curl http://localhost:8080/health` |
| **Email** | Configure SMTP client to `localhost:587` |
| **Messaging** | Use the CLI or SDK to publish/subscribe to channels |
| **Files** | `curl http://localhost:9090/files/` |
| **Docs** | Open `http://localhost:9091/docs/` in a browser |
| **Registry** | `GET http://localhost:9092/packages` |
| **Voice** | Use WebRTC-compatible client pointed at signaling port |
| **VPN** | Routes are automatically installed on TUN interface |

### DNS Resolution

Once your node is running, `.localweb` domains resolve automatically on your local network:

```bash
# Query a peer's .localweb address
nslookup peer1.localweb

# Test with dig
dig peer1.localweb @127.0.0.1 -p 5353
```

### Messaging

```go
// From Go code
import "github.com/mrityunjay/LocalWEB/pkg/services/messaging"

svc := messaging.NewService(store, privateKey)
chID := svc.CreateChannel([]*[32]byte{pubKey1, pubKey2})
msg, _ := svc.Publish(ctx, chID, myPubKey, []byte("hello"), "")
history, _ := svc.History(chID, "", 100)
```

### File Sharing

Share a file with a peer:
```bash
# On sender node
curl -T myfile.zip http://localhost:9090/files/myfile.zip

# On receiver node
curl http://localhost:9090/files/myfile.zip --output myfile.zip
```

### VPN

Start the VPN service (creates a TUN interface):
```bash
# TUN interface is created automatically when vpn service starts
ip addr show dev tun0  # Linux: tun0 created
ifconfig utun0         # macOS: utun0 created
```

## Development

### Code Review

All changes follow a disciplined workflow:
- TDD-first development
- Security review for crypto/transport layer
- Go vet and test gates (`make lint`)
- Subagent analysis for deep backend review

### Lint & Verify

```bash
make lint      # golangci-lint + go vet + gofmt
make test      # full test suite with race detection
```

### Commit Convention

- `feat:` new features
- `fix:` bug fixes
- `refactor:` code restructuring
- `test:` test additions
- `docs:` documentation
- `chore:` maintenance

## Status

| Layer | Component | Status | Tests |
|---|---|---|---|
| L1 | Transport (QUIC) | Verified | `quic_test.go`, `TestTCPServerAcceptAndRespond`, `TestTCPConcurrentRPCCalls` |
| L2 | Link (6 link types) | Verified (runtime detection) | `TestTwoNodeDiscoveryAllLinkTypes` |
| L3 | Discovery (mDNS/BLE/WiFi) | Verified | `discovery_test.go`, `TestTwoNodeDiscoveryAllLinkTypes` |
| L4 | DHT (Kademlia) | Verified | `dht_test.go`, `TestRPCRoundTrip`, `TestTCPMultipleSequentialRPCCalls` |
| L5 | Security (Noise XX, PoW, Audit) | Verified | `audit_test.go`, `capability_test.go`, `pow_test.go` |
| L6 | Store (BadgerDB + AES-GCM) | Verified | `store_test.go`, `block_store_test.go`, `peer_store_test.go` |
| L7 | Sync (CRDT + Merkle) | Verified | `crdt_test.go`, `TestComputeMerkleRoot` |
| L8 | Services (DNS, HTTP, Email, Messaging, Files, Docs, Registry, Voice, VPN) | Verified | Integration tests for DNS, Messaging, Full-Stack |
| L9 | App (node + CLI) | Verified (identity persistence, BadgerDB encryption) | — |

Phase 1 (security, crypto, critical transport, DNS, identity persistence) is complete and all Phase 1 integration tests pass. See `PHASE2_PLAN.md` for remaining Phase 2 work.

## Security

- **Transport encryption**: Noise XX handshake over QUIC (X25519 + SHA3-256)
- **Identity**: Ed25519 keypair, NodeID = SHA3-256(static public key)
- **Store encryption**: AES-256-GCM at rest via BadgerDB `WithEncryptionKey`
- **Access control**: Ed25519-signed capability tokens with canonical JSON
- **Spam resistance**: SHA3-based Proof of Work for email and DHT storage
- **Audit trail**: Append-only SHA3-256 hash chain log

## License

MIT. See [SECURITY.md](SECURITY.md) for vulnerability reporting.

## Links

- Documentation: `docs/architecture/TECH_STACK.md` (dependency details)
- Roadmap: `docs/architecture/ROADMAP.md` (implementation checklist)
- Architecture: `docs/architecture/ARCHITECTURE_V3.md`
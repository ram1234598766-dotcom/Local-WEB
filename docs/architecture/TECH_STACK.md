# LocalWEB — Technology Stack

**Version: 1.0.0 | Go 1.26+ | Module: `github.com/ram1234598766-dotcom/Local-WEB`**

---

## 📦 Module & Build System

| Aspect | Detail |
|--------|--------|
| **Module Path** | `github.com/ram1234598766-dotcom/Local-WEB` |
| **Language** | Go 1.26.0+ (tested on 1.26.x, 1.27.x) |
| **Build System** | GNU Make (`Makefile`) |
| **Code Generation** | `protoc` for Protocol Buffers |
| **Package Manager** | `go mod` (vendored via `go mod vendor`) |

### Makefile Targets

```bash
make build           # Build all packages
make test            # Run all tests with race detector
make test-unit       # Unit tests only
make test-integration # Integration tests
make bench           # Benchmarks
make lint            # golangci-lint + go vet + gofmt
make run-node        # Start node daemon (make quickstart)
make run-cli         # Build CLI
make cross-compile   # Cross-compile for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
make clean           # Clean build artifacts
make deps            # Download dependencies
make generate        # Code generation (protobuf)
```

---

## 📦 Core Dependencies

| Dependency | Version | Purpose | Layer |
|------------|---------|---------|-------|
| `github.com/quic-go/quic-go` | v0.62.0 | QUIC transport (RFC 9001) | L5 |
| `github.com/dgraph-io/badger/v3` | v3.2103.5 | Embedded LSM-tree KV store | Store |
| `github.com/ipfs/go-cid` | v0.4.0 | CIDv1 content addressing | Data |
| `github.com/klauspost/compress` | v1.18.0 | zstd compression | Files |
| `github.com/rs/zerolog` | v1.33.0 | Structured JSON logging | Observability |
| `github.com/spf13/cobra` | v1.8.1 | CLI framework | L9 |
| `github.com/stretchr/testify` | v1.12.1 | Test assertions/mocks | Test |
| `golang.org/x/crypto` | latest | Ed25519, X25519, SHA3-256, HKDF | Crypto |
| `go.yaml.in/yaml/v3` | latest | YAML parsing (manifests) | Registry |
| `google.golang.org/protobuf` | latest | Protobuf marshaling | Proto |

---

## 🔐 Security & Cryptography (`pkg/crypto/`, `pkg/security/`)

| Component | Algorithm | Implementation |
|-----------|-----------|----------------|
| **Identity** | Ed25519 | Keypair gen, sign, verify (`pkg/crypto/crypto.go`) |
| **Key Exchange** | X25519 | Ephemeral DH (`pkg/crypto/crypto.go`) |
| **Hashing** | SHA3-256 | Content addressing, audit chains |
| **Transport Encryption** | Noise XX | Handshake + XSalsa20Poly1305 (`pkg/crypto/noise.go`) |
| **Store Encryption** | AES-256-GCM | Authenticated encryption at rest (`pkg/store/store.go`) |
| **Capability Tokens** | Ed25519 + Canonical JSON | Signed, time-bounded, revocable (`pkg/security/capability.go`) |
| **Proof of Work** | SHA3-256 | Challenge/verify with difficulty adjustment (`pkg/security/pow.go`) |
| **Audit Log** | SHA3-256 Hash Chain | Append-only, tamper-evident (`pkg/security/audit.go`) |
| **Post-Quantum KEM** | Kyber-768 (hybrid) | Kyber + X25519 in Noise (`pkg/crypto/hybrid.go`, `pkg/transport/hybrid.go`) |

### Noise XX Protocol
```
Initiator                          Responder
    |                                  |
    |  ----> e (ephemeral)             |
    |                                  |
    |  <---- e, ee, s, es              |
    |                                  |
    |  ----> s, se                     |
    |                                  |
    |  (Handshake complete)            |
    |  Session key = HKDF(noise_key || kyber_ss)
    |                                  |
    |  <== Encrypted transport ==>     |
```

---

## 🌐 Networking (`pkg/transport/`, `pkg/discovery/`, `pkg/link/`)

### Transport Layer (L5)
| Feature | Specification |
|---------|---------------|
| Protocol | QUIC (RFC 9001) via `quic-go` v0.62 |
| TLS | Built-in QUIC TLS 1.3 |
| Multiplexing | 1-byte ServiceID per stream |
| Handshake | Noise XX over first QUIC stream |
| Identity Verification | NodeID = SHA3-256(Ed25519 PubKey) |

### Link Layer (L2) — 6 Physical Layers
| Link Type | Mode | Status | File |
|-----------|------|--------|------|
| WiFi Station | Station | ✅ | `wifi_station.go` |
| WiFi Direct | P2P | ✅ | `wifi_direct.go` |
| Ad-hoc WiFi | IBSS | ✅ | `adhoc.go` |
| USB Tether | RNDIS/ECM | ✅ | `usb.go` |
| BLE | Peripheral/Central | ✅ | `ble.go` |
| Acoustic FSK | Audio I/O | ✅ | `acoustic.go` |

### Discovery (`pkg/discovery/`)
| Mechanism | Protocol | Transport |
|-----------|----------|-----------|
| mDNS-SD | RFC 6762/6763 | UDP 5353 |
| BLE Discovery | GATT Service | BLE |
| Rendezvous (Federation) | HTTP/JSON | HTTPS |
| Orchestrator | Merges all sources | In-memory + score |

### NAT Traversal
| Method | Protocol | Fallback |
|--------|----------|----------|
| UDP Hole Punch | STUN-like | UDP |
| Circuit Relay | QUIC relay | TURN-like |
| UPnP/NAT-PMP | IGD/NAT-PMP | Router |

---

## 💾 Data Layer (`pkg/store/`, `pkg/dht/`, `pkg/crdt/`)

### Primary Store: BadgerDB
| Feature | Detail |
|---------|--------|
| Engine | LSM-tree (BadgerDB v3) |
| Encryption | AES-256-GCM (per-key) |
| Compression | Snappy (default) |
| TTL Support | Yes (via metadata) |
| Iterators | Prefix + reverse |

### Sub-stores
| Store | Purpose | Key Format |
|-------|---------|------------|
| BlockStore | Content-addressed blocks | `b/<CID>` |
| PeerStore | Peer metadata, keys, state | `p/<NodeID>` |
| FileStore | File metadata + Merkle DAG | `f/<FileCID>` |
| DocStore | CRDT documents | `d/<DocID>` |

### DHT (`pkg/dht/`)
| Parameter | Value |
|-----------|-------|
| Algorithm | Kademlia (XOR routing) |
| Key Space | SHA3-256 (256-bit) |
| k (bucket size) | 20 |
| α (concurrency) | 3 |
| Operations | FindNode, Store, Lookup, Register, Ping |

### CRDTs (`pkg/crdt/`)
| Type | Algorithm | Use Case |
|------|-----------|----------|
| ORSet | Add-wins with tombstones | Sets (peers, tags) |
| RGA | Replicated Growable Array | Collaborative text |
| LWW-Register | Last-Writer-Wins | Single values |
| Merkle DAG | Content-addressed | File sync, doc sync |

---

## ⚙️ Services Layer (`pkg/services/`) — 9 Services

| Service | Package | Protocol | Port | Key Features |
|---------|---------|----------|------|--------------|
| **DNS** | `dns` | mDNS/DoH | UDP 5353 | `.localweb` TLD, signed records |
| **HTTP** | `http` | HTTP/1.1 | TCP 8080 | Per-site mux, health, logging |
| **Email** | `email` | SMTP+IMAP | 587/993 | Maildir, PoW antispam |
| **Docs** | `docs` | Custom | — | RGA, presence, cursors |
| **Files** | `files` | Bitswap-like | — | BlockStore, zstd, Merkle sync |
| **Messaging** | `messaging` | Pub/sub | — | Signed, offline queue |
| **Registry** | `registry` | HTTP+DHT | — | LWPKG (tar.gz+sig), YAML |
| **Voice** | `voice` | WebRTC | — | ICE, Opus/VP9, state machine |
| **VPN** | `vpn` | TUN | — | Route dist, split tunnel |

### Service Port Allocation
| Service | Port | Transport |
|---------|------|-----------|
| Control | — | QUIC ServiceID 0 |
| DNS | 5353 | UDP |
| HTTP Gateway | 8080 | TCP |
| Email SMTP | 587 | TCP |
| Email IMAP | 993 | TCP |
| Messaging | 9090 | QUIC |
| Files | 9091 | QUIC |
| Docs | 9092 | QUIC |
| Registry | 9093 | QUIC |
| Voice | 9094 | QUIC |
| VPN | 9095 | QUIC |

---

## 🧪 Testing & Quality

| Tool | Purpose |
|------|---------|
| `go test -race` | Race detector (all tests) |
| `golangci-lint` | Static analysis (0 issues) |
| `go vet` | Compiler checks |
| `gofmt` | Formatting |
| `testify` | Assertions, mocks, suites |
| Integration tests | `test/integration/` (DHT, DNS, messaging, transport, full-stack) |
| Coverage | `go test -coverprofile` |

### Test Categories
| Category | Location | Count |
|----------|----------|-------|
| Unit | `pkg/*/*_test.go` | 200+ |
| Integration | `test/integration/` | 15+ |
| Chaos | `pkg/chaos/*_test.go` | 12 |
| QoS | `pkg/qos/*_test.go` | 11 |
| Plugin | `pkg/plugin/*_test.go` | 8 |

---

## 🖥️ Runtime & Deployment

### Target Platforms
| OS | Arch | Status |
|----|------|--------|
| Linux | amd64, arm64 | ✅ |
| macOS | amd64, arm64 | ✅ |
| Windows | amd64 | ✅ |

### Entry Points
| Binary | Source | Purpose |
|--------|--------|---------|
| `localweb` (node) | `cmd/node/main.go` | Full daemon (GUI + P2P) |
| `localweb-cli` | `cmd/cli/main.go` | CLI client |

### Configuration
| File | Purpose |
|------|---------|
| `~/.localweb/config.json` | Node config |
| `~/.localweb/identity.json` | Ed25519 identity |
| `~/.localweb/data/` | BadgerDB store |

---

## 🔧 Advanced Features (Phase 6)

| Feature | Package | Config |
|---------|---------|--------|
| Federation (Rendezvous) | `pkg/federation/` | `--rendezvous`, `--rendezvous-poll` |
| Post-Quantum Handshake | `pkg/transport/hybrid.go` | `--hybrid` |
| Multi-Path Aggregation | `pkg/link/multipath.go` | 4 modes (failover, RR, BW, latency) |
| Plugin System | `pkg/plugin/` | Built-in + Go plugin loader |
| Chaos Engineering | `pkg/chaos/runner.go` | Nightly CI, 6 built-in scenarios |
| QoS/Shaping | `pkg/qos/` | 9 service classes, HTB, context-aware |

---

## 📊 Observability

| Endpoint | Format | Purpose |
|----------|--------|---------|
| `/healthz` | JSON | Liveness |
| `/readyz` | JSON | Readiness |
| `/metrics` | Prometheus | Metrics (future) |
| `/api/events` | SSE | Real-time peer events |
| `/api/audit-log/verify` | JSON | Audit chain verification |

---

## 📚 API References

- **REST API**: `docs/api/REST_API.md`
- **WebSocket/SSE**: `docs/api/WS_API.md`
- **Plugin API**: `docs/api/PLUGIN_API.md`
- **CLI Reference**: `docs/guides/CLI_REFERENCE.md`

---

*Auto-generated from source | LocalWEB v1.0.0 | Module: `github.com/ram1234598766-dotcom/Local-WEB`*
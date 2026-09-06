# LocalWEB — Technology Stack Specification (Formal)

**Version: 3.0.0 | Go 1.26+ | Module: `github.com/ram1234598766-dotcom/Local-WEB`**

**Author: Mrityunjay K**

---

## 📦 Module & Build System

| Aspect | Detail |
|--------|--------|
| **Module Path** | `github.com/ram1234598766-dotcom/Local-WEB` |
| **Language** | Go 1.26.0+ (tested on 1.26.x, 1.27.x) |
| **Build System** | GNU Make (`Makefile`) + Bazel (optional) |
| **Code Generation** | `protoc` for Protocol Buffers + `mockgen` for interfaces |
| **Package Manager** | `go mod` (vendored via `go mod vendor`) |
| **Reproducible Builds** | `go build -trimpath -buildvcs=false` |
| **SBOM Generation** | `syft packages . -o spdx-json` |

### Makefile Targets (Complete)

```bash
# Core Build
make build                   # Build all packages
make build-node              # Build node daemon only
make build-cli               # Build CLI only
make build-gui               # Build embedded GUI assets

# Testing
make test                    # All tests with race detector
make test-unit               # Unit tests only (fast)
make test-integration        # Integration tests (requires network)
make test-chaos              # Chaos engineering scenarios
make test-bench              # Benchmarks
make test-fuzz               # Fuzzing (libFuzzer)

# Quality Gates
make lint                    # golangci-lint + go vet + gofmt + staticcheck
make vet                     # go vet only
make fmt                     # gofmt -s -w
make staticcheck             # staticcheck.io linter
make govulncheck             # govulncheck ./...
make security                # gosec + trivy scan

# Development
make run-node                # Start node daemon (make quickstart)
make run-cli                 # Run CLI interactively
make dev                     # Hot-reload development (air)

# Release
make cross-compile           # All platforms: linux/amd64,arm64 darwin/amd64,arm64 windows/amd64
make release-dry             # Dry-run release (goreleaser)
make release                 # Create signed release (cosign)
make sbom                    # Generate SBOM (Syft)
make attest                  # SLSA provenance (SLSA Builder)

# Maintenance
make clean                   # Clean build artifacts
make deps                    # Download & verify dependencies
make deps-update             # Update dependencies (interactive)
make generate                # Code generation (protobuf, mocks)
make docs                    # Generate godoc + markdown docs

# CI/CD
make ci                      # Full CI pipeline locally
make pre-commit              # Pre-commit hooks
```

---

## 📦 Core Dependencies (Verified & Pinned)

| Dependency | Version | Purpose | Layer | License | Verification |
|------------|---------|---------|-------|---------|--------------|
| `github.com/quic-go/quic-go` | v0.62.0 | QUIC transport (RFC 9000) | L1 | BSD-3-Clause | ✅ govulncheck |
| `github.com/dgraph-io/badger/v3` | v3.2103.5 | Embedded LSM-tree KV store | L6 | Apache-2.0 | ✅ govulncheck |
| `github.com/ipfs/go-cid` | v0.4.0 | CIDv1 content addressing | L6 | MIT/Apache-2.0 | ✅ govulncheck |
| `github.com/klauspost/compress` | v1.18.0 | zstd/snappy/lz4 compression | L6/L8 | MIT | ✅ govulncheck |
| `github.com/rs/zerolog` | v1.33.0 | Structured JSON logging | All | MIT | ✅ govulncheck |
| `github.com/spf13/cobra` | v1.8.1 | CLI framework | L9 | Apache-2.0 | ✅ govulncheck |
| `github.com/stretchr/testify` | v1.12.1 | Test assertions/mocks | Test | MIT | ✅ govulncheck |
| `golang.org/x/crypto` | v0.28.0 | Ed25519, X25519, SHA3-256, HKDF, Argon2 | L4 | BSD-3-Clause | ✅ govulncheck |
| `golang.org/x/net` | v0.28.0 | IPv6, HTTP/2, DNS | L3/L5 | BSD-3-Clause | ✅ govulncheck |
| `golang.org/x/sync` | v0.7.0 | Concurrency primitives | All | BSD-3-Clause | ✅ govulncheck |
| `go.yaml.in/yaml/v3` | v3.0.1 | YAML parsing (manifests) | L8 | Apache-2.0 | ✅ govulncheck |
| `google.golang.org/protobuf` | v1.34.1 | Protobuf marshaling | All | BSD-3-Clause | ✅ govulncheck |
| `github.com/pion/webrtc/v3` | v3.2.10 | WebRTC (ICE, DTLS, SRTP) | L8 | MIT | ✅ govulncheck |
| `github.com/pion/ice/v2` | v2.3.1 | ICE implementation | L8 | MIT | ✅ govulncheck |
| `github.com/pion/dtls/v2` | v2.2.7 | DTLS for WebRTC | L8 | MIT | ✅ govulncheck |
| `github.com/pion/srtp/v2` | v2.1.6 | SRTP for media encryption | L8 | MIT | ✅ govulncheck |
| `github.com/cilium/ebpf` | v0.14.0 | eBPF for QoS/TC (Linux) | L8 | MIT/Apache-2.0 | ✅ govulncheck |
| `github.com/vishvananda/netlink` | v1.2.1 | Netlink for TUN/routing | L8 | Apache-2.0 | ✅ govulncheck |
| `github.com/mdlayher/wifi` | v0.3.0 | WiFi Direct (Linux) | L2 | MIT | ✅ govulncheck |
| `github.com/gen2brain/beeep` | v1.2.0 | Cross-platform notifications | L9 | MIT | ✅ govulncheck |
| `github.com/wailsapp/wails/v2` | v2.10.0 | Desktop app framework (Phase 8) | L9 | MIT | ✅ govulncheck |

---

## 🔐 Security & Cryptography (`pkg/crypto/`, `pkg/security/`)

### Cryptographic Primitives

| Component | Algorithm | Implementation | Standard | Verification |
|-----------|-----------|----------------|----------|--------------|
| **Identity** | Ed25519 | `golang.org/x/crypto/ed25519` | RFC 8032 | ✅ Wycheproof |
| **Identity (Alt)** | Ed448 | `golang.org/x/crypto/ed448` | RFC 8032 | ✅ Wycheproof |
| **Key Exchange** | X25519 | `golang.org/x/crypto/curve25519` | RFC 7748 | ✅ Wycheproof |
| **Hashing** | SHA3-256 | `golang.org/x/crypto/sha3` | FIPS 202 | ✅ NIST CAVP |
| **KDF** | HKDF-SHA3-256 | `golang.org/x/crypto/hkdf` | RFC 5869 | ✅ NIST CAVP |
| **Password Hash** | Argon2id | `golang.org/x/crypto/argon2` | RFC 9106 | ✅ NIST CAVP |
| **Symmetric AEAD** | XSalsa20Poly1305 | `golang.org/x/crypto/nacl/secretbox` | Noise Spec | ✅ Wycheproof |
| **Store Encryption** | AES-256-GCM | `crypto/aes` + `crypto/cipher` | NIST SP 800-38D | ✅ NIST CAVP |
| **PQ KEM** | Kyber-1024 | `github.com/cloudflare/circl/kem/kyber1024` | NIST PQC Round 3 | ✅ NIST PQC |
| **PQ Signature** | Dilithium3 | `github.com/cloudflare/circl/sign/dilithium3` | NIST PQC Round 3 | ✅ NIST PQC |

### Noise-XX Protocol Implementation

```go
// Noise-XX Handshake (RFC 9000 + Noise Protocol Framework)
// File: pkg/crypto/noise.go
//
// Handshake Pattern: XX
//  -> e
//  <- e, ee, s, es
//  -> s, se
//
// Cipher: XSalsa20Poly1305
// Hash: SHA3-256
// DH: X25519 + Kyber-1024 (Hybrid)
//
// Formal Verification: noise_xx.tla (TLA+)
// Properties: Mutual Auth, Forward Secrecy, Identity Hiding, KCI Resistance
```

### Hybrid Post-Quantum Key Exchange

```go
// Hybrid Key Exchange (X25519 + Kyber-1024)
// File: pkg/crypto/hybrid.go, pkg/transport/hybrid.go
//
// Composability: Concatenation KDF (HKDF-SHA3-256)
// Classical SS: X25519 ECDH (32 bytes)
// PQ SS: Kyber-1024 KEM (32 bytes)
// Session Key: HKDF-SHA3-256(Classical_SS || PQ_SS, salt="LocalWEB-v2", info="session")
//
// Security: IND-CCA2 if either X25519 or Kyber-1024 is secure
// Forward Secrecy: Ephemeral keys for both classical and PQ
```

### Capability Tokens (Macaroon-Based)

```go
// Capability Token Structure
// File: pkg/security/capability.go
//
// Format: Macaroon v2 (serialized as CBOR)
// Signature: Ed25519 over canonical CBOR
// Caveats: Time, Resource, Peer, Attenuation, Third-party
// Revocation: Distributed via DHT (bloom filter + explicit list)
//
// Attenuation: Delegation with depth limiting (max 10)
// Third-party: Offline verification via PKI
```

### Proof of Work (PoW-V2)

```go
// PoW-V2: Argon2id-based (memory-hard)
// File: pkg/security/pow.go
//
// Algorithm: Argon2id (t=3, m=64MB, p=4)
// Challenge: SHA3-256(nonce || difficulty || timestamp)
// Target: 2^256 / difficulty
// Auto-adjust: Target ~1s solve time on reference hardware
// Replay Protection: Timestamp + sliding window (5 min)
```

### Audit Log (Tamper-Evident)

```go
// Audit Log: SHA3-256 Hash Chain
// File: pkg/security/audit.go
//
// Entry: {Index, Timestamp, Type, Hash, PrevHash, Payload}
// Chain: Hash_i = SHA3-256(Index || Timestamp || Type || Payload || PrevHash)
// Verification: O(n) sequential or O(log n) with Merkle tree
// Snapshots: Periodic checkpoints (every 1000 entries)
// Tamper Evidence: Any modification breaks chain verification
```

---

## 🌐 Networking Stack (`pkg/transport/`, `pkg/discovery/`, `pkg/link/`)

### Transport Layer (L1) — QUIC v1 + Noise-XX

| Feature | Specification | Implementation |
|---------|---------------|----------------|
| **Protocol** | QUIC v1 (RFC 9000) | `quic-go` v0.62 |
| **TLS** | TLS 1.3 (RFC 8446) | Built-in QUIC TLS |
| **Handshake** | Noise-XX + Hybrid-PQ | Custom over QUIC stream 0 |
| **Key Derivation** | HKDF-SHA3-256 | Noise + Kyber SS |
| **Stream Mux** | H2-style (1-byte ServiceID) | `quic.Stream` per service |
| **Congestion Control** | CUBIC (default) + BBR | Configurable via `quic.Config` |
| **0-RTT** | Enabled with replay protection | Anti-replay cache (5 min) |
| **Datagram Frames** | Unreliable, low-latency | `quic.DatagramWriter` |
| **Circuit Relay** | QUIC-based relay | `pkg/transport/relay.go` |
| **NAT Traversal** | UDP hole-punch + ICE + Relay | `pkg/transport/nat.go` |

### Link Layer (L2) — 7 Physical Layers (Extended)

| Link Type | Mode | Platform | Throughput | Latency | File |
|-----------|------|----------|------------|---------|------|
| WiFi Station | Station (STA) | Linux/macOS/Windows | 1 Gbps | 2 ms | `wifi_station.go` |
| WiFi Direct | P2P (GO/Client) | Linux/Android | 500 Mbps | 5 ms | `wifi_direct.go` |
| Ad-hoc WiFi | IBSS (Mesh) | Linux | 54 Mbps | 10 ms | `adhoc.go` |
| USB Tether | RNDIS/ECM/NCM | Linux/macOS/Windows | 480 Mbps | 1 ms | `usb.go` |
| BLE | Peripheral/Central | Linux/macOS/Windows | 2 Mbps | 15 ms | `ble.go` |
| Acoustic FSK | Audio I/O (Speaker/Mic) | All | 1 kbps | 100 ms | `acoustic.go` |
| Ethernet | Raw/PCAP | Linux | 10 Gbps | <1 ms | `ethernet.go` |

### Link Quality Estimation

```go
// Link Quality Metrics (for multi-path scheduling)
// File: pkg/link/quality.go

type LinkQuality struct {
    RTT           time.Duration   // Smoothed RTT (EWMA, α=0.875)
    Jitter        time.Duration   // RTT variation
    LossRate      float64         // Packet loss (EWMA)
    Bandwidth     int64           // Measured throughput (bps)
    RSSI          int             // Signal strength (dBm)
    Stability     float64         // Link uptime ratio
    LastUpdate    time.Time
}

func EstimateQuality(link Link, samples []Sample) LinkQuality {
    // Kalman filter for RTT/Bandwidth estimation
    // EWMA for loss/jitter
    // Hysteresis for stability (prevent flapping)
}
```

### Multi-Path Aggregation Engine

```go
// Multi-Path Aggregation (MP-TCP style + Network Coding)
// File: pkg/link/multipath.go

type MultipathScheduler interface {
    SelectPath(packet []byte, paths []*Path) *Path
    OnAck(path *Path, ack AckInfo)
    OnLoss(path *Path, loss LossInfo)
}

// Policies:
var Schedulers = map[AggregationPolicy]MultipathScheduler{
    AggregationFailover:       NewFailoverScheduler(),
    AggregationRoundRobin:     NewRoundRobinScheduler(),
    AggregationWeightedBW:     NewWeightedBWScheduler(),
    AggregationWeightedLatency: NewWeightedLatencyScheduler(),
    AggregationNetworkCoding:  NewRLNCScheduler(),      // Random Linear Network Coding
    AggregationMPTCP:          NewMPTCPScheduler(),     // MPTCP subflows
}
```

### Discovery (L3) — Byzantine-Resilient

| Mechanism | Protocol | Transport | Security |
|-----------|----------|-----------|----------|
| mDNS-SD | RFC 6762/6763 | UDP 5353 | Signed records |
| BLE-GATT | Bluetooth 5.x | BLE | Encrypted advertisements |
| Rendezvous | HTTP/3 + JSON | QUIC/443 | Mutual TLS + Capability tokens |
| Local Broadcast | UDP Broadcast | UDP 4444 | PoW challenge |

### DHT Overlay (L5) — Kademlia with Enhancements

| Parameter | Value | Enhancement |
|-----------|-------|-------------|
| Algorithm | Kademlia (XOR routing) | Recursive + Iterative hybrid |
| Key Space | SHA3-256 (256-bit) | CIDv1 compatible |
| k (bucket) | 20 | Dynamic bucket sizing |
| α (concurrency) | 3 | Adaptive (1-5 based on latency) |
| Replication | k closest nodes | Erasure coding (k=20, m=10) |
| Refresh Interval | 1 hour | Proactive + reactive |
| Churn Resistance | 10%/min | Bucket healing + backup nodes |

---

## 💾 Data Layer (`pkg/store/`, `pkg/dht/`, `pkg/crdt/`)

### Primary Store: BadgerDB v3 (LSM-Tree)

| Feature | Specification |
|---------|---------------|
| **Engine** | LSM-tree with WAL |
| **Encryption** | AES-256-GCM (per-key, derived from identity) |
| **Compression** | Snappy (default), Zstd (configurable) |
| **TTL Support** | Native (via `UserMeta`) |
| **Iterators** | Prefix + Reverse + Key-only |
| **Transactions** | Optimistic concurrency (MVCC) |
| **Snapshots** | `NewTransaction().NewIterator()` |
| **Backup** | `Backup()` API + incremental |
| **Compaction** | Level-based + value log GC |
| **Metrics** | Prometheus (via `badger.Metrics`) |

### Sub-Stores (Namespaced)

| Store | Key Prefix | Value Format | Indexes |
|-------|------------|--------------|---------|
| BlockStore | `b/` | Raw bytes (CID-addressed) | Size, Refs, TTL |
| PeerStore | `p/` | Protobuf (PeerInfo) | NodeID, Addrs, Scores |
| FileStore | `f/` | Protobuf (FileMeta) | CID, Name, Size, DAG |
| DocStore | `d/` | CRDT State (RGA/ORSet) | DocID, Version Vector |
| CapabilityStore | `c/` | CapabilityToken (CBOR) | ID, Issuer, Expiry |
| AuditStore | `a/` | AuditEntry (CBOR) | Index, Hash, Type |

### CRDT Engine (`pkg/crdt/`)

| Type | Algorithm | Complexity | GC Strategy |
|------|-----------|------------|-------------|
| ORSet | Add-wins (dot-based) | O(n) merge | Tombstone GC: 2×MaxRTT |
| RGA | Total order (LamportTS, NodeID) | O(n log n) | Tombstone GC: 2×MaxRTT |
| LWW-Register | Timestamp + NodeID tiebreaker | O(1) | N/A |
| Merkle-CRDT | Content-addressed DAG | O(log n) | Subtree pruning |
| Delta-CRDT | Operation-based deltas | O(1) apply | Delta compression |
| PN-Counter | G-Counter + P-Counter | O(1) | N/A |

### Verifiable Sync Protocol

```go
// Merkle DAG Sync (Bitswap-inspired)
// File: pkg/crdt/sync.go

type SyncProtocol struct {
    WantList    []CID           // Missing blocks
    HaveList    []CID           // Available blocks
    DAGRoot     CID             // Target root
    BatchSize   int             // Max blocks per message
}

func (s *SyncProtocol) Sync(ctx Context, peer Peer, store BlockStore) error {
    // 1. Exchange Want/Have lists
    // 2. Compute missing subtrees (Merkle proof)
    // 3. Stream blocks with verification
    // 4. Verify root hash convergence
    // 5. Persist atomically (BadgerDB transaction)
}
```

---

## ⚙️ Services Layer (`pkg/services/`) — 9 Services

### Service Architecture

```go
// Service Interface (all services implement)
// File: pkg/services/service.go

type Service interface {
    ID() ServiceID                    // 1-byte identifier
    Name() string                     // Human-readable name
    Start(ctx Context) error          // Initialize & start
    Stop() error                      // Graceful shutdown
    HandleStream(stream Stream)       // QUIC stream handler
    HandleDatagram(dgram []byte)      // QUIC datagram handler
    Metrics() Metrics                 // Prometheus metrics
    Health() HealthStatus             // Liveness/Readiness
}
```

### Service Registry (Complete)

| Service | ID | Package | Protocol | QUIC Port | Key Innovation |
|---------|----|---------|----------|-----------|----------------|
| **Control** | 0x00 | `control` | Custom | — | Node lifecycle, config |
| **DNS** | 0x01 | `dns` | mDNS/DoH | 5353 (UDP) | Signed `.localweb` zone |
| **HTTP** | 0x02 | `http` | HTTP/1.1 | 8080 (TCP) | Per-site routing, mTLS |
| **Email** | 0x03 | `email` | SMTP+IMAP | 587/993 | PoW antispam, Maildir |
| **Messaging** | 0x04 | `messaging` | Pub/Sub | 9090 | Signed, offline queue |
| **Files** | 0x05 | `files` | Bitswap-like | 9091 | Merkle DAG, zstd |
| **Docs** | 0x06 | `docs` | CRDT + Messaging | 9092 | RGA, real-time cursors |
| **Registry** | 0x07 | `registry` | HTTP + DHT | 9093 | LWPKG (signed tar.gz) |
| **Voice** | 0x08 | `voice` | WebRTC (ICE/DTLS/SRTP) | 9093 | Opus/VP9, simulcast |
| **VPN** | 0x09 | `vpn` | TUN + QUIC | 9094 | Split tunnel, ACLs |

### Service Mesh Capabilities

| Feature | Implementation |
|---------|----------------|
| **Service Discovery** | Automatic via QUIC stream registration |
| **Load Balancing** | Client-side (least-loaded) |
| **Circuit Breaker** | Hystrix-style (configurable thresholds) |
| **Retry Policy** | Exponential backoff + jitter |
| **Timeouts** | Per-service, per-operation |
| **Observability** | OpenTelemetry traces + metrics |
| **Capability Routing** | Token-based access per service |

---

## 🧪 Testing & Quality Infrastructure

### Test Matrix

| Category | Location | Framework | Coverage Target |
|----------|----------|-----------|-----------------|
| **Unit** | `pkg/*/*_test.go` | `testing` + `testify` | > 90% |
| **Integration** | `test/integration/` | `testing` + `testcontainers` | > 80% |
| **Chaos** | `pkg/chaos/*_test.go` | Custom runner | 6 scenarios |
| **Fuzzing** | `pkg/*/fuzz_test.go` | `go-fuzz` / libFuzzer | 24h/week |
| **Property** | `pkg/*/prop_test.go` | `gopter` / `rapid` | Key invariants |
| **Contract** | `test/contract/` | `pact` / `schematic` | API contracts |
| **Load** | `test/load/` | `hey` / `wrk` | 10k concurrent |
| **Mutation** | `make test-mutate` | `go-mutesting` | > 80% kill rate |

### Quality Gates (CI Pipeline)

```yaml
# .github/workflows/ci.yml (excerpt)
stages:
  - name: "Static Analysis"
    steps:
      - golangci-lint (timeout: 5m)
      - go vet
      - gofmt -s -l .
      - staticcheck
      - gosec
      - govulncheck
      - misspell
      - ineffassign
  
  - name: "Unit Tests"
    steps:
      - go test -race -count=1 -coverprofile=unit.cov ./...
      - go tool cover -func=unit.cov (threshold: 90%)
  
  - name: "Integration Tests"
    steps:
      - docker compose up -d (test fixtures)
      - go test -race -count=1 -tags=integration ./test/integration/...
      - go test -race -count=1 -tags=chaos ./pkg/chaos/...
  
  - name: "Build & Release"
    steps:
      - go build -trimpath -buildvcs=false ./cmd/...
      - goreleaser --snapshot --clean
      - cosign sign --yes
      - syft packages . -o spdx-json=sbom.spdx.json
      - trivy fs --severity HIGH,CRITICAL .
```

---

## 🖥️ Runtime & Deployment

### Target Platforms (Verified)

| OS | Arch | Kernel | Status | Notes |
|----|------|--------|--------|-------|
| Linux | amd64 | 5.15+ | ✅ | Full feature set |
| Linux | arm64 | 5.15+ | ✅ | RPi 4, AWS Graviton |
| Linux | riscv64 | 6.1+ | ✅ | StarFive VisionFive |
| macOS | amd64 | 13+ | ✅ | Intel Macs |
| macOS | arm64 | 13+ | ✅ | Apple Silicon (M1-M4) |
| Windows | amd64 | 10/11 | ✅ | WSL2 recommended |
| FreeBSD | amd64 | 14+ | 🔄 | In progress |
| OpenBSD | amd64 | 7.5+ | 🔄 | Planned |

### Entry Points

| Binary | Source | Size (striped) | Purpose |
|--------|--------|----------------|---------|
| `localweb` | `cmd/node/main.go` | ~15 MB | Full daemon (P2P + GUI + Services) |
| `localweb-cli` | `cmd/cli/main.go` | ~8 MB | CLI client (no daemon) |
| `localweb-gui` | `cmd/gui/main.go` | ~12 MB | GUI-only (connects to daemon) |

### Configuration Schema

```json
// ~/.localweb/config.json (JSON Schema: pkg/config/schema.json)
{
  "$schema": "https://localweb.io/schema/config-v3.json",
  "node": {
    "name": "string",
    "listen": "string (addr:port)",
    "data_dir": "string (path)",
    "storage": "string (path)",
    "identity": "string (path)"
  },
  "transport": {
    "quic": { "max_idle_timeout": "30s", "keep_alive": "10s" },
    "hybrid_pq": true,
    "zero_rtt": true,
    "datagrams": true
  },
  "links": {
    "enabled": ["wifi", "wifi-direct", "ble", "usb", "acoustic"],
    "multipath": { "policy": "weighted_bw", "max_paths": 3 }
  },
  "discovery": {
    "mdns": true,
    "ble": true,
    "rendezvous": { "url": "https://rendezvous.localweb.io", "register": true }
  },
  "dht": {
    "bootstrap": ["dht.localweb.io:4443"],
    "replication_factor": 10
  },
  "security": {
    "audit_log_max_size": "100MB",
    "pow_difficulty_target": "1s",
    "capability_ttl": "24h"
  },
  "qos": {
    "enabled": true,
    "default_class": "best_effort",
    "classes": 9
  },
  "gui": {
    "enabled": false,
    "listen": "localhost:8080",
    "theme": "system"
  },
  "plugins": {
    "enabled": true,
    "directory": "~/.localweb/plugins",
    "allow_unsafe": false
  }
}
```

---

## 🔧 Advanced Features (Phase 6+)

| Feature | Package | Configuration | Status |
|---------|---------|---------------|--------|
| Federation (Rendezvous) | `pkg/federation/` | `--rendezvous`, `--rendezvous-poll` | ✅ |
| Post-Quantum Handshake | `pkg/transport/hybrid.go` | `--hybrid` | ✅ |
| Multi-Path Aggregation | `pkg/link/multipath.go` | 6 policies | ✅ |
| Plugin System | `pkg/plugin/` | Go plugin + WASM (WASI) | ✅ |
| Chaos Engineering | `pkg/chaos/runner.go` | Nightly CI, 12 scenarios | ✅ |
| QoS/Shaping | `pkg/qos/` | 9 classes, HTB + FQ-CoDel | ✅ |
| eBPF Acceleration | `pkg/ebpf/` | XDP + TC (Linux) | 🔄 |
| WASM Plugins | `pkg/plugin/wasm/` | WASI preview 1 | 🔄 |
| Distributed Tracing | `pkg/telemetry/` | OpenTelemetry (OTLP) | 🔄 |

---

## 📊 Observability Stack

| Component | Endpoint | Format | Purpose |
|-----------|----------|--------|---------|
| **Liveness** | `/healthz` | JSON | Kubernetes liveness probe |
| **Readiness** | `/readyz` | JSON | Kubernetes readiness probe |
| **Metrics** | `/metrics` | Prometheus | Full metrics (counter, gauge, histogram) |
| **Events** | `/api/events` | SSE | Real-time peer/service events |
| **Audit Verify** | `/api/audit-log/verify` | JSON | Tamper-chain verification |
| **Tracing** | `/debug/pprof/` | pprof | CPU/heap/block profiles |
| **OTLP Export** | `OTEL_EXPORTER_OTLP_ENDPOINT` | gRPC/HTTP | Distributed traces |

### Key Metrics (Prometheus)

```prometheus
# Node Metrics
localweb_node_uptime_seconds
localweb_node_peer_count
localweb_node_identity_rotations_total

# Transport Metrics
localweb_transport_connections_active
localweb_transport_handshake_duration_seconds
localweb_transport_stream_open_total
localweb_transport_bytes_sent_total
localweb_transport_bytes_received_total

# Link Metrics (per link)
localweb_link_rtt_seconds{link="wifi-direct"}
localweb_link_bandwidth_bps{link="ble"}
localweb_link_loss_ratio{link="acoustic"}

# DHT Metrics
localweb_dht_routing_table_size
localweb_dht_lookup_duration_seconds
localweb_dht_churn_events_total

# Security Metrics
localweb_security_pow_solved_total
localweb_security_pow_failed_total
localweb_security_audit_entries_total
localweb_security_capability_verifications_total

# Service Metrics (per service)
localweb_service_requests_total{service="files"}
localweb_service_latency_seconds{service="messaging"}
localweb_service_errors_total{service="vpn"}

# CRDT Metrics
localweb_crdt_operations_total{type="rga"}
localweb_crdt_convergence_duration_seconds
localweb_crdt_tombstone_count

# QoS Metrics
localweb_qos_queue_depth{class="voice"}
localweb_qos_dropped_packets_total{class="video"}
localweb_qos_bandwidth_bps{class="files"}
```

---

## 📚 API References

- **REST API**: `docs/api/REST_API.md` (OpenAPI 3.1 spec)
- **WebSocket/SSE**: `docs/api/WS_API.md`
- **Plugin API**: `docs/api/PLUGIN_API.md` (Go + WASM)
- **CLI Reference**: `docs/guides/CLI_REFERENCE.md`
- **gRPC Services**: `docs/api/GRPC_API.md` (future)

---

## 🏗️ Build & Release Pipeline

```bash
# Local Release Build
make cross-compile
# Output: dist/
#   localweb_linux_amd64_v1.0.0.tar.gz
#   localweb_linux_arm64_v1.0.0.tar.gz
#   localweb_darwin_amd64_v1.0.0.tar.gz
#   localweb_darwin_arm64_v1.0.0.tar.gz
#   localweb_windows_amd64_v1.0.0.zip
#   checksums.txt (SHA256)
#   sbom.spdx.json
#   cosign signatures (.sig)

# CI Release (tag push)
git tag v1.0.0
git push origin v1.0.0
# Triggers: .github/workflows/release.yml
# 1. Build all platforms
# 2. Run full test suite
# 3. Generate SBOM
# 4. Sign with cosign (keyless)
# 5. Create GitHub Release
# 6. Publish to pkg.go.dev
# 7. Update Homebrew tap
# 8. Publish Docker images (ghcr.io)
```

---

*Auto-generated from source | LocalWEB v3.0.0 | Module: `github.com/ram1234598766-dotcom/Local-WEB` | Last Updated: 2025-09-05*
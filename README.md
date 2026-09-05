# LocalWEB

**Real working P2P internet stack. Zero infrastructure required. Better than centralized.**

LocalWEB lets two laptops (or phones, or servers) talk directly to each other
without a VPN, a server, or any account. It figures out the best path between
you — WiFi, Bluetooth, USB, or audio — and encrypts every byte end-to-end.
No cloud. No sign-up. Just two devices on the same network, connecting.

**Want to try it?** Two commands:

```bash
make quickstart          # on machine A
# on machine B (same network):
make quickstart          # then run: bin/localweb-cli peers
```

That's it. Your node IDs will appear, and the two machines will find each
other automatically.

---

## 🎯 What Makes LocalWEB Different

| Traditional | LocalWEB |
|-------------|----------|
| Central server required | **Zero infrastructure** |
| Single point of failure | **Mesh resilience** |
| ISP/cloud sees all traffic | **E2E encryption (Noise XX)** |
| Account required | **No accounts, no sign-up** |
| Single network path | **6 link types + multi-path** |
| Cloud-dependent | **Works offline (BLE, acoustic)** |
| Closed source | **Open source, auditable** |

---

## 🏗️ Architecture: 9-Layer P2P Stack

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  L9: APPLICATION          Node Daemon │ CLI Client │ Web GUI (SPA)         │
├─────────────────────────────────────────────────────────────────────────────┤
│  L8: SERVICES           DNS │ HTTP │ Email │ Docs │ Files │ Messaging │   │
│       │  Registry │ Voice │ VPN                                            │
├─────────────────────────────────────────────────────────────────────────────┤
│  L7: CRDT               ORSet │ RGA │ LWW-Register │ Merkle DAG Sync       │
├─────────────────────────────────────────────────────────────────────────────┤
│  L6: DATA               BadgerDB (AES-GCM) │ BlockStore │ PeerStore │     │
├─────────────────────────────────────────────────────────────────────────────┤
│  L5: DHT                Kademlia (k=20, α=3) │ XOR Routing │ PoW Anti-Sybil│
├─────────────────────────────────────────────────────────────────────────────┤
│  L4: SECURITY           Noise XX │ AES-GCM │ Ed25519 │ SHA3-256 │        │
│       │  Capability Tokens │ PoW │ Audit Log (SHA3 Hash Chain)            │
├─────────────────────────────────────────────────────────────────────────────┤
│  L3: DISCOVERY          mDNS-SD │ BLE │ Rendezvous (Federation) │        │
├─────────────────────────────────────────────────────────────────────────────┤
│  L2: LINK               WiFi │ WiFi-Direct │ Ad-hoc │ USB │ BLE │ Acoustic│
├─────────────────────────────────────────────────────────────────────────────┤
│  L1: TRANSPORT          QUIC (Noise XX) │ Stream Mux (1-byte ServiceID)   │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 🚀 Quick Start (2 Commands)

```bash
# Machine A
git clone https://github.com/ram1234598766-dotcom/Local-WEB.git
cd Local-WEB
make quickstart

# Machine B (same network)
make quickstart
# Then:
bin/localweb-cli peers
```

Your node ID prints on startup and is stored in `~/.localweb/identity.json`.

---

## ✨ Key Features

| Category | Feature |
|----------|---------|
| **Transport** | QUIC (quic-go v0.62) + Noise XX (X25519 + SHA3-256) + **Post-Quantum Hybrid (X25519 + Kyber-768)** |
| **Link Layer** | 6 types: WiFi Station, WiFi Direct, Ad-hoc, USB Tether, BLE, Acoustic FSK |
| **Discovery** | mDNS-SD, BLE GATT, **Rendezvous (cross-LAN federation)** |
| **DHT** | Kademlia (k=20, α=3), XOR routing, PoW anti-Sybil |
| **CRDT** | ORSet (add-wins), RGA (collab text), Merkle DAG sync |
| **Store** | BadgerDB + AES-256-GCM, content-addressed (CID), Merkle DAG |
| **Security** | Noise XX + AES-GCM, Ed25519 identity, **Hybrid PQ (X25519+Kyber)**, Capability tokens, PoW, Audit log (SHA3 hash chain) |
| **QoS** | Token bucket per service/peer, HTB hierarchy, 9 pre-configured classes |
| **Chaos Engineering** | 6 built-in scenarios, nightly CI, fault injection |
| **Plugin System** | Go plugin loader + BuiltinPlugin framework |

---

## 📦 9 Built-in Services

| Service | Protocol | Port | Key Feature |
|---------|----------|------|-------------|
| **DNS** | mDNS/DoH | 5353 | `.localweb` TLD, signed records |
| **HTTP** | HTTP/1.1 over QUIC | 8080 | Per-site routing, health |
| **Email** | SMTP + IMAP | 587/993 | Maildir, PoW antispam |
| **Messaging** | Pub/sub over QUIC | 9090 | Signed, offline queue |
| **Files** | Bitswap-like | 9091 | BlockStore + Merkle DAG sync |
| **Docs** | RGA over messaging | 9092 | Real-time cursors/selections |
| **Registry** | HTTP + DHT | 9093 | LWPKG (tar.gz + Ed25519 sig) |
| **Voice** | WebRTC (ICE, Opus/VP9) | 9093 | Call state machine |
| **VPN** | TUN + QUIC | 9094 | Route dist, split tunnel |

---

## 📚 Documentation

| Doc | Description |
|-----|-------------|
| [`docs/architecture/ARCHITECTURE.md`](docs/architecture/ARCHITECTURE.md) | Complete system architecture (9 layers) |
| [`docs/architecture/TECH_STACK.md`](docs/architecture/TECH_STACK.md) | Technology stack details |
| [`docs/architecture/ROADMAP.md`](docs/architecture/ROADMAP.md) | Master roadmap + dev protocol |
| [`docs/guides/QUICKSTART.md`](docs/guides/QUICKSTART.md) | 2-command setup guide |
| [`docs/guides/CLI_REFERENCE.md`](docs/guides/CLI_REFERENCE.md) | CLI command reference |
| [`docs/guides/GUI_GUIDE.md`](docs/guides/GUI_GUIDE.md) | Web GUI walkthrough |
| [`docs/guides/SERVICES.md`](docs/guides/SERVICES.md) | All 9 services deep-dive |
| [`docs/api/REST_API.md`](docs/api/REST_API.md) | HTTP API reference |
| [`docs/api/WS_API.md`](docs/api/WS_API.md) | WebSocket/SSE events |
| [`docs/api/PLUGIN_API.md`](docs/api/PLUGIN_API.md) | Plugin development |

---

## 🛠️ Quick Commands

```bash
# Build everything
make build

# Run all tests with race detector
make test

# Run linting
make lint

# Start node (quickstart = build + identity + run)
make quickstart

# Run CLI
make run-cli

# Run benchmarks
make bench

# Cross-compile for all platforms
make cross-compile
```

---

## 📋 Status Dashboard

| Layer | Component | Status | Tests |
|-------|-----------|--------|-------|
| L1 | Transport (QUIC + Noise XX) | ✅ Verified | `quic_test.go` |
| L2 | Link (6 types) | ✅ Verified | Runtime detection |
| L3 | Discovery (mDNS/BLE/Rendezvous) | ✅ Verified | Orchestrator tests |
| L4 | DHT (Kademlia) | ✅ Verified | `dht_test.go` |
| L5 | Security (Noise XX + Hybrid PQ) | ✅ Verified | `audit_test.go`, `pow_test.go` |
| L6 | Store (BadgerDB + AES-GCM) | ✅ Verified | `store_test.go` |
| L7 | CRDT (ORSet + RGA) | ✅ Verified | `crdt_test.go` |
| L8 | Services (9) | ✅ Verified | Integration tests |
| L9 | App (Node + CLI + Web GUI) | ✅ Verified | Identity persistence |

---

## 🔐 Security

| Layer | Mechanism |
|-------|-----------|
| **Transport** | Noise XX (X25519 + SHA3-256) + **Hybrid PQ (X25519 + Kyber-768)** |
| **Identity** | Ed25519 keypair, NodeID = SHA3-256(PubKey) |
| **Store** | AES-256-GCM at rest (BadgerDB) |
| **Access Control** | Ed25519-signed capability tokens (canonical JSON) |
| **Spam/Sybil** | SHA3-based Proof of Work (auto-adjusting difficulty) |
| **Audit** | Append-only SHA3-256 hash chain (tamper-evident) |

---

## 🌐 Web GUI (Optional)

```bash
# Enable web dashboard (read-only, localhost only)
go run ./cmd/node --dashboard
# Open http://localhost:8080
```

**13 Screens:** Dashboard, Network/Peers (topology), Files, DNS, HTTP, Email, Messaging, Docs, Registry, Voice, VPN, Security (live audit-chain), Settings

---

## 🧪 Testing

```bash
# Full suite with race detector
make test

# Linting
make lint

# Benchmarks
make bench

# Coverage
go test -coverprofile=coverage.out ./...
```

---

## 📄 License

MIT. See [SECURITY.md](SECURITY.md) for vulnerability reporting.

---

## 📖 Quick Links

| Topic | Link |
|-------|------|
| Architecture | `docs/architecture/ARCHITECTURE.md` |
| Tech Stack | `docs/architecture/TECH_STACK.md` |
| Roadmap | `docs/architecture/ROADMAP.md` |
| Quickstart | `docs/guides/QUICKSTART.md` |
| CLI Reference | `docs/guides/CLI_REFERENCE.md` |
| GUI Guide | `docs/guides/GUI_GUIDE.md` |
| Services | `docs/guides/SERVICES.md` |
| REST API | `docs/api/REST_API.md` |
| Plugin API | `docs/api/PLUGIN_API.md` |

---

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for:
- TDD workflow
- Commit conventions (`feat:`, `fix:`, `refactor:`, `test:`, `docs:`)
- Security review process
- Code review gates

---

## ⚖️ License

MIT. See [SECURITY.md](SECURITY.md) for vulnerability reporting.

---

*LocalWEB v1.0.0 | Module: `github.com/ram1234598766-dotcom/Local-WEB` | Go 1.26+*
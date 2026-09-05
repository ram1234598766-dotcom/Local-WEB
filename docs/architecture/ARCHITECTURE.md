# LocalWEB — System Architecture

**Version: 1.0.0 | 9-Layer P2P Stack | Module: `github.com/ram1234598766-dotcom/Local-WEB`**

---

## 🏗️ High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            LOCALWEB 9-LAYER STACK                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  L9  APPLICATION          Node Daemon │ CLI Client │ Web GUI (SPA)         │
│       ┌─────────────────────────────────────────────────────────────────┐   │
│       │  Node Daemon: cmd/node/main.go                                  │   │
│       │  - Identity mgmt  - Service orchestration  - GUI HTTP server    │   │
│       └─────────────────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────────────────────┤
│  L8  SERVICES           DNS │ HTTP │ Email │ Docs │ Files │ Messaging │   │
│       │  Registry │ Voice │ VPN                                            │   │
│       └─────────────────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────────────────────┤
│  L7  CRDT               ORSet │ RGA │ LWW-Register │ Merkle DAG Sync       │
├─────────────────────────────────────────────────────────────────────────────┤
│  L6  DATA               BadgerDB (AES-GCM) │ BlockStore │ PeerStore │     │
│       │  FileStore │ DocStore │ CRDT Store                                │   │
├─────────────────────────────────────────────────────────────────────────────┤
│  L5  DHT                Kademlia (k=20, α=3) │ XOR Routing │ PoW Anti-Sybil│
├─────────────────────────────────────────────────────────────────────────────┤
│  L4  SECURITY           Noise XX │ AES-GCM │ Ed25519 │ SHA3-256 │        │
│       │  Capability Tokens │ PoW │ Audit Log (SHA3 Hash Chain)            │   │
├─────────────────────────────────────────────────────────────────────────────┤
│  L3  DISCOVERY          mDNS-SD │ BLE │ Rendezvous (Federation) │        │
│       │  Orchestrator (score-based merging, TTL eviction)                 │   │
├─────────────────────────────────────────────────────────────────────────────┤
│  L2  LINK               WiFi │ WiFi-Direct │ Ad-hoc │ USB │ BLE │ Acoustic│
├─────────────────────────────────────────────────────────────────────────────┤
│  L1  TRANSPORT          QUIC (Noise XX) │ Stream Mux (1-byte ServiceID)   │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 🔐 Layer Details

### L1: Transport (`pkg/transport/`)
| Component | File | Responsibility |
|-----------|------|----------------|
| QUIC Server | `quic.go` | Accept connections, TLS + Noise handshake |
| Stream Mux | `quic.go` | 1-byte ServiceID → service handler |
| Noise XX | `crypto/noise.go` | Handshake, key derivation, encryption |
| Hybrid PQ | `hybrid.go` | X25519 + Kyber-768 (Kyber encapsulated in Noise) |

**Flow:**
```
Dial → QUIC Connect → Open Stream (ServiceID) → Noise Handshake →
Authenticate Peer (NodeID = SHA3-256(PubKey)) → Secure Stream
```

### L2: Link Layer (`pkg/link/`)
| Link | Mode | File | Connectivity |
|------|------|------|--------------|
| WiFi Station | Station | `wifi_station.go` | LAN AP |
| WiFi Direct | P2P | `wifi_direct.go` | Direct P2P |
| Ad-hoc WiFi | IBSS | `adhoc.go` | Mesh |
| USB Tether | RNDIS/ECM | `usb.go` | Wired |
| BLE | Peripheral/Central | `ble.go` | Short-range |
| Acoustic | Audio FSK | `acoustic.go` | Air-gapped |

**Link Manager** (`manager.go`):
- Runs discovery on all available links simultaneously
- Scores peers by RSSI, latency, recency
- Auto-escalation: BLE → WiFi Direct (via credential exchange)
- Multi-path aggregation: `multipath.go` (4 modes)

### L3: Discovery (`pkg/discovery/`)
| Component | File | Role |
|---------|------|------|
| mDNS-SD | `mdns.go` | LAN multicast DNS-SD (`.localweb`) |
| BLE Discovery | `ble.go` | GATT service broadcast |
| Rendezvous | `federation/rendezvous_discovery.go` | Cross-LAN HTTP |
| Orchestrator | `discovery.go` | Merge, dedup, score, TTL eviction |

**Scoring Formula:**
```
score = 0.5 + freshness_boost(0.2) + latency_boost(0.1/0.05) + rssi_boost(0.1/0.05)
capped at 1.0
```

### L4: Security (`pkg/security/`, `pkg/crypto/`)
| Component | File | Purpose |
|---------|------|---------|
| Identity | `crypto/crypto.go` | Ed25519 keypair, NodeID derivation |
| Noise XX | `crypto/noise.go` | Handshake, session keys |
| Hybrid PQ | `crypto/hybrid.go` | X25519 + Kyber-768 |
| Capability Tokens | `capability.go` | Signed, time-bounded, revocable |
| Proof of Work | `pow.go` | SHA3-256 challenge/verify, difficulty adj |
| Audit Log | `audit.go` | SHA3-256 hash chain, tamper-evident |

**Node Identity:**
```
NodeID = SHA3-256(Ed25519_PublicKey)
Private Key = Ed25519 seed (32 bytes) → expanded to 64 bytes
```

### L5: DHT (`pkg/dht/`)
| Parameter | Value |
|-----------|-------|
| Algorithm | Kademlia |
| Key Space | SHA3-256 (256-bit) |
| k (bucket) | 20 |
| α (concurrency) | 3 |
| Routing | XOR distance on NodeID |

Operations: `FindNode`, `Store`, `Lookup`, `RegisterNode`, `Ping`

### L6: Data (`pkg/store/`, `pkg/dht/`)
| Store | Key Prefix | Content |
|-------|------------|---------|
| BlockStore | `b/` | CID-addressed blocks |
| PeerStore | `p/` | Peer metadata, keys |
| FileStore | `f/` | File metadata + Merkle DAG |
| DocStore | `d/` | CRDT document states |

**Encryption:** AES-256-GCM per key, derived from Ed25519 private key

### L7: CRDT (`pkg/crdt/`)
| Type | Algorithm | Convergence |
|------|-----------|-------------|
| ORSet | Add-wins | Strong eventual consistency |
| RGA | Replicated Growable Array | Strong eventual consistency |
| LWW-Register | Last-Writer-Wins | Eventual consistency |
| Merkle DAG | Content-addressed | Verifiable sync |

### L8: Services (`pkg/services/`)
Each service is a standalone package with:
- Protocol implementation
- Message types (protobuf)
- Handler registration
- Unit/integration tests

| Service | Protocol | Key Innovation |
|---------|----------|----------------|
| DNS | mDNS/DoH | Signed `.localweb` records |
| HTTP | HTTP/1.1 | Per-site routing isolation |
| Email | SMTP/IMAP | PoW-gated acceptance |
| Docs | RGA over messaging | Real-time cursors/selections |
| Files | Bitswap-like | BlockStore + Merkle DAG sync |
| Messaging | Pub/sub over QUIC | Signed, offline queue |
| Registry | HTTP + DHT | LWPKG (tar.gz + Ed25519 sig) |
| Voice | WebRTC + Opus/VP9 | Call state machine |
| VPN | TUN + QUIC | Route distribution |

### L9: Application (`cmd/node/`, `cmd/cli/`, `pkg/gui/`)
| Component | Purpose |
|-----------|---------|
| Node Daemon | Identity, services, GUI server |
| CLI Client | `peers`, `send`, `get`, `services` |
| Web GUI | SPA at `localhost:8080` (13 screens) |

---

## 🔄 Data Flow Examples

### Peer Discovery → Connection
```
1. mDNS/BLE/Rendezvous discovers peer
   ↓
2. Orchestrator merges, scores, stores in PeerStore
   ↓
3. CLI `peers` lists discovered peers
   ↓
4. User initiates connection (auto or manual)
   ↓
5. Link Manager selects best link (WiFi Direct > BLE > USB)
   ↓
6. QUIC dial → Noise XX handshake
   ↓
6. Service streams multiplexed over QUIC (ServiceID)
```

### File Transfer (Bitswap-like)
```
1. Sender: File → Chunk (1MB) → SHA3-256 CID → BlockStore
   ↓
2. Merkle DAG built → Root CID sent to receiver
   ↓
3. Receiver: Want-list (CIDs) → BitSwap protocol
   ↓
4. Blocks exchanged over QUIC streams (ServiceID: Files)
   ↓
5. Receiver verifies CID → Reassembles → FileStore
```

### Collaborative Editing (RGA)
```
1. Local edit → RGA insert/delete op
   ↓
2. Op broadcast over Messaging service (signed)
   ↓
3. Remote: Apply op → RGA merge (convergent)
   ↓
4. Presence/cursor broadcast → UI update
```

---

## 🔑 Key Invariants

| Invariant | Enforcement |
|-----------|-------------|
| **NodeID = SHA3-256(PubKey)** | Verified on every handshake |
| **No identity regeneration** | Persisted to `identity.json` |
| **Store encryption at rest** | AES-256-GCM, key from Ed25519 seed |
| **Audit log tamper-evident** | SHA3-256 hash chain, `VerifyIntegrity()` |
| **Capability tokens unforgeable** | Ed25519 signed, canonical JSON |
| **PoW difficulty adjusts** | Target ~1s solve time |
| **Audit log append-only** | No mutation, only append |
| **CRDT convergence** | Verified by test suites |

---

## 📊 Performance Targets

| Metric | Target |
|--------|--------|
| Handshake latency | < 100ms (LAN), < 500ms (Internet) |
| Stream throughput | > 100 Mbps (WiFi Direct) |
| Discovery time | < 5s (LAN), < 30s (Internet) |
| File sync (1GB) | < 2 min (WiFi Direct) |
| CRDT convergence | < 1s (LAN) |
| Memory (node) | < 100 MB |
| CPU (idle) | < 1% |

---

## 🔌 Extension Points

| Extension | Mechanism |
|-----------|-----------|
| New Service | Implement `Service` interface, register in node |
| New Link | Implement `Link` interface, add to Manager |
| New Discovery | Implement `DiscoveryMode`, add to Orchestrator |
| Plugin | Go plugin (`.so`) or BuiltinPlugin |
| QoS Class | Register with QoSManager |

---

## 🔒 Threat Model

| Threat | Mitigation |
|--------|------------|
| Eavesdropping | Noise XX + AES-GCM (all transport) |
| MITM | NodeID verification (SHA3-256 PubKey) |
| Replay | Nonce in Noise + PoW timestamp |
| Sybil | PoW + Capability tokens |
| Store tampering | AES-GCM + Audit log hash chain |
| PoW bypass | Difficulty auto-adjust (~1s target) |
| Token theft | Short TTL, revocation list |

---

*Generated from source code | LocalWEB v1.0.0 | Module: `github.com/ram1234598766-dotcom/Local-WEB`*
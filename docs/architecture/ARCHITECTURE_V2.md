# LocalWEB — Advanced Architecture v2

**A fully decentralized internet stack. Zero central authority. Offline-first. Real implementation.**

---

## 1. Design Philosophy

| Principle | Meaning |
|-----------|---------|
| **Offline-first** | Every service works without internet. Sync when connected. |
| **Real, not simulated** | Actual QUIC, actual DNS, actual SMTP. No toy protocols. |
| **Auto-discovery** | Laptops find each other on WiFi instantly. No manual config. |
| **All services simultaneously** | DNS, HTTP, email, messaging, files, docs, voice, VPN — all at once. |
| **Better than centralized** | Faster on LAN, works offline, censorship-resistant, encrypted by default. |

---

## 2. Architecture — 8 Layers

```
┌─────────────────────────────────────────────────────────────────────┐
│  Layer 7: APPLICATION                                               │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ Federated│ Real-time│ Voice/   │ App      │ Collaborative     │  │
│  │ Social   │ Docs     │ Video    │ Registry │ Dashboard/CLI     │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 6: SERVICES                                                  │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ DNS      │ HTTP/3   │ SMTP/    │ MQTT     │ WireGuard         │  │
│  │ .localweb│ Gateway  │ IMAP     │ Pub/Sub  │ Mesh VPN          │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 5: DATA / SYNC                                               │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ CRDT     │ Merkle   │ Content- │ Anti-    │ Encrypted         │  │
│  │ Engine   │ DAG      │ Addressed│ Entropy  │ Local Store       │  │
│  │ (OR-Set, │          │ Storage  │ Sync     │ (BadgerDB)        │  │
│  │  RGA,    │          │ (IPFS-   │          │                   │  │
│  │  LWW)    │          │  compat) │          │                   │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 4: SECURITY                                                  │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ Key      │ Noise    │ Capability│ Spam    │ Audit             │  │
│  │ Mgmt     │ Protocol │ Access   │ Resist  │ Logging           │  │
│  │ Hierarchy│ (XX)     │ Control  │ (PoW)   │                   │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 3: TRANSPORT                                                 │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ QUIC     │ Stream   │ Circuit  │ Hole     │ Flow Control      │  │
│  │ (RFC     │ Mux      │ Relay    │ Punching │ + Backpressure    │  │
│  │  9000)   │          │          │ (NAT)    │                   │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 2: ROUTING                                                   │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ Kademlia │ S/Kad    │ Peer     │ Storage  │ Iterative         │  │
│  │ DHT      │ Sybil    │ Scoring  │ Proofs   │ Lookup            │  │
│  │ (256-bit)│ Resist   │ (0→1.0)  │          │ α=3, k=20         │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 1: DISCOVERY                                                 │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ mDNS-SD  │ DNS-SD   │ ARP      │ SSDP     │ Bluetooth LE      │  │
│  │ (local)  │ (service │ Scan     │ (UPnP)   │ (proximity)       │  │
│  │          │  browse) │          │          │                   │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 0: PLATFORM / OS                                             │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ Network  │ File     │ Crypto   │ Process  │ FUSE/ Dokany      │  │
│  │ Stack    │ System   │ Primitives│ Mgmt   │ Mount             │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 3. Layer 0 — Platform Abstraction

```
pkg/platform/
├── network.go       # Cross-platform network interface detection
├── filesystem.go    # FUSE (Linux/macOS), Dokany (Windows), overlay mount
├── process.go       # Graceful shutdown, signal handling, watchdog
└── metrics.go       # CPU, memory, bandwidth, disk I/O per service
```

**Responsibilities:**
- Detect all active network interfaces (WiFi, Ethernet, VPN, loopback)
- Handle platform-specific multicast (mDNS on Linux vs macOS vs Windows)
- Manage FUSE/dokany mounts for virtual file systems
- Expose service metrics to the dashboard

**Key interfaces:**
```go
type Platform interface {
    ActiveInterfaces() ([]NetworkInterface, error)
    MountFUSE(root, mountpoint string) (io.Closer, error)
    Watchdog(service string) <-chan error
}
```

---

## 4. Layer 1 — Discovery

### 4.1 mDNS-SD (Primary — LAN)

**Protocol:** Multicast DNS Service Discovery (RFC 6762 + RFC 6763)
**Multicast:** `224.0.0.251:5353` (IPv4), `[ff02::fb]:5353` (IPv6)
**Service Type:** `_localweb._tcp.local`

**Discovery Sequence:**
```
Node A powers on
  → Constructs mDNS query: "ANY _localweb._tcp.local"
  → Multicasts to 224.0.0.251:5353
  → All LocalWEB nodes on subnet respond with:
      - Node ID (SHA3-256 of pubkey)
      - QUIC address (IP:port)
      - Capabilities (dns, http, smtp, mqtt, ...)
      - Latency hint
      - Version
  → Node A adds all responders to routing table
  → Node A sends announce with its own capabilities
  → Convergence: <500ms on same subnet

Periodic:
  → Announce every 30s
  → Probing for address conflicts
  → TTL-based expiry: miss 3 announces → evict
```

**Packet format (DNS wire format):**
```
Header:
  ID: random
  QR=1, OPCODE=0, AA=1, TC=0, RD=0, RA=0
  QDCOUNT=0, ANCOUNT=1+

Answer Section:
  NAME: _localweb._tcp.local
  TYPE: SRV (33)
  CLASS: cache-flush-internet
  TTL: 120
  RDATA: priority=0, weight=0, port=4443, target=<hostname>.local

Additional Section:
  NAME: <hostname>.local
  TYPE: A (1) / AAAA (28)
  RDATA: <ip address>

  NAME: <hostname>.local
  TYPE: TXT (16)
  RDATA: "id=<hex_peer_id>" "svc=dns,http,smtp,mqtt" "ver=0.1.0"
```

### 4.2 DNS-SD Browse

```go
type DiscoveryService interface {
    // Advertise announces this node's services
    Advertise(services []ServiceRecord) error

    // Browse finds all LocalWEB nodes
    Browse(ctx context.Context) ([]PeerInfo, error)

    // Subscribe emits events when peers join/leave
    Subscribe(ctx context.Context) <-chan PeerEvent

    // Resolve looks up a specific node
    Resolve(nodeID PeerID) (*PeerInfo, error)
}

type PeerEvent struct {
    Type  EventType // PeerJoin, PeerLeave, PeerUpdate
    Peer  PeerInfo
    Time  time.Time
}

type ServiceRecord struct {
    Name   string
    Type   string
    Port   int
    TXT    map[string]string
}
```

### 4.3 ARP Scan (Fallback)

For networks where multicast is blocked:
```go
// Send ARP requests to all IPs in subnet
// Parse responses for MAC + IP
// Probe each for QUIC on port 4443
func arScanSubnet(iface net.Interface) ([]PeerInfo, error)
```

### 4.4 SSDP/UPnP (NAT Discovery)

```go
// M-SEARCH for UPnP devices
// Parse location headers
// Discover router for port mapping
func discoverUPnPRouter() (*net.UDPAddr, error)
func addPortMapping(port int) error
```

### 4.5 Bluetooth LE (Proximity)

For nearby devices without WiFi:
```go
// Advertise LocalWEB service UUID
// Scan for matching advertisements
// Exchange QUIC addresses via GATT characteristic
// Fallback: mesh through intermediate nodes
```

---

## 5. Layer 2 — DHT (Distributed Hash Table)

### 5.1 Kademlia with S/Kademlia Hardening

```
Key Space:     256-bit (SHA3-256 of Ed25519 public key)
Routing:       256 k-buckets, k=20 peers per bucket
Lookup:        α=3 parallel queries
               500ms timeout per round
               15 max hops
               Converges in O(log n) hops
Storage:       Replicate to k=20 closest peers
Refresh:       Random lookup every 3600s
Bucket Refresh: Ping stale entries, evict dead after 3 misses
```

### 5.2 Operations

| Operation | RPC | Description |
|-----------|-----|-------------|
| `PING` | `MsgPing` → `MsgPong` | Liveness check, latency measurement |
| `FIND_NODE(id)` | `MsgFindNode` → `MsgFoundNode` | Return k closest peers |
| `FIND_VALUE(key)` | `MsgFindValue` → `MsgFoundValue` | Iterative lookup + cache |
| `STORE(key, val)` | `MsgStore` | Replicate to k closest |
| `REFRESH` | — | Periodic random lookups |
| `NOTIFY` | `MsgAnnounce` | Tell peers about new node |

### 5.3 S/Kademlia Sybil Resistance

```
Pre-Layer: Unidirectional computational puzzle
  → Before joining, solve hash puzzle: SHA3(seed || nodeID) < difficulty
  → Difficulty adjusts to maintain network size
  → Cost: ~1s CPU per join attempt

Bucket Isolation:
  → Each k-bucket only accepts peers with different prefix lengths
  → Limits Sybil nodes to a single bucket
  → Self-ID XOR prefix → isolate by network topology

Peer Scoring (0.0 → 1.0):
  + Response rate to DHT queries     (weight: 0.3)
  + Uptime fraction                  (weight: 0.2)
  + Content validity                 (weight: 0.2)
  + Storage proof correctness        (weight: 0.15)
  + Relay reliability                (weight: 0.15)

  Score < 0.2: bucket eviction, query exclusion
  Score > 0.8: preferential routing, replication target
```

### 5.4 Storage Proofs

```go
// Proof of Storage: peer proves it holds data
type StorageProof struct {
    Key       PeerID
    ValueHash [32]byte
    Challenge []byte  // random challenge
    Response  []byte  // HMAC(key, challenge)
    Timestamp int64
}

// Verify: check HMAC matches, timestamp is fresh
func VerifyStorageProof(proof StorageProof, key []byte) bool
```

---

## 6. Layer 3 — Transport

### 6.1 QUIC (Primary Transport)

```
Protocol:      QUIC RFC 9000 over UDP
Handshake:     Noise Protocol Framework (XX pattern)
  → Mutual auth via Ed25519 static keys
  → Forward secrecy via X25519 ephemeral keys
  → Session keys: HKDF(key_material, "LocalWEB v1", 32)
  → Encrypted with ChaCha20-Poly1305 (via QUIC TLS 1.3)

Stream Multiplexing:
  Stream 0: Control (heartbeats, node state, peer exchange)
  Stream 1: DNS over QUIC (DoQ)
  Stream 2: HTTP/3
  Stream 3: Messaging (protobuf)
  Stream 4: File transfer (fragmented blocks)
  Stream 5: SMTP
  Stream 6: MQTT
  Stream 7: WireGuard mesh
  Stream 8: Voice/Video (RTP-like)
  Stream 9+: Dynamic allocation

Flow Control:
  → Per-stream and connection-level windows
  → Dynamic window sizing based on RTT
  → Backpressure: pause streams on memory pressure

0-RTT Resume:
  → Cache crypto params from previous connection
  → Replay protection: single-use tokens + timestamps
```

### 6.2 Connection Flow

```
Client                          Server
  |                               |
  |--- Initial (clientHello) ---->|  ← Noise XX pattern
  |<-- ServerHello + Auth --------|  ← Ed25519 signature
  |--- Finish + Auth ------------>|  ← Mutual proof
  |                               |
  |  [Encrypted QUIC streams]     |
  |                               |
  |--- Stream 0: PING ----------->|  ← Control
  |<-- Stream 0: PONG + Peers ----|  ← Peer exchange
  |                               |
  |--- Stream 1: DNS Query ------>|  ← DoQ
  |<-- Stream 1: DNS Response ----|  ← .localweb resolution
```

### 6.3 NAT Traversal

```
Strategy: UDP hole punching + relay fallback

Direct connection:
  1. Node A learns Node B's public IP:port from DHT
  2. Both send UDP packets to each other simultaneously
  3. NAT creates mapping → bidirectional flow established
  4. QUIC handshake over hole-punched path

Relay fallback (Circuit Relay v2):
  1. If direct fails, use relay node R
  2. A → R → B (double encrypted)
  3. Relay sees only encrypted bytes
  4. Relay capacity limited to prevent abuse

Hole Punching Helper:
  → STUN-like: Node queries known peer for its observed address
  → Returns: {public_ip, public_port, nat_type}
```

### 6.4 Wire Protocol

```go
// Frame is the atomic unit on any QUIC stream
type Frame struct {
    Type      uint8     // Message type
    Flags     uint8     // Compressed, priority, etc.
    Length    uint32    // Payload length (big-endian)
    Payload   []byte    // Protobuf-encoded
    Checksum  [4]byte   // CRC32C
}

// Connection is a QUIC connection with service routing
type Connection struct {
    conn        quic.Connection
    streams     map[uint8]quic.Stream
    mux         *StreamMux
    crypto      *NoiseSession
    peer        *PeerInfo
    sendQueue   chan *Frame
    recvQueues  map[uint8]chan *Frame
}
```

---

## 7. Layer 4 — Security

### 7.1 Key Hierarchy

```
Master Key (Ed25519)
  ├── Node Identity Key (persistent, stored encrypted)
  │     ├── Signing Key (for messages, blocks, DHT)
  │     ├── Encryption Key (derived via HKDF)
  │     └── Auth Key (Noise static key)
  │
  ├── Session Keys (ephemeral, per-connection)
  │     ├── Noise ephemeral (X25519)
  │     └── Derived via HKDF from handshake
  │
  └── User Keys (optional, for multi-user)
        ├── Per-user Ed25519 keypair
        └── Capability tokens (signed delegations)
```

### 7.2 Noise Protocol (XX Pattern)

```
→ e                    // Client sends ephemeral pubkey
← e, ee, s, es        // Server sends ephemeral + static + proofs
→ s, se               // Client sends static + proof

Result: 256-bit shared secret
  → HKDF expansion:
    enc_send = HKDF("LocalWEB-enc-send", shared_secret)
    enc_recv = HKDF("LocalWEB-enc-recv", shared_secret)
    mac_send = HKDF("LocalWEB-mac-send", shared_secret)
    mac_recv = HKDF("LocalWEB-mac-recv", shared_secret)
```

### 7.3 Capability-Based Access Control

```go
type Capability struct {
    Resource   string    // "/files/abc123", "/channel/general"
    Actions    []string  // ["read", "write", "delete"]
    GrantedTo  [32]byte  // Recipient's public key
    GrantedBy  [32]byte  // Granter's public key
    ExpiresAt  int64     // Unix timestamp (0 = never)
    Nonce      [16]byte  // Prevent replay
    Signature  [64]byte  // Signed by granter
}
```

### 7.4 Spam Prevention (Proof of Work)

```
To send message or store data:
  → Compute: SHA3(nonce || sender_id || payload_hash) < difficulty
  → Difficulty: 20-bit (≈1s on modern CPU)
  → Rate limit: max 10 messages/second per peer
  → Abuse: dynamic difficulty increase per peer
```

---

## 8. Layer 5 — Data / Sync

### 8.1 CRDT Types

| Data Type | CRDT | Use Case | Merge Rule |
|-----------|------|----------|------------|
| DNS records | OR-Set | Add/remove records | Union of add-sets minus remove-sets |
| File metadata | LWW-Element-Set | File tree | Latest timestamp wins |
| Chat messages | RGA | Chat history | Prepend/insert with causal ordering |
| User presence | LWW-Register | Online status | Latest timestamp wins |
| Channel membership | OR-Set | Group membership | Union of add-sets minus remove-sets |
| Config overrides | LWW-Register | Settings | Latest timestamp wins |
| Application state | Delta-CRDT | Collaborative edits | Per-field merge |

### 8.2 Merkle DAG (Content-Addressed Storage)

```
Structure:
  Each block → SHA3-256 hash → CID (content identifier)
  Files → tree of blocks → Merkle root
  Directories → Merkle tree of file roots

Properties:
  Immutable: change content → new CID
  Deduplicated: same content → same CID globally
  Verifiable: Merkle proof from root to any block
  Dedup: global CID-based deduplication
```

### 8.3 Encrypted Local Store (BadgerDB)

```go
type LocalStore struct {
    db         *badger.DB
    encryptKey [32]byte     // Derived from node identity
    gcTicker   *time.Ticker // Periodic garbage collection
}

// Storage layout:
//   key: CID → value: encrypted(block_data)
//   key: meta:<CID> → value: encrypted(file_metadata)
//   key: crdt:<domain>:<key> → value: encrypted(crdt_state)
//   key: cap:<id> → value: encrypted(capability)
//   key: peer:<id> → value: encrypted(peer_info)
```

### 8.4 Anti-Entropy Sync

```
On connect to peer:
  1. Exchange Merkle roots for each CRDT domain
  2. Diff: find branches where hashes differ
  3. Request missing branches (delta, not full state)
  4. Merge using CRDT rules (commutative, idempotent, associative)
  5. Update local Merkle root

Triggers:
  → On new peer connection
  → Periodic every 300s (background)
  → On local mutation (push delta to connected peers)
  → On reconnect after offline (catch-up sync)
```

---

## 9. Layer 6 — Services

### 9.1 DNS Service (`.localweb` TLD)

```
Features:
  → Full RFC 1035 compliant parser/serializer
  → Authoritative per-node: each node owns *.localweb
  → Record types: A, AAAA, TXT, SRV, CNAME, PTR, HTTPS, SVCB
  → Caching with negative TTL
  → Zone transfers (AXFR) between peers
  → DNSSEC-like signing with Ed25519

Resolution chain:
  Local cache hit → mDNS query (1ms) → DHT lookup (50-200ms) → cached (0ms)

Port: 5353 (standard mDNS) + 5354 (fallback)
Transport: UDP + TCP + DoQ (DNS over QUIC)
```

**Implementation:**
```go
type DNSServer struct {
    zones    map[string]*Zone        // Local zones
    cache    *DNSCache               // TTL-based cache
    upstream []string                // Fallback resolvers
    signing  *crypto.Signer          // DNSSEC signing
    transport net.PacketConn
}

type Zone struct {
    Records  map[string][]*Record
    SOA      *SOA
    SignedAt time.Time
    Sig      [64]byte
}
```

### 9.2 HTTP/3 Gateway

```
Features:
  → Static file serving from ~/LocalWEB/sites/<domain>/
  → Reverse proxy to backend services (configurable)
  → WebSocket upgrade over QUIC streams
  → Server-Sent Events for real-time push
  → Range requests for video/audio streaming
  → Content-addressable cache (/ipfs/<cid>)
  → Automatic Let's Encrypt-style certs for .localweb
  → CORS headers
  → Gzip/Brotli compression
  → Rate limiting per client

Port: 8080 (HTTP) + 8443 (HTTPS/QUIC)
Transport: HTTP/3 over QUIC
```

**Implementation:**
```go
type HTTPServer struct {
    handler   *http.ServeMux
    sites     map[string]*Site       // domain → file root
    proxy     map[string]*ProxyRule  // domain → backend
    certCache *CertCache             // Auto-generated TLS certs
    store     *ContentStore          // /ipfs/ cache
}

type ProxyRule struct {
    Target    string   // "http://localhost:9000"
    Headers   map[string]string
    WebSocket bool
    Timeout   time.Duration
}
```

### 9.3 Email (SMTP/IMAP)

```
Features:
  → Full SMTP submission (port 587) with AUTH
  → SMTP delivery (port 25) between nodes
  → IMAP4rev2 access (port 993) with TLS
  → Local mailbox storage (mbox or Maildir)
  → Address format: user@nodename.localweb
  → Inter-node delivery via QUIC
  → Anti-spam: PoW + reputation
  → Mailing lists: subscribe@listname.localweb

Delivery path:
  Sender → Sender's SMTP → DHT lookup recipient node → QUIC → Recipient's SMTP → Mailbox
```

**Implementation:**
```go
type SMTPServer struct {
    listener  net.Listener
    store     *MailStore
    resolver  *DNSResolver
    transport *QUICTransport
    antispam  *SpamFilter
}

type IMAPServer struct {
    listener net.Listener
    store    *MailStore
    auth     *AuthProvider
}

type MailStore struct {
    baseDir  string           // ~/LocalWEB/mail/<username>/
    inbox    *Maildir
    sent     *Maildir
    drafts   *Maildir
}
```

### 9.4 Messaging (E2E Encrypted)

```
Features:
  → End-to-end encrypted: X3DH key agreement + Double Ratchet
  → Channels: open, invite_only, admin_only
  → Direct messages: 1:1 encrypted
  → Group messaging: sender keys
  → Media: images, files, voice notes (via content-addressed storage)
  → Reactions, replies, threads
  → Offline: messages queued in outbox, delivered on reconnect
  → Delivery receipts: sent, delivered, read
  → Message ordering: vector clocks for causal ordering

Port: 9090 (WebSocket) + QUIC stream 3
Transport: Protobuf over QUIC + WebSocket fallback
```

**Implementation:**
```go
type MessagingServer struct {
    channels   map[string]*Channel
    store      *MessageStore
    transport  *QUICTransport
    ratchet    *RatchetManager
    presence   *PresenceTracker
}

type MessageStore struct {
    db         *badger.DB
    crdt       *CRDTStore        // RGA for message ordering
    index      *IndexStore       // Full-text search
}
```

### 9.5 File Sync

```
Features:
  → Content-addressed block storage (4MB blocks)
  → Zstd compression on blocks > 4KB
  → Global deduplication via CID
  → Merkle DAG diff — only transfer changed blocks
  → FUSE mount (Linux/macOS), Dokany (Windows)
  → ACL by Ed25519 public key, optional expiry
  → File versioning: keep N historical versions
  → Partial sync: download specific files/dirs
  → Background sync: push/pull on timer
  → Conflict resolution: LWW-Element-Set CRDT

Sync protocol:
  1. Build Merkle DAG of file tree
  2. Send root CID to recipient
  3. Recipient compares with local version
  4. Request missing/changed blocks (bitswap-like)
  5. Blocks stored and deduplicated globally
```

**Implementation:**
```go
type FileSyncService struct {
    store     *BlockStore
    dag       *MerkleDAG
    sync      *SyncEngine
    fuse      *FUSEMount
    shares    *ShareManager
}

type BlockStore struct {
    baseDir   string           // ~/LocalWEB/blocks/
    db        *badger.DB       // CID → metadata
    index     *IndexStore      // filename → CID index
}

type SyncEngine struct {
    peers     map[PeerID]*PeerSync
    interval  time.Duration
    crdt      *LWWSet          // File metadata CRDT
}
```

### 9.6 Collaborative Documents (CRDT)

```
Features:
  → Real-time collaborative editing
  → CRDT-based: no central server, no conflicts
  → Rich text: paragraphs, headings, lists, code blocks
  → Presence: see who's editing what
  → History: full edit history with undo/redo
  → Export: Markdown, HTML, PDF
  → Offline: edits queued, merged on reconnect
  → Comments and annotations

Protocol:
  → Per-document CRDT state (RGA for text)
  → Operations: insert(char, position), delete(position)
  → Broadcast operations to all connected editors
  → Periodic state sync for new joiners
```

**Implementation:**
```go
type DocService struct {
    docs     map[string]*Document
    store    *DocStore
    crdt     *RGACRDT
    presence *PresenceTracker
}

type Document struct {
    ID       string
    Title    string
    Content  *RGA          // Collaborative text
    Metadata *LWWRegister // Title, settings
    History  []Operation   // Full edit log
    Authors  map[PeerID]*AuthorPresence
}
```

### 9.7 Voice/Video (WebRTC-like)

```
Features:
  → 1:1 and group calls
  → Audio: Opus codec (48kHz, 32kbps)
  → Video: VP9/AV1 codec
  → Screen sharing
  → Data channels over QUIC
  → ICE-like candidate gathering (local + STUN + relay)
  → Adaptive bitrate based on network
  → Echo cancellation, noise suppression

Protocol:
  → Signaling via messaging channels
  → Media via QUIC streams (not SRTP — simpler)
  → FEC (Forward Error Correction) for loss resilience
```

**Implementation:**
```go
type CallService struct {
    sessions  map[string]*CallSession
    codec     *CodecManager
    signaling *MessagingService
}

type CallSession struct {
    ID        string
    Peers     map[PeerID]*PeerMedia
    Audio     *AudioTrack
    Video     *VideoTrack
    DataChan  quic.Stream
}
```

### 9.8 Mesh VPN (WireGuard-compatible)

```
Features:
  → WireGuard-compatible protocol
  → Virtual interface: tun0 on each node
  → Encrypted tunnel between any two nodes
  → Routing: each node advertises its IP range
  → NAT traversal: relay through peers
  → Split tunneling: route only LocalWEB traffic

Addressing:
  → Each node gets fd00:localweb:<id>/128 (ULA IPv6)
  → IPv4: 10.0.0.0/8 range allocated by DHT
```

**Implementation:**
```go
type MeshVPN struct {
    tun       *TUNDevice
    peers     map[PeerID]*Tunnel
    routes    *RoutingTable
    wireguard *WireGuardImpl
}

type Tunnel struct {
    PeerID    PeerID
    Endpoint  *net.UDPAddr
    PublicKey [32]byte
    AllowedIPs []net.IPNet
    Keepalive int
}
```

### 9.9 App Registry / Package Store

```
Features:
  → Local package registry
  → Sign packages with Ed25519
  → Install/update/remove
  → Dependencies resolved via DHT
  → Binary distribution (cross-compiled)
  → Web-based package browser (served via HTTP)

Format:
  → .lwpkg (LocalWEB Package): tar.gz + signature
  → manifest.yaml: name, version, deps, binaries, checksums
```

---

## 10. Layer 7 — Application

### 10.1 Federated Social Feed

```
→ Each node hosts its own profile
→ Follow/subscribe via capability tokens
→ Posts stored as CRDTs (OR-Set + LWW)
→ Feed assembled from followed nodes
→ No algorithm — chronological only
→ Media via content-addressed storage
```

### 10.2 Dashboard / CLI

```
CLI:
  localweb status          # Node status, peers, services
  localweb peers list      # Known peers
  localweb dns query <name>
  localweb http serve <dir>
  localweb mail send <to> <subject>
  localweb msg send <channel> <text>
  localweb files sync       # Trigger file sync
  localweb docs new         # Create collaborative doc
  localweb call <peer>      # Start voice/video call
  localweb vpn status       # Mesh VPN status

Web Dashboard (served on HTTP port):
  → Real-time peer map
  → Service status
  → Message inbox
  → File browser
  → Doc editor
  → Call management
  → Configuration
```

---

## 11. Complete Project Structure

```
LocalWEB/
├── cmd/
│   ├── node/main.go                 # Entry point
│   └── cli/main.go                  # CLI entry point
│
├── internal/
│   ├── config/config.go             # Viper config loader
│   ├── identity/identity.go         # Ed25519 keypair management
│   ├── logging/logging.go           # Zerolog initialization
│   └── node/node.go                 # Service orchestrator
│
├── pkg/
│   ├── crypto/
│   │   ├── crypto.go                # Ed25519, SHA3, secretbox, HKDF
│   │   ├── noise.go                 # Noise XX handshake
│   │   ├── x3dh.go                  # X3DH key agreement
│   │   └── ratchet.go               # Double ratchet
│   │
│   ├── dht/
│   │   ├── dht.go                   # Kademlia DHT core
│   │   ├── routing.go               # K-bucket routing table
│   │   ├── lookup.go                # Iterative lookup
│   │   ├── storage.go               # DHT storage + proofs
│   │   └── sybil.go                 # S/Kademlia anti-Sybil
│   │
│   ├── discovery/
│   │   ├── discovery.go             # Discovery service orchestrator
│   │   ├── mdns.go                  # mDNS-SD advertiser/browser
│   │   ├── dns_sd.go                # DNS-SD service browsing
│   │   ├── arp.go                   # ARP scan fallback
│   │   ├── ssdp.go                  # SSDP/UPnP discovery
│   │   └── ble.go                   # Bluetooth LE proximity
│   │
│   ├── transport/
│   │   ├── quic.go                  # QUIC server + client
│   │   ├── stream.go                # Stream multiplexer
│   │   ├── relay.go                 # Circuit relay
│   │   ├── nat.go                   # NAT traversal / hole punching
│   │   └── connection.go            # Connection management
│   │
│   ├── protocol/
│   │   ├── protocol.go              # Wire frames, message types
│   │   ├── encoding.go              # Protobuf codecs
│   │   └── framing.go               # Frame encode/decode
│   │
│   ├── security/
│   │   ├── capability.go            # Capability-based access control
│   │   ├── spam.go                  # PoW spam prevention
│   │   ├── audit.go                 # Audit logging
│   │   └── cert.go                  # Auto-generated TLS certs
│   │
│   ├── sync/
│   │   ├── crdt.go                  # CRDT implementations
│   │   │   ├── orset.go             # OR-Set
│   │   │   ├── lww.go               # LWW-Register + LWW-Element-Set
│   │   │   ├── rga.go               # RGA (text)
│   │   │   └── vector_clock.go      # Vector clock
│   │   ├── merkle.go                # Merkle DAG
│   │   ├── anti_entropy.go          # Anti-entropy sync protocol
│   │   └── store.go                 # Encrypted BadgerDB store
│   │
│   ├── services/
│   │   ├── dns/
│   │   │   ├── server.go            # DNS server (RFC 1035)
│   │   │   ├── parser.go            # DNS message parser/serializer
│   │   │   ├── cache.go             # DNS cache with TTL
│   │   │   └── zone.go              # Zone management
│   │   │
│   │   ├── http/
│   │   │   ├── server.go            # HTTP/3 gateway
│   │   │   ├── static.go            # Static file serving
│   │   │   ├── proxy.go             # Reverse proxy
│   │   │   ├── websocket.go         # WebSocket over QUIC
│   │   │   └── cert.go              # Auto TLS certs
│   │   │
│   │   ├── email/
│   │   │   ├── smtp.go              # SMTP server
│   │   │   ├── imap.go              # IMAP server
│   │   │   ├── mailbox.go           # Mail storage
│   │   │   └── antispam.go          # Spam filter
│   │   │
│   │   ├── messaging/
│   │   │   ├── server.go            # Messaging server
│   │   │   ├── channel.go           # Channel management
│   │   │   ├── store.go             # Message store
│   │   │   └── types.go             # Message/Channel types
│   │   │
│   │   ├── files/
│   │   │   ├── files.go             # Content-addressed utilities
│   │   │   ├── server.go            # File store + transfer
│   │   │   ├── sync.go              # File sync engine
│   │   │   └── fuse.go              # FUSE/Dokany mount
│   │   │
│   │   ├── docs/
│   │   │   ├── server.go            # Collaborative doc server
│   │   │   ├── rga.go               # RGA CRDT for text
│   │   │   └── presence.go          # Editor presence
│   │   │
│   │   ├── voice/
│   │   │   ├── server.go            # Voice/video server
│   │   │   ├── codec.go             # Opus/VP9 wrapper
│   │   │   └── signaling.go         # Call signaling
│   │   │
│   │   ├── vpn/
│   │   │   ├── server.go            # WireGuard mesh VPN
│   │   │   ├── tunnel.go            # Tunnel management
│   │   │   └── routing.go           # IP routing
│   │   │
│   │   └── registry/
│   │       ├── server.go            # Package registry
│   │       ├── package.go           # Package format
│   │       └── verify.go            # Signature verification
│   │
│   ├── platform/
│   │   ├── network.go               # Network interface detection
│   │   ├── filesystem.go            # FUSE/dokany abstraction
│   │   └── metrics.go               # System metrics
│   │
│   └── types/
│       └── types.go                 # All core types
│
├── api/
│   └── proto/
│       ├── messages.proto           # Protobuf definitions
│       └── generated/               # Generated Go code
│
├── configs/
│   └── config.yaml                  # Default configuration
│
├── docs/
│   └── architecture/
│       ├── ARCHITECTURE_V2.md       # This file
│       ├── SERVICES.md              # Service specifications
│       └── PROTOCOLS.md             # Wire protocol details
│
├── test/
│   ├── integration/                 # Integration tests
│   ├── unit/                        # Unit tests
│   └── bench/                       # Benchmark tests
│
├── scripts/
│   ├── build.sh                     # Build script
│   ├── cross-compile.sh             # Cross-compilation
│   └── generate.sh                  # Proto generation
│
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

---

## 12. Go Module Dependencies

```go
module github.com/mrityunjayjha/LocalWEB

go 1.23

require (
    // Transport
    github.com/quic-go/quic-go v0.45.0

    // Config
    github.com/spf13/cobra v1.8.1
    github.com/spf13/viper v1.19.0

    // Serialization
    google.golang.org/protobuf v1.35.1

    // Content addressing
    github.com/ipfs/go-cid v0.4.1

    // Compression
    github.com/klauspost/compress v1.17.9

    // Logging
    github.com/rs/zerolog v1.33.0

    // Time sync
    github.com/beevik/ntp v1.3.1

    // Database
    github.com/dgraph-io/badger/v3 v3.2103.5

    // mDNS
    github.com/grandcat/zeroconf v1.0.0

    // DNS
    github.com/miekg/dns v1.1.58

    // FUSE
    github.com/hanwen/go-fuse v1.0.0

    // Crypto
    golang.org/x/crypto v0.28.0

    // WireGuard (userspace)
    golang.zx2c4.com/wireguard v0.0.0-20231211153847-12269c276173

    // Voice codec
    github.com/nicknack/opus v0.0.0-20231123234113-a9c0ba12a03c
)
```

---

## 13. Interface Contracts

### Service Interface (Unchanged — Enhanced)

```go
type Service interface {
    Name() string
    Start() error
    Stop() error

    // New capabilities:
    Status() ServiceStatus
    Metrics() ServiceMetrics
}

type ServiceStatus struct {
    Running   bool
    Peers     int
    Uptime    time.Duration
    LastError error
}

type ServiceMetrics struct {
    BytesIn    uint64
    BytesOut   uint64
    Requests   uint64
    Errors     uint64
    AvgLatency time.Duration
}
```

### Transport Interface

```go
type Transport interface {
    Listen(addr string) error
    Connect(addr string) (*Connection, error)
    RegisterService(id string, handler StreamHandler)
    Connections() []*Connection
    Stop() error
}
```

### Discovery Interface

```go
type Discovery interface {
    Advertise(services []ServiceRecord) error
    Browse(ctx context.Context) ([]PeerInfo, error)
    Subscribe(ctx context.Context) <-chan PeerEvent
    Resolve(id PeerID) (*PeerInfo, error)
    Stop() error
}
```

### Store Interface

```go
type Store interface {
    Get(key []byte) ([]byte, error)
    Put(key, value []byte) error
    Delete(key []byte) error
    Has(key []byte) (bool, error)
    List(prefix []byte) ([][]byte, error)
    Close() error
}
```

### CRDT Interface

```go
type CRDT interface {
    // Apply a local operation
    Apply(op Operation) error

    // Merge state from another peer
    Merge(state []byte) error

    // Export current state
    Export() ([]byte, error)

    // Query current value
    Value() interface{}
}
```

---

## 14. Configuration (Enhanced)

```yaml
node:
  name: "my-laptop"
  data_dir: "./data"
  listen_addr: "0.0.0.0"
  max_storage: 10737418240    # 10GB

discovery:
  enabled: true
  interval: "30s"
  mdns_enabled: true
  arp_scan: true              # Fallback discovery
  ssdp_enabled: true          # UPnP discovery
  ble_enabled: false          # Bluetooth (power saving)

dht:
  enabled: true
  bucket_size: 20
  alpha: 3
  max_hops: 15
  refresh_interval: "1h"
  sybil_difficulty: 20        # Bits for PoW join
  bootstrap_peers: []

transport:
  quic_port: 4443
  noise_enabled: true
  relay_enabled: true
  max_connections: 1000
  keepalive: "30s"

security:
  tls_min_version: "1.3"
  auth_required: false
  pow_difficulty: 20          # Spam prevention
  capability_enabled: true

services:
  dns:
    enabled: true
    port: 5353
    tld: ".localweb"
    cache_ttl: "5m"
    zone_transfer: true

  http:
    enabled: true
    port: 8080
    quic_port: 8443
    sites_dir: "./data/sites"
    compression: true
    websocket: true

  email:
    enabled: true
    smtp_port: 587
    imap_port: 993
    mailbox_dir: "./data/mail"

  messaging:
    enabled: true
    port: 9090
    e2e_enabled: true
    offline_queue: true

  files:
    enabled: true
    block_size: 4194304       # 4MB
    compression: "zstd"
    fuse_mount: "./mount"
    sync_interval: "60s"

  docs:
    enabled: true
    port: 9091

  voice:
    enabled: true
    audio_codec: "opus"
    video_codec: "vp9"

  vpn:
    enabled: false            # Opt-in
    interface: "tun0"
    mtu: 1420

  registry:
    enabled: true
    port: 9092

logging:
  level: "info"
  format: "json"
  file: "./data/logs/localweb.log"
```

---

## 15. Performance Targets

| Metric | Target | Notes |
|--------|--------|-------|
| mDNS discovery | <500ms | Same subnet |
| DHT lookup | <200ms | 3 hops average |
| QUIC 0-RTT connect | <50ms | Cached params |
| HTTP first byte | <100ms | Local files |
| DNS resolution | <50ms | Cached |
| Message delivery | <50ms | Same LAN |
| File sync (1GB) | 30s | 1Gbps LAN |
| Email delivery | <2s | Local node |
| Voice latency | <100ms | Same LAN |
| Node startup | <2s | Full services |
| Memory idle | <100MB | All services |
| Memory loaded | <500MB | Active use |
| CPU idle | <1% | Background |
| Concurrent peers | 1000+ | Per node |
| Storage per block | 4MB | Content-addressed |

---

*Last updated: 2026-09-04*
*Author: Mrityunjay K*

# LocalWEB — Advanced Architecture v3

**Real working internet stack. Zero infrastructure required. Better than centralized.**

---

## Core Principle

```
Mode 1: NO WiFi (zero infrastructure)
  → BLE advertising + scanning
  → WiFi Direct (peer-to-peer)
  → Ad-hoc WiFi network
  → USB data link
  → Acoustic coupling (audio FSK)
  → Serial/UART over USB

Mode 2: WITH WiFi (standard networking)
  → mDNS-SD (subnet discovery)
  → ARP scan (subnet sweep)
  → SSDP/UPnP (NAT traversal)
  → Internet relay (cross-subnet)

Mode 3: HYBRID (best of both)
  → BLE discovers peer → exchanges WiFi Direct credentials → high-bandwidth transfer
  → mDNS fails → BLE fallback → escalate to direct connection
```

---

## 9-Layer Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│  Layer 8: APPLICATION                                               │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ Federated│ Real-time│ Voice/   │ App      │ Dashboard         │  │
│  │ Social   │ Docs     │ Video    │ Registry │ (Web + CLI)       │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 7: SERVICES                                                  │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ DNS      │ HTTP/3   │ SMTP/    │ MQTT     │ WireGuard         │  │
│  │ .localweb│ Gateway  │ IMAP     │ Pub/Sub  │ Mesh VPN          │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 6: DATA / SYNC                                               │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ CRDT     │ Merkle   │ Content- │ Anti-    │ Encrypted         │  │
│  │ Engine   │ DAG      │ Addressed│ Entropy  │ Local Store       │  │
│  │ (OR-Set, │          │ Storage  │ Sync     │ (BadgerDB)        │  │
│  │  RGA,    │          │          │          │                   │  │
│  │  LWW)    │          │          │          │                   │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 5: SECURITY                                                  │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ Key      │ Noise    │ Capability│ Spam    │ Audit             │  │
│  │ Mgmt     │ Protocol │ Access   │ Resist  │ Logging           │  │
│  │ Hierarchy│ (XX)     │ Control  │ (PoW)   │                   │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 4: TRANSPORT                                                 │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ QUIC     │ Stream   │ Circuit  │ Hole     │ Flow Control      │  │
│  │ (RFC     │ Mux      │ Relay    │ Punching │ + Backpressure    │  │
│  │  9000)   │          │          │ (NAT)    │                   │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 3: ROUTING                                                   │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ Kademlia │ S/Kad    │ Peer     │ Storage  │ Iterative         │  │
│  │ DHT      │ Sybil    │ Scoring  │ Proofs   │ Lookup            │  │
│  │ (256-bit)│ Resist   │          │          │                   │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 2: DISCOVERY                                                 │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ mDNS-SD  │ BLE      │ WiFi     │ ARP      │ SSDP              │  │
│  │ (WiFi)   │ (No WiFi)│ Direct   │ Scan     │ (NAT)             │  │
│  │          │          │ (No WiFi)│          │                   │  │
│  └──────────┴──────────┴──────────┴──────────┴───────────────────┘  │
├─────────────────────────────────────────────────────────────────────┤
│  Layer 1: ADAPTIVE LINK                                             │
│  ┌──────────┬──────────┬──────────┬──────────┬───────────────────┐  │
│  │ WiFi     │ WiFi     │ Ad-hoc   │ USB      │ Acoustic          │  │
│  │ Station  │ Direct   │ WiFi     │ Tether   │ Coupling          │  │
│  │          │          │          │          │ (FSK modem)       │  │
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

## Layer 1: Adaptive Link (The Innovation)

This is what makes LocalWEB work without WiFi. The Adaptive Link layer detects what connectivity is available and uses the best option.

### 1.1 WiFi Direct (No Router Required)

Two laptops connect peer-to-peer without any WiFi router.

```
Discovery:
  → Node A sends WiFi Direct service discovery probe
  → Node B (also WiFi Direct capable) responds
  → Group Owner negotiation (WPS-like)
  → One node becomes GO, other joins
  → IP assigned via DHCP (GO is DHCP server)
  → QUIC connection established over WiFi Direct interface

Go library: Use OS WiFi Direct APIs
  Linux:  wpa_supplicant P2P commands
  macOS:  CoreWLAN (limited P2P support)
  Windows: Wi-Fi Direct APIs (Windows.Devices.WiFiDirect)
```

**Implementation:**
```go
type WiFiDirect struct {
    iface       string          // p2p0 or wlan0
    groupOwner bool
    peerMAC     net.HardwareAddr
    conn        net.Conn        // TCP/UDP over WiFi Direct
    wpaCtrl     *WPAControl     // wpa_supplicant control interface
}

// Discover sends WiFi Direct service discovery query
func (w *WiFiDirect) Discover(ctx context.Context) ([]PeerInfo, error) {
    // 1. Send P2P_FIND request via wpa_cli
    // 2. Parse service discovery responses
    // 3. Return discovered peers with addresses
}

// Connect establishes WiFi Direct connection
func (w *WiFiDirect) Connect(peerMAC net.HardwareAddr) (net.Conn, error) {
    // 1. Send P2P_CONNECT <mac> with WPS
    // 2. Wait for connection event
    // 3. Get assigned IP address
    // 4. Return TCP/UDP connection
}
```

### 1.2 Bluetooth Low Energy (BLE)

Works at 1-100 meters. No WiFi at all. Low bandwidth (~1 Mbps) but enough for peer exchange + control.

```
BLE Service:  UUID = "LocalWEB-XXXX-XXXX-XXXX-XXXX"
Characteristics:
  → Identity:  Read/Notify (node ID, capabilities)
  → Messaging: Write/Notify (small messages, control)
  → Transfer:  Write Without Response (bulk data chunks)

Flow:
  1. Node A advertises BLE service
  2. Node B scans, finds LocalWEB service
  3. Node B connects, reads Identity characteristic
  4. Exchange: node IDs, QUIC addresses, capabilities
  5. If both have WiFi → connect via QUIC
  6. If no WiFi → use BLE for small messages, escalate to WiFi Direct
```

**Implementation:**
```go
type BLEDiscovery struct {
    adapter     *bluetooth.Adapter
    serviceName string
    peers       map[PeerID]*BLEPeer
    onPeer      func(PeerInfo)
}

type BLEPeer struct {
    ID         PeerID
    RSSI       int32
    Address    bluetooth.Address
    Capabilities []string
    LastSeen   time.Time
}

// StartAdvertising advertises LocalWEB BLE service
func (b *BLEDiscovery) StartAdvertising(services []string) error {
    // 1. Register GATT service with UUID
    // 2. Add Identity characteristic (read + notify)
    // 3. Add Messaging characteristic (write + notify)
    // 4. Start advertising with service UUID
}

// Scan discovers nearby LocalWEB nodes
func (b *BLEDiscovery) Scan(ctx context.Context) <-chan BLEPeer {
    // 1. Start BLE scan filter for LocalWEB UUID
    // 2. Parse advertisement data
    // 3. Return channel of discovered peers
}
```

### 1.3 USB Tethering

When two laptops are connected via USB cable.

```
Linux: usb0 interface (CDC Ethernet)
macOS: USB-C network interface
Windows: USB RNDIS driver

Flow:
  1. Detect USB network interface (usb0, en0 via USB)
  2. Assign link-local IP (169.254.x.x)
  3. Discover peer via ARP on USB interface
  4. Establish QUIC connection over USB
  5. Bandwidth: ~480 Mbps (USB 2.0) or ~5 Gbps (USB 3.0)
```

### 1.4 Ad-hoc WiFi (IBSS)

No router, no WiFi Direct. Pure 802.11 ad-hoc mode.

```
Linux: iw dev wlan0 set type ibss
       iw dev wlan0 ibss join <ssid> 2412
macOS: Limited support (deprecated)
Windows: Native WiFi ad-hoc support

Flow:
  1. Both nodes join same IBSS network (SSID: "LocalWEB")
  2. Link-local IP assignment (169.254.x.x)
  3. ARP discover peers
  4. QUIC connection
```

### 1.5 Acoustic Coupling (Emergency)

When no network interface works. Uses speakers/microphone.

```
Modulation: FSK (Frequency Shift Keying)
Data rate: ~100 bps (slow but works)
Range: 1-5 meters (acoustic)

Use case: Exchange node IDs + initial keys
  → Node A plays audio sequence
  → Node B decodes via microphone
  → Exchanged: nodeID, publicKey, capabilities
  → Then both try to establish higher-bandwidth link

Note: This is v1.x feature, not v1.0
```

### 1.6 Adaptive Link Manager

```go
type AdaptiveLinkManager struct {
    links       []Link
    active      Link
    preferences LinkPreferences
    monitor     *LinkMonitor
}

type Link interface {
    Name() string
    Priority() int          // Higher = preferred
    Bandwidth() int         // Mbps
    Latency() time.Duration
    Discover(ctx context.Context) ([]PeerInfo, error)
    Connect(addr string) (net.Conn, error)
    IsAvailable() bool
    SupportsWiFi() bool     // false for BLE, USB, acoustic
}

// Available links, ordered by preference:
var linkPriority = []string{
    "wifi-direct",     // Fast, no router, good range
    "wifi-station",    // Fast, needs router
    "ad-hoc-wifi",     // Medium, no router
    "usb-tether",      // Fast, needs cable
    "ble",             // Slow, no infrastructure
    "acoustic",        // Very slow, emergency only
}

func (m *AdaptiveLinkManager) BestLink() Link {
    // 1. Check WiFi available → use WiFi station
    // 2. Check WiFi Direct capable → use WiFi Direct
    // 3. Check USB connected → use USB tether
    // 4. Check BLE available → use BLE
    // 5. Fallback to acoustic (extreme)
}

func (m *AdaptiveLinkManager) Escalate(from, to Link) error {
    // BLE found peer → exchange addresses → connect via WiFi Direct
    // Use low-bandwidth link to bootstrap high-bandwidth link
}
```

---

## Layer 2: Discovery (Multi-Modal)

### 2.1 mDNS-SD (WiFi Mode)

Standard multicast DNS service discovery. Works when WiFi is available.

```
Multicast: 224.0.0.251:5353
Service: _localweb._tcp.local
Announce: every 30s
Expiry: miss 3 announces → evict
```

### 2.2 BLE Discovery (No WiFi Mode)

BLE scanning + advertising. Works without any network.

```
Service UUID: 0x1234 (custom LocalWEB)
Advertising: every 1s
Scanning: continuous
Range: 1-100m
```

### 2.3 WiFi Direct Discovery (No Router Mode)

WiFi Direct service discovery. No router needed.

```
Protocol: WiFi P2P
Discovery: P2P_FIND
Connection: WPS + P2P_CONNECT
```

### 2.4 ARP Scan (Fallback)

ARP requests to all IPs in subnet. Works when multicast is blocked.

```
Range: /24 subnet (254 hosts)
Timeout: 500ms per host
Probe: QUIC on port 4443
```

### 2.5 SSDP/UPnP (NAT Discovery)

M-SEARCH for UPnP devices. Discovers router for port mapping.

```
Multicast: 239.255.255.250:1900
Service: urn:schemas-upnp-org:device:InternetGatewayDevice:1
```

### 2.6 Discovery Orchestrator

```go
type DiscoveryOrchestrator struct {
    modes       []DiscoveryMode
    peerDB      *PeerDatabase
    events      chan PeerEvent
    transport   Transport
}

type DiscoveryMode interface {
    Name() string
    RequiresWiFi() bool
    Discover(ctx context.Context) ([]PeerInfo, error)
    Advertise(info PeerInfo) error
}

func (d *DiscoveryOrchestrator) Run(ctx context.Context) {
    // Phase 1: Check connectivity
    wifi := d.hasWiFi()

    if wifi {
        // Run: mDNS + ARP + SSDP (high bandwidth discovery)
        go d.runMDNS(ctx)
        go d.runARPScan(ctx)
        go d.runSSDP(ctx)
    } else {
        // Run: BLE + WiFi Direct + USB (no infrastructure)
        go d.runBLE(ctx)
        go d.runWiFiDirect(ctx)
        go d.runUSBScan(ctx)
    }

    // Always: merge results into peer database
    for _, mode := range d.modes {
        go func(m DiscoveryMode) {
            for peer := range m.Discover(ctx) {
                d.peerDB.Add(peer)
                d.events <- PeerEvent{Type: PeerJoin, Peer: peer}
            }
        }(mode)
    }
}
```

---

## Layer 4: Transport (Deep)

### 4.1 QUIC + Noise Protocol

```
Handshake: Noise XX pattern over QUIC TLS 1.3
  → Client sends: e (ephemeral X25519 key)
  → Server sends: e, ee, s, es (ephemeral + static + proofs)
  → Client sends: s, se (static + proof)
  → Derived: 256-bit session key → ChaCha20-Poly1305

Streams:
  0: Control (heartbeat, peer exchange)
  1: DNS (DoQ)
  2: HTTP/3
  3: Messaging
  4: File transfer
  5: SMTP
  6: MQTT
  7: WireGuard
  8: Voice/Video
  9+: Dynamic

Flow Control:
  → Per-stream window: 1MB initial, grows to 16MB
  → Connection window: 16MB initial, grows to 256MB
  → Backpressure: pause on memory pressure
  → BDP estimation for window sizing
```

### 4.2 Stream Multiplexer

```go
type StreamMux struct {
    conn        quic.Connection
    streams     map[uint8]*ManagedStream
    mu          sync.RWMutex
    handler     map[uint8]StreamHandler
    flowCtrl    *FlowController
}

type ManagedStream struct {
    id       uint8
    stream   quic.Stream
    sendBuf  chan []byte
    recvBuf  chan []byte
    priority int
    state    StreamState
}

// Multiplex routes incoming streams to handlers
func (m *StreamMux) handleConn(conn quic.Connection) {
    for {
        stream, err := conn.AcceptStream(ctx)
        if err != nil { return }

        go func(s quic.Stream) {
            // Read service ID (1 byte)
            header := make([]byte, 1)
            s.Read(header)
            svcID := header[0]

            // Route to handler
            if h, ok := m.handler[svcID]; ok {
                h(ctx, s)
            }
        }(stream)
    }
}
```

### 4.3 Circuit Relay v2

```
Protocol: Same as Tor but simpler
  → 3-hop circuit: A → R1 → R2 → B
  → Each hop: separate encryption layer
  → Relay sees only adjacent hop

Setup:
  1. A selects relay R1 from DHT (score > 0.8)
  2. A → R1: "extend to R2"
  3. R1 → R2: "connect from A"
  4. R2 → B: "new connection from relay"
  5. B accepts → circuit established

Data flow:
  A encrypts 3x → R1 decrypts 1x → R2 decrypts 1x → B decrypts 1x
  Each relay only sees one encryption layer
```

### 4.4 NAT Traversal

```
Strategy 1: UDP Hole Punching
  → A learns B's public IP:port from DHT
  → Both send UDP to each other simultaneously
  → NAT creates mapping → bidirectional flow

Strategy 2: Port Prediction
  → Predict NAT port allocation pattern
  → Send to predicted port

Strategy 3: UPnP/PCP
  → Query router for port mapping
  → Map external port → internal:4443

Strategy 4: Relay Fallback
  → If all else fails → circuit relay
  → Always works (just slower)
```

---

## Layer 6: CRDT Sync Engine (Deep)

### 6.1 OR-Set (Observed-Remove Set)

```
Used for: DNS records, channel membership, capabilities

Structure:
  Adds:    map[element]Set<tag>    // tags are unique per add
  Removes: Set<tag>               // tombstones

Operation:
  Add(elem):
    tag = uniqueTag()
    adds[elem].insert(tag)
    broadcast(AddOp{elem, tag})

  Remove(elem):
    tags = adds[elem]
    removes.union(tags)
    adds[elem].removeAll(tags)
    broadcast(RemoveOp{elem, tags})

Merge(A, B):
  adds = A.adds.union(B.adds) - A.removes.union(B.removes)
  removes = A.removes.union(B.removes)
```

### 6.2 RGA (Replicated Growable Array)

```
Used for: Text editing, chat history

Structure: Linked list of (timestamp, author, value)
  Each node has: ID = (timestamp, author)
  Position determined by: causal ordering

Operation:
  Insert(afterID, value):
    node = {ID: (now, self), Value: value, After: afterID}
    list.insertAfter(afterID, node)
    broadcast(InsertOp{node})

  Delete(nodeID):
    node.Value = nil  // tombstone
    broadcast(DeleteOp{nodeID})

Merge(A, B):
  1. Find nodes in B not in A
  2. Insert each after its causal predecessor
  3. Tombstones applied to both
  Result: Same text on both sides
```

### 6.3 LWW-Register (Last-Writer-Wins)

```
Used for: User presence, config, file metadata

Structure: {Value, Timestamp, Author}

Operation:
  Set(value):
    reg = {Value: value, Timestamp: now(), Author: self}
    broadcast(SetOp{reg})

Merge(A, B):
  if A.Timestamp > B.Timestamp: return A
  if B.Timestamp > A.Timestamp: return B
  if A.Timestamp == B.Timestamp: return A if A.Author > B.Author (tiebreak)
```

### 6.4 Anti-Entropy Protocol

```
On connect to peer:
  1. Exchange Merkle roots for each CRDT domain
     → Root(domain) = SHA3(merge all states)

  2. Diff: find branches where hashes differ
     → Walk tree, compare hashes at each level
     → Stop at leaf where hash differs

  3. Request missing branches
     → Send: GetBranch(domain, path)
     → Receive: Branch{path, state, children}

  4. Merge using CRDT rules
     → OR-Set: union adds, union removes
     → RGA: insert missing nodes in causal order
     → LWW: keep latest timestamp

  5. Update local Merkle root
     → Recompute root from merged state

Periodic:
  → Full sync every 300s
  → Delta sync on each mutation
  → Sync all connected peers
```

---

## Layer 7: Services (Deep)

### 9.1 DNS — Full RFC 1035 Implementation

```go
// DNS Message Format (RFC 1035 §4.1)
type DNSMessage struct {
    Header      DNSHeader
    Questions   []DNSQuestion
    Answers     []DNSRecord
    Authorities []DNSRecord
    Additionals []DNSRecord
}

type DNSHeader struct {
    ID      uint16    // Transaction ID
    Flags   uint16    // QR, Opcode, AA, TC, RD, RA, Z, RCODE
    QDCOUNT uint16
    ANCOUNT uint16
    NSCOUNT uint16
    ARCOUNT uint16
}

// Record types (RFC 1035 §3.2.4)
const (
    TypeA     = 1
    TypeNS    = 2
    TypeCNAME = 5
    TypeSOA   = 6
    TypePTR   = 12
    TypeMX    = 15
    TypeTXT   = 16
    TypeAAAA  = 28
    TypeSRV   = 33
    TypeHTTPS = 65
    TypeSVCB  = 64
)

// .localweb zone management
type Zone struct {
    SOA       SOARecord
    Records   map[string][]ResourceRecord
    Transfers []PeerID        // Peers allowed to AXFR
    SignedAt  time.Time
    Sig       [64]byte        // Ed25519 signature
}
```

### 9.2 HTTP/3 — Full Gateway

```
Features:
  → HTTP/3 over QUIC (RFC 9114)
  → QPACK header compression
  → Static file serving (range requests, ETags, compression)
  → Reverse proxy (load balancing, health checks)
  → WebSocket over QUIC stream
  → Server-Sent Events
  → Auto-generated TLS certs for .localweb
  → Content-addressable cache (/ipfs/<cid>)
  → CORS, CSP headers
  → Rate limiting per client IP
  → Request body streaming
  → Graceful shutdown (drain connections)
```

### 9.3 Email — Full SMTP/IMAP

```
SMTP Server (RFC 5321):
  → EHLO/HELO greeting
  → STARTTLS (upgrade to QUIC)
  → AUTH PLAIN/LOGIN/CRAM-MD5
  → MAIL FROM / RCPT TO / DATA
  → Message parsing (RFC 5322 headers)
  → Local delivery (Maildir)
  → Remote delivery via QUIC (DHT lookup → connect → SMTP)
  → Queue: offline messages stored, retried on reconnect
  → Bounce messages for undeliverable

IMAP Server (RFC 9051):
  → LOGIN / CAPABILITY
  → SELECT / EXAMINE mailbox
  → FETCH (body, headers, bodystructure)
  → STORE (flags: \Seen, \Answered, \Flagged, \Deleted)
  → SEARCH (by date, from, subject, text)
  → COPY / MOVE
  → IDLE (push notifications)
  → UIDVALIDITY / UIDNEXT
```

### 9.4 Messaging — E2E Encrypted

```
Encryption layers:
  1. X3DH key agreement (initial handshake)
  2. Double Ratchet (per-message key evolution)
  3. Sender keys (group messaging efficiency)

Protocol buffers:
  message Envelope {
    bytes sender_public_key = 1;
    bytes encrypted_payload = 2;
    bytes nonce = 3;
    int64 timestamp = 4;
  }

  message PlaintextPayload {
    string message_id = 1;
    string channel_id = 2;
    bytes sender = 3;
    int64 timestamp = 4;
    oneof content {
      TextContent text = 5;
      FileContent file = 6;
      ReactionContent reaction = 7;
      TypingIndicator typing = 8;
    }
    bytes signature = 9;
    string parent_id = 10;
  }
```

### 9.5 File Sync — Content-Addressed

```
Block Protocol:
  → 4MB blocks (configurable)
  → CID = SHA3-256(block_data)
  → Zstd compression before storage (level 3)
  → Merkle DAG: file → blocks → root CID

Bitswap-like Exchange:
  → Have: list of CIDs I have
  → Want: list of CIDs I need
  → Block: actual block data
  → On connect: exchange Have/Want lists
  → Priority: rarest-first block selection

Sync Protocol:
  1. Node A sends root CID of file tree
  2. Node B compares with local root
  3. If different: request Merkle proof of difference
  4. Identify changed subtrees
  5. Request only changed blocks
  6. Deduplicate globally by CID
```

### 9.6 Collaborative Docs — Real-time CRDT

```
Document = RGA CRDT for text content

Operations:
  Insert(position, char, author)
  Delete(position, author)

Conflict resolution:
  → RGA guarantees convergence
  → Concurrent inserts: order by (timestamp, author_id)
  → Delete + Insert at same position: insert wins (tombstone for delete)

Presence:
  → Each editor broadcasts cursor position
  → Show remote cursors with colors
  → Typing indicators

History:
  → Full operation log (append-only)
  → Undo: apply inverse operations
  → Redo: re-apply undone operations
  → Export: current RGA state → Markdown/HTML
```

### 9.7 Voice/Video

```
Audio:
  → Opus codec (48kHz, 32kbps mono, 128kbps stereo)
  → Capture: microphone → Opus encoder → QUIC stream
  → Playback: QUIC stream → Opus decoder → speaker
  → Echo cancellation (speexdsp AEC)
  → Noise suppression (RNNoise)
  → AGC (Automatic Gain Control)
  → Jitter buffer (adaptive, 20-200ms)

Video:
  → VP9 codec (720p@30fps or 1080p@15fps)
  → Capture: screen/window → VP9 encoder → QUIC stream
  → Decode: QUIC stream → VP9 decoder → display
  → Adaptive bitrate (bandwidth estimation)
  → Key frame interval: 2s

Signaling:
  → Via messaging channel (QUIC stream 3)
  → Offer/Answer exchange
  → ICE candidate gathering
  → Call state machine: idle → ringing → connecting → active → ended

Group calls:
  → Star topology: all connect to initiator
  → Initiator mixes audio, relays video
  → Or: SFU-style: each sends to all
```

### 9.8 Mesh VPN (WireGuard)

```
WireGuard-compatible:
  → Noise_IK handshake
  → ChaCha20-Poly1305 packet encryption
  → BLAKE2s key derivation
  → Keypair: private + public per node

TUN interface:
  → Linux: /dev/tun0 via tuntap
  → macOS: utun interface
  → Windows: WinTun driver

Addressing:
  → IPv6: fd00:localweb:<node_id>/128
  → IPv4: 10.<network>.<node>.1/32

Routing:
  → Each node advertises its IP via DHT
  → Route table: destination → tunnel peer
  → Default: only route LocalWEB traffic (split tunnel)
```

### 9.9 App Registry

```
Package format (.lwpkg):
  tar.gz containing:
    manifest.yaml:
      name: my-app
      version: 1.0.0
      description: ...
      author: <pubkey>
      dependencies:
        - name: core-utils
          version: ">=1.0.0"
      binaries:
        - os: linux
          arch: amd64
          path: bin/my-app
          checksum: sha3-256:...
    bin/
      my-app (ELF binary)
    lib/
      libfoo.so

Signature:
  → Ed25519 signature of manifest.yaml
  → Stored in manifest.yaml: signature: <base64>
  → Verified before install

Registry server:
  → HTTP API for listing/searching packages
  → DHT for package metadata distribution
  → Content-addressed storage for package files
```

---

## Configuration (v3)

```yaml
node:
  name: "my-laptop"
  data_dir: "./data"
  listen_addr: "0.0.0.0"
  max_storage: 10737418240    # 10GB

link:
  modes:                       # Which link layers to use
    - wifi-station
    - wifi-direct
    - ad-hoc-wifi
    - ble
    - usb-tether
  preferences:                 # Order of preference
    - wifi-direct
    - wifi-station
    - ad-hoc-wifi
    - usb-tether
    - ble
  auto_escalate: true          # BLE → WiFi Direct automatically
  ble_scan_interval: "1s"
  ble_advertise_interval: "1s"

discovery:
  modes:
    - mdns                     # WiFi mode
    - ble                      # No WiFi mode
    - wifi-direct              # No router mode
    - arp-scan                 # Fallback
    - ssdp                     # NAT discovery
  interval: "30s"
  ble_enabled: true
  mdns_enabled: true

dht:
  enabled: true
  bucket_size: 20
  alpha: 3
  max_hops: 15
  refresh_interval: "1h"
  sybil_difficulty: 20
  bootstrap_peers: []

transport:
  quic_port: 4443
  noise_enabled: true
  relay_enabled: true
  max_connections: 1000
  keepalive: "30s"
  flow_control:
    stream_window: 1048576     # 1MB
    conn_window: 16777216      # 16MB
    max_window: 268435456      # 256MB

security:
  tls_min_version: "1.3"
  auth_required: false
  pow_difficulty: 20
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
    block_size: 4194304
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
    enabled: false
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

## Performance Targets

| Metric | With WiFi | Without WiFi (BLE) | Without WiFi (WiFi Direct) |
|--------|-----------|--------------------|-----------------------------|
| Discovery time | <500ms | <5s | <2s |
| Connection time | <50ms | <3s | <500ms |
| Bandwidth | 100+ Mbps | ~1 Mbps | 50+ Mbps |
| Latency | <10ms | <100ms | <20ms |
| Max peers | 1000+ | 10 | 10 |
| Message delivery | <50ms | <500ms | <100ms |
| File sync (100MB) | <10s | ~100s | ~5s |
| Node startup | <2s | <2s | <2s |
| Memory idle | <100MB | <50MB | <100MB |

---

*Last updated: 2026-09-04*
*Version: 3.0*
*Author: Mrityunjay K*

# LocalWEB — v1.0 Roadmap v3

**Works without WiFi. Works with WiFi. Better than both.**

---

## Phase Overview

| Phase | Duration | Focus | Parallel Tracks |
|-------|----------|-------|-----------------|
| 1 | Week 1-2 | Adaptive Link + Discovery + Transport | 3 tracks |
| 2 | Week 3-4 | DHT + DNS + HTTP/3 | 3 tracks |
| 3 | Week 5-6 | Email + Messaging + File Sync | 3 tracks |
| 4 | Week 7-8 | CRDT Engine + Docs + Voice/Video | 3 tracks |
| 5 | Week 9-10 | VPN + Registry + Integration | 3 tracks |
| 6 | Week 11-12 | Testing + Polish + Release | Full stack |

---

## Phase 1: Adaptive Link + Discovery + Transport (Week 1-2)

### Track 1A: Adaptive Link Manager (Days 1-7)

**Goal:** Laptops discover and connect via ANY available link — WiFi, WiFi Direct, BLE, USB.

**Files to create:**
```
pkg/link/manager.go            (NEW — adaptive link orchestrator)
pkg/link/wifi.go               (NEW — WiFi station mode)
pkg/link/wifi_direct.go        (NEW — WiFi Direct P2P)
pkg/link/ble.go                (NEW — Bluetooth Low Energy)
pkg/link/usb.go                (NEW — USB tethering)
pkg/link/adhoc.go              (NEW — ad-hoc WiFi)
pkg/link/types.go              (NEW — link interfaces + types)
```

**Day 1-2: Link Interface + Manager**
```go
type Link interface {
    Name() string
    RequiresWiFi() bool
    RequiresRouter() bool
    Bandwidth() int
    Latency() time.Duration
    Discover(ctx context.Context) ([]PeerInfo, error)
    Connect(addr string) (net.Conn, error)
    IsAvailable() bool
}

type AdaptiveLinkManager struct {
    links       []Link
    active      Link
    onPeer      func(PeerInfo)
    monitor     *LinkMonitor
}
```

- Detect available network interfaces
- Enumerate all link types
- Rank by preference (WiFi Direct > WiFi > BLE > USB)
- Monitor link quality, auto-switch on failure

**Day 3-4: WiFi Direct (No Router)**
- wpa_supplicant P2P control interface (Linux)
- P2P_FIND service discovery
- Group Owner negotiation
- WPS connection setup
- IP assignment (GO runs DHCP)
- QUIC connection over WiFi Direct interface

**Day 5-6: BLE (No WiFi)**
- BLE GATT service registration (UUID: LocalWEB)
- Identity characteristic (read + notify)
- Messaging characteristic (write + notify)
- Continuous scanning for peers
- RSSI-based proximity sorting
- Auto-escalate: BLE peer found → exchange WiFi Direct credentials → high-bandwidth connect

**Day 7: USB + Ad-hoc + Integration**
- USB CDC Ethernet detection (usb0 interface)
- Link-local IP assignment
- ARP discovery on USB interface
- Ad-hoc WiFi (IBSS) join/create
- Merge all discovered peers into unified database

**Acceptance Criteria:**
- [ ] Two laptops on same WiFi discover each other via mDNS
- [ ] Two laptops with no router discover each other via WiFi Direct
- [ ] Two laptops with BLE find each other within 10m
- [ ] Two laptops connected via USB discover each other
- [ ] Adaptive link manager selects best available link
- [ ] Auto-escalation: BLE → WiFi Direct works

---

### Track 1B: Discovery (Days 1-7)

**Goal:** Multi-modal peer discovery that works in all scenarios.

**Files to create:**
```
pkg/discovery/discovery.go     (REWRITE — orchestrator)
pkg/discovery/mdns.go          (REWRITE — full mDNS-SD)
pkg/discovery/ble_scan.go      (NEW — BLE discovery)
pkg/discovery/arp.go           (NEW — ARP scan)
pkg/discovery/ssdp.go          (NEW — UPnP/SSDP)
pkg/discovery/peer_db.go       (NEW — peer database)
```

**Day 1-2: mDNS-SD (WiFi Mode)**
- DNS wire format encoder/decoder (RFC 1035)
- Multicast query: "ANY _localweb._tcp.local"
- Response parsing: SRV + A/AAAA + TXT records
- Announce with capabilities
- Address conflict detection
- TTL-based peer expiry (miss 3 announces → evict)

**Day 3-4: Peer Database**
- In-memory peer store with BadgerDB persistence
- Peer scoring (0.0 → 1.0): response rate, uptime, content validity
- Event system: PeerJoin, PeerLeave, PeerUpdate
- Capacity: 1000+ peers per node
- Thread-safe operations

**Day 5: ARP Scan + SSDP**
- ARP request for entire /24 subnet
- Parse responses for MAC + IP
- Probe each IP for QUIC on port 4443
- SSDP M-SEARCH for UPnP devices
- Router detection for NAT traversal

**Day 6-7: BLE Discovery + Integration**
- BLE scanning for LocalWEB service UUID
- Parse advertisement data for peer info
- Merge results from all discovery modes
- Deduplicate by PeerID
- Notify handlers on new peer

**Acceptance Criteria:**
- [ ] mDNS finds peers on same subnet in <500ms
- [ ] ARP scan finds 254 hosts in <1s
- [ ] SSDP finds UPnP router
- [ ] BLE discovers peers within 10m
- [ ] All modes merge into unified peer list
- [ ] Peer scoring works correctly

---

### Track 1C: Transport (Days 1-7)

**Goal:** QUIC + Noise transport with stream multiplexing, relay, and NAT traversal.

**Files to create:**
```
pkg/transport/quic.go          (REWRITE — full Noise handshake)
pkg/transport/stream.go        (NEW — stream multiplexer)
pkg/transport/connection.go    (NEW — connection pool)
pkg/transport/relay.go         (NEW — circuit relay v2)
pkg/transport/nat.go           (NEW — NAT traversal)
```

**Day 1-2: Noise XX Handshake**
- Replace bare TLS with Noise Protocol Framework
- Ed25519 static key exchange
- X25519 ephemeral keys (forward secrecy)
- HKDF session key derivation
- Mutual authentication

**Day 3-4: Stream Multiplexer**
- Service ID routing (1-byte prefix)
- Per-stream flow control (1MB initial, grows to 16MB)
- Connection-level backpressure (16MB → 256MB)
- 0-RTT connection resumption
- Stream lifecycle: open → data → half-close → close

**Day 5-6: Circuit Relay + NAT Traversal**
- Relay discovery via DHT
- 3-hop circuit: A → R1 → R2 → B
- Double-encrypted relay path
- UDP hole punching
- STUN-like address discovery
- NAT type detection (cone, symmetric)
- Fallback: relay when direct fails

**Day 7: Integration + Benchmarks**
- Integrate with Adaptive Link Manager
- Benchmark: <50ms connect, <1ms stream open
- Test: direct, relay, NAT traversal
- Connection pooling (max 1000 per node)

**Acceptance Criteria:**
- [ ] Two nodes connect via QUIC with Noise handshake
- [ ] Multiple service streams multiplexed
- [ ] Relay routing works when direct fails
- [ ] 0-RTT connection resumption works
- [ ] NAT traversal works with hole punching
- [ ] <50ms connect time

---

## Phase 2: DHT + DNS + HTTP/3 (Week 3-4)

### Track 2A: DHT (Days 8-14)

**Files:**
```
pkg/dht/dht.go         (REWRITE)
pkg/dht/routing.go     (NEW)
pkg/dht/lookup.go      (NEW)
pkg/dht/storage.go     (NEW)
pkg/dht/sybil.go       (NEW)
```

**Kademlia + S/Kademlia:**
- 256 k-buckets (k=20 peers each)
- α=3 parallel queries, 500ms timeout, 15 max hops
- STORE: replicate to k=20 closest
- FIND_NODE/FIND_VALUE: iterative lookup
- Storage proofs: HMAC challenge/response
- Sybil resistance: computational puzzle (20-bit)
- Peer scoring: response rate, uptime, validity

---

### Track 2B: DNS (Days 8-14)

**Files:**
```
pkg/services/dns/server.go   (REWRITE — RFC 1035)
pkg/services/dns/parser.go   (NEW)
pkg/services/dns/cache.go    (NEW)
pkg/services/dns/zone.go     (NEW)
```

**Full RFC 1035:**
- Message parser/serializer (all record types)
- .localweb TLD authority
- Wildcard records
- Zone transfer (AXFR)
- TTL-based cache
- Resolution chain: cache → mDNS → DHT
- Ed25519 DNSSEC-like signing

---

### Track 2C: HTTP/3 (Days 8-14)

**Files:**
```
pkg/services/http/server.go    (REWRITE)
pkg/services/http/static.go    (NEW)
pkg/services/http/proxy.go     (NEW)
pkg/services/http/websocket.go (NEW)
pkg/services/http/cert.go      (NEW)
```

**Full HTTP/3:**
- HTTP/3 over QUIC (RFC 9114)
- QPACK header compression
- Static file serving (range, ETag, compression)
- Reverse proxy (load balancing, health checks)
- WebSocket over QUIC stream
- Auto-generated TLS certs for .localweb
- Content-addressable cache (/ipfs/<cid>)
- CORS, CSP, rate limiting

---

## Phase 3: Email + Messaging + File Sync (Week 5-6)

### Track 3A: Email (Days 15-21)

**Files:**
```
pkg/services/email/smtp.go      (NEW)
pkg/services/email/imap.go      (NEW)
pkg/services/email/mailbox.go   (NEW)
pkg/services/email/antispam.go  (NEW)
pkg/services/email/types.go     (NEW)
```

**Full SMTP/IMAP:**
- SMTP: EHLO, STARTTLS, AUTH, MAIL FROM/RCPT TO/DATA
- Remote delivery via QUIC (DHT lookup → SMTP)
- IMAP4rev2: SELECT, FETCH, STORE, SEARCH, IDLE
- Maildir storage
- PoW anti-spam
- Offline queue with retry

---

### Track 3B: Messaging (Days 15-21)

**Files:**
```
pkg/services/messaging/server.go     (REWRITE)
pkg/services/messaging/channel.go    (NEW)
pkg/services/messaging/store.go      (NEW)
pkg/services/messaging/ratchet.go    (NEW)
pkg/services/messaging/types.go      (REWRITE)
```

**E2E Encrypted Messaging:**
- X3DH key agreement
- Double Ratchet per-message
- Sender keys for groups
- Channel policies: open/invite_only/admin_only
- Offline queue, delivery receipts
- Media via content-addressed storage
- Reactions, replies, threads

---

### Track 3C: File Sync (Days 15-21)

**Files:**
```
pkg/services/files/files.go     (REWRITE)
pkg/services/files/server.go    (REWRITE)
pkg/services/files/sync.go      (NEW)
pkg/services/files/fuse.go      (NEW)
pkg/services/files/store.go     (NEW)
```

**Content-Addressed File Sync:**
- 4MB blocks, Zstd compression
- Global deduplication via CID
- Merkle DAG diff (only transfer changed blocks)
- Bitswap-like exchange protocol
- FUSE mount (Linux/macOS), Dokany (Windows)
- File versioning, ACL by pubkey
- Sync engine: periodic + on-mutation

---

## Phase 4: CRDT Engine + Docs + Voice/Video (Week 7-8)

### Track 4A: CRDT Engine (Days 22-28)

**Files:**
```
pkg/sync/crdt.go              (NEW)
pkg/sync/orset.go             (NEW)
pkg/sync/lww.go               (NEW)
pkg/sync/rga.go               (NEW)
pkg/sync/vector_clock.go      (NEW)
pkg/sync/merkle.go            (NEW)
pkg/sync/anti_entropy.go      (NEW)
pkg/sync/store.go             (NEW)
```

**Full CRDT:**
- OR-Set (DNS, channels)
- LWW-Register (presence, config)
- LWW-Element-Set (file metadata)
- RGA (text editing)
- Vector clock (causal ordering)
- Merkle DAG (state verification)
- Anti-entropy protocol (convergence)
- Encrypted BadgerDB store

---

### Track 4B: Collaborative Docs (Days 22-28)

**Files:**
```
pkg/services/docs/server.go    (NEW)
pkg/services/docs/rga.go       (NEW)
pkg/services/docs/presence.go  (NEW)
pkg/services/docs/types.go     (NEW)
```

**Real-time Collaborative Editing:**
- RGA CRDT for text
- Insert/delete operations broadcast
- Presence: cursor positions, selections
- Rich text: headings, lists, code blocks
- Full edit history, undo/redo
- Export: Markdown, HTML
- Offline edits merged on reconnect

---

### Track 4C: Voice/Video (Days 22-28)

**Files:**
```
pkg/services/voice/server.go     (NEW)
pkg/services/voice/codec.go      (NEW)
pkg/services/voice/signaling.go  (NEW)
pkg/services/voice/track.go      (NEW)
```

**Voice/Video Calls:**
- Opus audio (48kHz, echo cancellation)
- VP9 video (720p, adaptive bitrate)
- Call signaling via messaging
- ICE-like candidate gathering
- Group calls: star topology
- Screen sharing
- Data channels for text

---

## Phase 5: VPN + Registry + Integration (Week 9-10)

### Track 5A: Mesh VPN (Days 29-35)

**Files:**
```
pkg/services/vpn/server.go    (NEW)
pkg/services/vpn/tunnel.go    (NEW)
pkg/services/vpn/routing.go   (NEW)
pkg/services/vpn/tun.go       (NEW)
```

**WireGuard-compatible VPN:**
- Noise_IK handshake
- TUN interface (Linux/macOS/Windows)
- fd00:localweb:<id>/128 addressing
- Route propagation via DHT
- Split tunneling
- NAT traversal + relay fallback

---

### Track 5B: App Registry (Days 29-35)

**Files:**
```
pkg/services/registry/server.go   (NEW)
pkg/services/registry/package.go  (NEW)
pkg/services/registry/verify.go   (NEW)
```

**Package Registry:**
- .lwpkg format (tar.gz + Ed25519 signature)
- manifest.yaml (name, version, deps, checksums)
- HTTP API for listing/searching
- DHT for metadata distribution
- CLI: install, list, search, publish

---

### Track 5C: Integration (Days 29-35)

**Files:**
```
test/integration/discovery_test.go
test/integration/dht_test.go
test/integration/dns_test.go
test/integration/http_test.go
test/integration/email_test.go
test/integration/messaging_test.go
test/integration/files_test.go
test/integration/voice_test.go
test/integration/vpn_test.go
test/integration/docs_test.go
test/integration/full_stack_test.go
```

**Integration Testing:**
- Two-node discovery (all link types)
- DHT routing across 5 nodes
- DNS resolution end-to-end
- HTTP file serving
- Email send/receive
- Message delivery with E2E encryption
- File sync with deduplication
- Voice call quality
- Full stack: 5 nodes, all services

---

## Phase 6: Testing + Polish + Release (Week 11-12)

### Track 6A: Performance + Security (Days 36-42)

- Benchmark all services
- Profile CPU/memory
- Optimize hot paths
- Crypto audit
- Protocol fuzzing
- Rate limiting
- Graceful shutdown
- Resource limits

### Track 6B: Documentation + Release (Days 36-42)

- API documentation
- Protocol specifications
- Configuration reference
- CLI help text
- Cross-compile: Linux, macOS, Windows
- Release binaries
- Installation scripts
- Changelog

---

## Build System

```makefile
.PHONY: build test lint cross-compile generate

build:
	go build -o bin/localweb-node ./cmd/node
	go build -o bin/localweb ./cmd/cli

test:
	go test ./... -v -count=1
	go test -race ./...
	go test -coverprofile=coverage.out ./...

bench:
	go test -bench=. -benchmem ./...

lint:
	golangci-lint run
	go vet ./...
	go fmt ./...

cross-compile:
	GOOS=linux   GOARCH=amd64 go build -o bin/localweb-linux-amd64 ./cmd/node
	GOOS=linux   GOARCH=arm64 go build -o bin/localweb-linux-arm64 ./cmd/node
	GOOS=darwin  GOARCH=arm64 go build -o bin/localweb-macos-arm64 ./cmd/node
	GOOS=darwin  GOARCH=amd64 go build -o bin/localweb-macos-amd64 ./cmd/node
	GOOS=windows GOARCH=amd64 go build -o bin/localweb-windows-amd64.exe ./cmd/node

generate:
	protoc --go_out=. --go_opt=paths=source_relative api/proto/messages.proto

clean:
	rm -rf bin/ coverage.out data/
```

---

## Success Criteria for v1.0

| Metric | With WiFi | Without WiFi |
|--------|-----------|--------------|
| Discovery | <500ms | <5s (BLE) / <2s (WiFi Direct) |
| Connection | <50ms | <3s (BLE) / <500ms (WiFi Direct) |
| Bandwidth | 100+ Mbps | ~1 Mbps (BLE) / 50+ Mbps (WiFi Direct) |
| DNS | Working | Working |
| HTTP/3 | Working | Working |
| Email | Send/receive | Send/receive |
| Messaging | E2E encrypted | E2E encrypted |
| File sync | Block-level | Block-level |
| Voice/Video | Clear audio | Audio only (BLE) |
| Collaborative docs | Real-time | Real-time |
| VPN | Working | Working |
| All services | Simultaneous | Simultaneous |
| Test coverage | >= 80% | >= 80% |
| Cross-platform | Linux, macOS, Windows | Linux, macOS, Windows |

---

*Last updated: 2026-09-04*
*Version: 3.0*
*Author: Mrityunjay K*

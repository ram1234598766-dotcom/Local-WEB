# LocalWEB — v1.0 Roadmap

**All services simultaneously. Real implementation. No simulators.**

---

## Overview

| Phase | Duration | Focus | Agents |
|-------|----------|-------|--------|
| Phase 1 | Week 1-2 | Core Transport + Discovery + DHT | 3 parallel tracks |
| Phase 2 | Week 3-4 | DNS + HTTP/3 + Email | 3 parallel tracks |
| Phase 3 | Week 5-6 | Messaging + File Sync + CRDT | 3 parallel tracks |
| Phase 4 | Week 7-8 | Voice/Video + VPN + Docs | 3 parallel tracks |
| Phase 5 | Week 9-10 | App Registry + Integration | 2 parallel tracks |
| Phase 6 | Week 11-12 | Testing + Polish + Release | Full stack |

**Total: 12 weeks to v1.0 working internet stack**

---

## Phase 1: Core Transport + Discovery + DHT (Week 1-2)

### Track 1A: QUIC Transport (Days 1-7)

**Files to create:**
```
pkg/transport/quic.go        (REWRITE — full Noise handshake)
pkg/transport/stream.go      (NEW — stream multiplexer)
pkg/transport/connection.go  (NEW — connection pool + management)
pkg/transport/relay.go       (NEW — circuit relay v2)
pkg/transport/nat.go         (NEW — UDP hole punching)
```

**Day 1-2: Noise XX Handshake**
- Replace bare TLS with Noise Protocol Framework
- Ed25519 static key exchange
- X25519 ephemeral keys for forward secrecy
- HKDF session key derivation
- Mutual authentication

**Day 3-4: Stream Multiplexer**
- Service ID routing (1-byte prefix per stream)
- Per-stream flow control
- Connection-level backpressure
- 0-RTT connection resumption
- Stream lifecycle management (open, half-close, reset)

**Day 5-6: Circuit Relay**
- Relay discovery via DHT
- Relay capability exchange
- Double-encrypted relay path (A→R→B)
- Relay capacity limits (max 100 streams per relay)
- Relay scoring and rotation

**Day 7: NAT Traversal**
- UDP hole punching (simultaneous open)
- STUN-like address discovery
- NAT type detection (cone, symmetric)
- Fallback: relay when direct fails

**Acceptance Criteria:**
- [ ] Two nodes connect via QUIC with Noise handshake
- [ ] Multiple service streams multiplexed over single connection
- [ ] Relay routing works when direct connection fails
- [ ] 0-RTT connection resumption works
- [ ] Benchmarks: <50ms connect, <1ms stream open

---

### Track 1B: Discovery (Days 1-7)

**Files to create:**
```
pkg/discovery/discovery.go   (REWRITE — orchestrator)
pkg/discovery/mdns.go        (REWRITE — full mDNS-SD)
pkg/discovery/dns_sd.go      (NEW — service browsing)
pkg/discovery/arp.go         (NEW — ARP scan fallback)
pkg/discovery/ssdp.go        (NEW — UPnP discovery)
pkg/discovery/ble.go         (NEW — Bluetooth LE)
```

**Day 1-2: mDNS-SD (Primary)**
- DNS wire format encoder/decoder
- Multicast query: "ANY _localweb._tcp.local"
- Response parsing: SRV + A/AAAA + TXT records
- Announce with capabilities
- Address conflict detection (probing)
- TTL-based peer expiry

**Day 3-4: DNS-SD Browse + Service Resolution**
- Browse for all LocalWEB nodes
- Service-specific queries (_http._tcp, _smtp._tcp, etc.)
- Peer capability negotiation
- Event system: PeerJoin, PeerLeave, PeerUpdate

**Day 5: ARP Scan Fallback**
- ARP request for entire /24 subnet
- Parse responses for MAC + IP
- Probe each IP for QUIC on port 4443
- Merge discovered peers with mDNS results

**Day 6: SSDP/UPnP**
- M-SEARCH multicast for UPnP devices
- Parse location headers for router detection
- UPnP port mapping for NAT traversal
- Router model + capabilities detection

**Day 7: Bluetooth LE (Optional)**
- GATT service advertisement
- Scan for matching LocalWEB UUIDs
- Exchange addresses via characteristic
- Proximity-based mesh routing

**Acceptance Criteria:**
- [ ] Two laptops on same WiFi discover each other in <500ms
- [ ] mDNS announces work across subnet boundaries
- [ ] ARP scan finds peers when multicast is blocked
- [ ] SSDP discovers router for port mapping
- [ ] All discovery methods merge into unified peer list

---

### Track 1C: DHT (Days 1-7)

**Files to create:**
```
pkg/dht/dht.go         (REWRITE — full implementation)
pkg/dht/routing.go     (NEW — optimized k-bucket)
pkg/dht/lookup.go      (NEW — iterative lookup)
pkg/dht/storage.go     (NEW — DHT storage + proofs)
pkg/dht/sybil.go       (NEW — S/Kademlia hardening)
```

**Day 1-2: Routing Table + K-Buckets**
- XOR distance calculation
- Bucket indexing by prefix length
- Bucket splitting
- Eviction policy: lowest latency
- Thread-safe operations

**Day 3-4: Iterative Lookup**
- FIND_NODE: α=3 parallel queries
- FIND_VALUE: iterative with caching
- Timeout: 500ms per round
- Max hops: 15
- Convergence: O(log n)

**Day 5: Storage Operations**
- STORE: replicate to k=20 closest peers
- GET: iterative lookup with first-value-wins
- Storage proofs: HMAC-based challenge/response
- Expiration: TTL-based eviction

**Day 6: S/Kademlia Sybil Resistance**
- Pre-layer computational puzzle
- Bucket isolation by prefix
- Peer scoring (0.0 → 1.0)
- Dynamic difficulty adjustment

**Day 7: Bootstrap + Integration**
- Bootstrap from config peers
- Bootstrap from mDNS-discovered peers
- Periodic refresh (random lookups)
- Integration with transport layer

**Acceptance Criteria:**
- [ ] DHT routes correctly (XOR distance)
- [ ] Iterative lookup finds k closest peers
- [ ] STORE/GET work across 3+ nodes
- [ ] Storage proofs verify correctly
- [ ] Sybil resistance blocks fake nodes
- [ ] Bootstrap joins network in <5s

---

## Phase 2: DNS + HTTP/3 + Email (Week 3-4)

### Track 2A: DNS Service (Days 8-14)

**Files to create:**
```
pkg/services/dns/server.go   (REWRITE — RFC 1035)
pkg/services/dns/parser.go   (NEW — message parser/serializer)
pkg/services/dns/cache.go    (NEW — TTL cache)
pkg/services/dns/zone.go     (NEW — zone management)
```

**Day 8-9: DNS Message Parser**
- Full RFC 1035 header parsing
- Question section: QNAME, QTYPE, QCLASS
- Answer/Authority/Additional sections
- Record types: A, AAAA, TXT, SRV, CNAME, PTR, HTTPS
- Compression pointers (RFC 1035 §4.1.4)
- Wire format encode/decode

**Day 10-11: Zone Management**
- .localweb TLD authority
- Per-node zone: *.mynode.localweb
- Wildcard records (*.localweb)
- Zone transfer (AXFR) between peers
- SOA record generation

**Day 12-13: DNS Cache + Resolution**
- TTL-based cache with negative TTL
- Cache warming from DHT
- Resolution chain: cache → mDNS → DHT → upstream
- EDNS0 support (UDP payload size)
- DNSSEC-like signing with Ed25519

**Day 14: Integration**
- Register node's DNS records on startup
- Auto-update records when services change
- Port 5353 (mDNS) + 5354 (fallback)
- TCP fallback for large responses

**Acceptance Criteria:**
- [ ] `dig @localhost -p 5353 mynode.localweb A` returns correct IP
- [ ] All record types parse and serialize correctly
- [ ] Cache expires entries at TTL
- [ ] Zone transfers work between two nodes
- [ ] Wildcard records resolve correctly
- [ ] DNSSEC signatures verify

---

### Track 2B: HTTP/3 Gateway (Days 8-14)

**Files to create:**
```
pkg/services/http/server.go    (REWRITE — HTTP/3)
pkg/services/http/static.go    (NEW — file serving)
pkg/services/http/proxy.go     (NEW — reverse proxy)
pkg/services/http/websocket.go (NEW — WebSocket over QUIC)
pkg/services/http/cert.go      (NEW — auto TLS certs)
```

**Day 8-9: HTTP/3 over QUIC**
- HTTP/3 framing on QUIC streams
- Header compression (QPACK)
- Stream prioritization
- Server push (optional)
- Connection coalescing

**Day 10-11: Static + Reverse Proxy**
- Directory listing (optional)
- MIME type detection (full table)
- Range requests (video/audio streaming)
- Conditional requests (If-Modified-Since, ETag)
- Reverse proxy to backend services
- Load balancing across backends

**Day 12-13: WebSocket + Real-time**
- WebSocket upgrade over QUIC stream
- Binary/text frame support
- Ping/pong keepalive
- Server-Sent Events
- Auto-reconnect

**Day 14: Auto TLS + Integration**
- Self-signed cert generation for .localweb
- Certificate chain verification
- OCSP stapling (local)
- Integration with DNS (A/AAAA records)

**Acceptance Criteria:**
- [ ] HTTP/3 serves static files from ~/LocalWEB/sites/
- [ ] Range requests work for video streaming
- [ ] Reverse proxy forwards to backend services
- [ ] WebSocket connections work over QUIC
- [ ] Auto-generated TLS certs for .localweb domains
- [ ] <100ms first byte for local files

---

### Track 2C: Email (SMTP/IMAP) (Days 8-14)

**Files to create:**
```
pkg/services/email/smtp.go      (NEW — SMTP server)
pkg/services/email/imap.go      (NEW — IMAP server)
pkg/services/email/mailbox.go   (NEW — Maildir storage)
pkg/services/email/antispam.go  (NEW — PoW spam filter)
pkg/services/email/types.go     (NEW — email types)
```

**Day 8-9: SMTP Submission**
- ESMTP greeting + EHLO
- STARTTLS negotiation
- AUTH PLAIN/LOGIN
- MAIL FROM / RCPT TO / DATA
- Message parsing (RFC 5322)
- Local mailbox delivery

**Day 10-11: Inter-node Delivery**
- DHT lookup for recipient's node
- QUIC connection to remote SMTP
- MX record resolution for .localweb
- Queue for offline delivery
- Delivery receipts

**Day 12-13: IMAP Server**
- IMAP4rev2 (RFC 9051)
- LOGIN / SELECT / FETCH / STORE
- Maildir storage backend
- UIDVALIDITY / UIDNEXT
- Flags: \Seen, \Answered, \Flagged, \Deleted

**Day 14: Anti-spam + Integration**
- Proof of Work: SHA3(nonce || sender || payload) < difficulty
- Rate limiting per sender
- Sender reputation scoring
- Address format: user@nodename.localweb

**Acceptance Criteria:**
- [ ] Send email from node A to node B
- [ ] Email arrives in B's mailbox via QUIC
- [ ] IMAP fetches messages correctly
- [ ] PoW spam filter blocks low-effort spam
- [ ] Address resolution works: user@nodename.localweb
- [ ] Offline queue delivers on reconnect

---

## Phase 3: Messaging + File Sync + CRDT (Week 5-6)

### Track 3A: Messaging (Days 15-21)

**Files to create:**
```
pkg/services/messaging/server.go     (REWRITE — full server)
pkg/services/messaging/channel.go    (NEW — channel management)
pkg/services/messaging/store.go      (NEW — message store)
pkg/services/messaging/ratchet.go    (NEW — double ratchet)
pkg/services/messaging/types.go      (REWRITE — enhanced types)
```

**Day 15-16: Channel System**
- Create/delete channels
- Join/leave with policies (open, invite_only, admin_only)
- Channel metadata (name, description, avatar)
- Member management with capabilities
- Channel discovery via DHT

**Day 17-18: E2E Encryption**
- X3DH key agreement (prekey bundles)
- Double Ratchet for forward secrecy
- Per-message key rotation
- Sender keys for group messaging
- Key verification (safety numbers)

**Day 19-20: Message Store + Delivery**
- BadgerDB message storage
- RGA CRDT for message ordering
- Offline message queue (outbox)
- Delivery receipts: sent → delivered → read
- Media attachments via content-addressed storage

**Day 21: Integration + UI**
- WebSocket endpoint for real-time
- Presence tracking (online/typing)
- Message reactions, replies, threads
- Search across message history

**Acceptance Criteria:**
- [ ] Create channel, invite peer, send messages
- [ ] Messages are E2E encrypted (verify with second peer)
- [ ] Offline messages queued and delivered on reconnect
- [ ] Message ordering consistent across peers
- [ ] Media attachments work via CID
- [ ] Delivery receipts work

---

### Track 3B: File Sync (Days 15-21)

**Files to create:**
```
pkg/services/files/files.go     (REWRITE — enhanced)
pkg/services/files/server.go    (REWRITE — full server)
pkg/services/files/sync.go      (NEW — sync engine)
pkg/services/files/fuse.go      (NEW — FUSE mount)
pkg/services/files/store.go     (NEW — block store)
```

**Day 15-16: Block Store**
- BadgerDB CID → block metadata
- Raw block storage in ~/LocalWEB/blocks/
- Zstd compression on blocks > 4KB
- Global deduplication via CID
- Garbage collection for unreferenced blocks

**Day 17-18: Merkle DAG + Sync**
- Build Merkle DAG from file tree
- DAG diff: compare roots, find differing branches
- Bitswap-like block exchange protocol
- Partial sync: download specific files/dirs
- Background sync: push/pull on timer

**Day 19-20: FUSE Mount**
- Read-only FUSE mount (Linux/macOS)
- List files from DAG
- Read blocks on-demand (lazy loading)
- Cache frequently accessed blocks
- Write support: stage changes, then commit

**Day 21: Integration**
- File share: generate CID + ACL
- Peer downloads shared file
- Version history (keep N versions)
- Conflict resolution: LWW-Element-Set CRDT

**Acceptance Criteria:**
- [ ] Store file → CID computed → blocks written
- [ ] Retrieve file by CID → correct content
- [ ] Sync between two nodes → only changed blocks transferred
- [ ] FUSE mount shows remote files
- [ ] Deduplication: same content → same CID → one block stored
- [ ] File versioning works

---

### Track 3C: CRDT Engine (Days 15-21)

**Files to create:**
```
pkg/sync/crdt.go              (NEW — CRDT orchestrator)
pkg/sync/orset.go             (NEW — OR-Set)
pkg/sync/lww.go               (NEW — LWW-Register + Element-Set)
pkg/sync/rga.go               (NEW — RGA for text)
pkg/sync/vector_clock.go      (NEW — vector clock)
pkg/sync/merkle.go            (NEW — Merkle DAG)
pkg/sync/anti_entropy.go      (NEW — anti-entropy protocol)
pkg/sync/store.go             (NEW — encrypted BadgerDB)
```

**Day 15-16: Core CRDTs**
- OR-Set: add/remove with tombstones
- LWW-Register: latest timestamp wins
- LWW-Element-Set: add/remove with timestamps
- Vector clock: causal ordering
- Merge: commutative, idempotent, associative

**Day 17-18: RGA (Text CRDT)**
- Insert operation: (position, char)
- Delete operation: (position)
- Tombstone-based deletion
- Concurrent insert resolution
- Unicode-aware (grapheme clusters)

**Day 19-20: Merkle DAG + Anti-Entropy**
- Build DAG from CRDT state
- Diff: find branches with different hashes
- Delta exchange: only send differing branches
- State merge: apply CRDT rules
- Conflict-free by design

**Day 21: Encrypted Store**
- BadgerDB with per-record encryption
- Key derivation from node identity
- Encrypted backup/export
- Garbage collection for tombstones

**Acceptance Criteria:**
- [ ] OR-Set: concurrent add/remove merges correctly
- [ ] LWW: concurrent writes → latest wins
- [ ] RGA: concurrent text edits merge correctly
- [ ] Vector clock: causal ordering preserved
- [ ] Anti-entropy: two nodes converge after sync
- [ ] Store: all data encrypted at rest

---

## Phase 4: Voice/Video + VPN + Docs (Week 7-8)

### Track 4A: Voice/Video (Days 22-28)

**Files to create:**
```
pkg/services/voice/server.go     (NEW — call server)
pkg/services/voice/codec.go      (NEW — audio/video codecs)
pkg/services/voice/signaling.go  (NEW — call signaling)
pkg/services/voice/track.go      (NEW — media track management)
pkg/services/voice/types.go      (NEW — call types)
```

**Day 22-23: Audio**
- Opus codec integration (48kHz, 32kbps)
- Capture: microphone → Opus frames
- Playback: Opus frames → speaker
- Echo cancellation (speexdsp)
- Noise suppression (RNNoise)
- Jitter buffer

**Day 24-25: Video**
- VP9/AV1 codec integration
- Capture: screen/window → encoded frames
- Decode: encoded frames → display
- Adaptive bitrate (network-aware)
- Screen sharing mode

**Day 26-27: Signaling + Calls**
- Call signaling via messaging channels
- ICE-like candidate gathering
- Call states: ringing → connecting → active → ended
- Group calls: SFU-style (relay through initiator)
- Data channels for text during calls

**Day 28: Integration**
- Call UI via WebSocket dashboard
- Call history
- Missed call notifications
- Network quality indicators

**Acceptance Criteria:**
- [ ] 1:1 voice call works between two nodes
- [ ] Audio quality: clear, no echo, minimal latency
- [ ] Screen sharing works
- [ ] Group calls (3+ participants)
- [ ] Calls work over relay when direct fails
- [ ] Adaptive bitrate adjusts to network

---

### Track 4B: Mesh VPN (Days 22-28)

**Files to create:**
```
pkg/services/vpn/server.go    (NEW — VPN server)
pkg/services/vpn/tunnel.go    (NEW — WireGuard tunnel)
pkg/services/vpn/routing.go   (NEW — IP routing)
pkg/services/vpn/tun.go       (NEW — TUN device)
```

**Day 22-23: TUN Device + WireGuard**
- Create TUN interface (tun0)
- WireGuard handshake (Noise_IK)
- Encrypt/decrypt IP packets
- MTU negotiation (1420 for WireGuard overhead)
- Key generation and management

**Day 24-25: Routing**
- Each node advertises fd00:localweb:<id>/128
- Route table: peer → tunnel
- Route propagation via DHT
- Split tunneling: only LocalWEB traffic
- Default route exclusion

**Day 26-27: NAT Traversal**
- Direct connection: UDP hole punching
- Relay fallback: through intermediate peer
- Keepalive packets (every 25s)
- Handshake retry on failure

**Day 28: Integration**
- `localweb vpn status` command
- Traffic stats per tunnel
- Kill switch (block traffic if VPN down)
- DNS resolution through VPN

**Acceptance Criteria:**
- [ ] Two nodes communicate via TUN interface
- [ ] WireGuard encryption verified (packet capture)
- [ ] Routing works: ping fd00:localweb:<remote_id>
- [ ] NAT traversal works with relay fallback
- [ ] Keepalive maintains connection
- [ ] Traffic stats accurate

---

### Track 4C: Collaborative Docs (Days 22-28)

**Files to create:**
```
pkg/services/docs/server.go    (NEW — doc server)
pkg/services/docs/rga.go       (NEW — RGA CRDT for text)
pkg/services/docs/presence.go  (NEW — editor presence)
pkg/services/docs/types.go     (NEW — document types)
```

**Day 22-23: Document Model**
- Document = RGA CRDT for text
- Operations: insert(char, pos), delete(pos)
- Operations broadcast to all editors
- State sync for new joiners
- Metadata: title, author, timestamps

**Day 24-25: Real-time Collaboration**
- WebSocket endpoint for live editing
- Operation broadcast via QUIC
- Presence: cursor positions, selections
- Typing indicators
- Conflict resolution: RGA guarantees convergence

**Day 26-27: Rich Text + History**
- Block types: paragraph, heading, list, code, quote
- Inline formatting: bold, italic, code, link
- Full edit history (operation log)
- Undo/redo via operation reversal
- Export: Markdown, HTML

**Day 28: Integration**
- Create/edit/delete documents
- Share documents via capability tokens
- Document listing and search
- Offline: local edits queued, merged on connect

**Acceptance Criteria:**
- [ ] Two users edit same document simultaneously
- [ ] Concurrent edits merge correctly (no conflicts)
- [ ] Presence shows cursor positions
- [ ] History shows full edit log
- [ ] Export to Markdown works
- [ ] Offline edits merge on reconnect

---

## Phase 5: App Registry + Integration (Week 9-10)

### Track 5A: App Registry (Days 29-35)

**Files to create:**
```
pkg/services/registry/server.go   (NEW — registry server)
pkg/services/registry/package.go  (NEW — package format)
pkg/services/registry/verify.go   (NEW — signature verification)
```

**Day 29-30: Package Format**
- .lwpkg format: tar.gz + Ed25519 signature
- manifest.yaml: name, version, deps, binaries, checksums
- Package signing with node identity
- Verification on install

**Day 31-32: Registry Server**
- HTTP endpoint for package listing
- Upload/download packages
- Dependency resolution via DHT
- Package search
- Version management

**Day 33-35: CLI Integration**
- `localweb pkg install <name>`
- `localweb pkg list`
- `localweb pkg search <query>`
- `localweb pkg publish <path>`
- Auto-update check

**Acceptance Criteria:**
- [ ] Publish package → appears in registry
- [ ] Install package → binary downloaded + verified
- [ ] Dependency resolution works
- [ ] Package search returns results
- [ ] Signature verification catches tampering

---

### Track 5B: Integration Testing (Days 29-35)

**Files to create:**
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

**Day 29-30: Unit Tests**
- Crypto operations
- CRDT merge correctness
- DNS parsing
- Protocol framing
- Store operations

**Day 31-32: Integration Tests**
- Two-node discovery
- DHT routing across 5 nodes
- DNS resolution end-to-end
- HTTP file serving
- Email send/receive
- Message delivery
- File sync
- Call signaling

**Day 33-35: Full Stack Test**
- Start 5-node network
- All services running
- Cross-service operations
- Offline/online transitions
- Failure recovery
- Performance benchmarks

**Acceptance Criteria:**
- [ ] All unit tests pass
- [ ] Integration tests pass with 2 nodes
- [ ] Full stack test passes with 5 nodes
- [ ] Benchmarks meet performance targets
- [ ] No data races (go test -race)
- [ ] Coverage >= 80%

---

## Phase 6: Testing + Polish + Release (Week 11-12)

### Track 6A: Performance + Security (Days 36-42)

**Day 36-37: Performance**
- Benchmark all services
- Profile CPU/memory
- Optimize hot paths
- Connection pooling
- Request batching

**Day 38-39: Security Audit**
- Crypto review (key management)
- Protocol fuzzing
- Input validation
- Rate limiting
- Capability enforcement
- Audit logging

**Day 40-42: Hardening**
- Graceful shutdown for all services
- Resource limits (memory, connections, storage)
- Error recovery and retry logic
- Logging and observability
- Health checks

---

### Track 6B: Documentation + Release (Days 36-42)

**Day 36-37: Documentation**
- API documentation
- Protocol specifications
- Configuration reference
- Troubleshooting guide
- Architecture diagrams

**Day 38-39: CLI + Dashboard**
- All CLI commands working
- Web dashboard functional
- Status/monitoring endpoints
- Help text and examples

**Day 40-42: Release**
- Cross-compile: Linux, macOS, Windows
- Release binaries
- Installation scripts
- Migration guide from v0.1
- Changelog

---

## Build System

```makefile
# Makefile

.PHONY: build test lint cross-compile generate

# Build
build:
	go build -o bin/localweb-node ./cmd/node
	go build -o bin/localweb ./cmd/cli

# Test
test:
	go test ./... -v -count=1
	go test -race ./...
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

# Benchmarks
bench:
	go test -bench=. -benchmem ./...

# Lint
lint:
	golangci-lint run
	go vet ./...
	go fmt ./...

# Cross-compile
cross-compile:
	GOOS=linux GOARCH=amd64 go build -o bin/localweb-linux-amd64 ./cmd/node
	GOOS=linux GOARCH=arm64 go build -o bin/localweb-linux-arm64 ./cmd/node
	GOOS=darwin GOARCH=arm64 go build -o bin/localweb-macos-arm64 ./cmd/node
	GOOS=darwin GOARCH=amd64 go build -o bin/localweb-macos-amd64 ./cmd/node
	GOOS=windows GOARCH=amd64 go build -o bin/localweb-windows-amd64.exe ./cmd/node

# Proto generation
generate:
	protoc --go_out=. --go_opt=paths=source_relative api/proto/messages.proto

# Clean
clean:
	rm -rf bin/ coverage.out data/
```

---

## Risk Analysis

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| QUIC library instability | HIGH | Medium | Pin quic-go version, fallback to TCP |
| mDNS blocked on some networks | MEDIUM | High | ARP scan + SSDP fallback |
| NAT traversal failure | HIGH | Medium | Circuit relay as always-available fallback |
| FUSE not available (Windows) | LOW | Medium | Dokany driver, or virtual filesystem |
| WireGuard kernel issues | HIGH | Low | Userspace WireGuard (golang.zx2c4.com/wireguard) |
| Opus codec performance | LOW | Low | CPU encoding is fast enough for real-time |
| BadgerDB corruption | HIGH | Low | WAL mode, regular backups |
| Memory usage too high | MEDIUM | Medium | Profiling, lazy loading, GC tuning |

---

## Success Criteria for v1.0

| Metric | Target |
|--------|--------|
| Two laptops discover each other | <500ms on same WiFi |
| DNS resolution | Working end-to-end |
| HTTP file serving | Working with HTTP/3 |
| Email delivery | Send/receive between nodes |
| Message delivery | E2E encrypted, <50ms LAN |
| File sync | Block-level, <30s for 1GB |
| Voice call | Clear audio, <100ms latency |
| Collaborative docs | Real-time, conflict-free |
| VPN tunnel | Working WireGuard-compatible |
| All services | Running simultaneously |
| Test coverage | >= 80% |
| Cross-platform | Linux, macOS, Windows |

---

*Last updated: 2026-09-04*
*Author: Mrityunjay K*

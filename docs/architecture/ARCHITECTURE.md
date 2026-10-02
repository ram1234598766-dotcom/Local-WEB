# LocalWEB — System Architecture

**Status: reflects `main`, covering two correctness passes (described in §9) plus
the packaging and GUI-HTTP work in §9.1.** Figures are re-measured rather than
remembered; where a defect was found against a specific earlier commit, that hash
is named in place.
**Module: `github.com/ram1234598766-dotcom/Local-WEB` | Go 1.26 | Author: Mrityunjay K**

> **How to read this document.** Every claim below is tagged with its evidence
> and its verification state. Nothing is marked working unless a test exercises
> it.
>
> - ✅ **Verified** — implemented and covered by a named passing test.
> - ⚠️ **Partial** — real code, but a documented property does not hold.
> - ❌ **Not implemented** — no code, or a stub that does nothing.
>
> The previous revision of this document described a system roughly one build
> generation ahead of what existed: it claimed 27 Prometheus metrics (none
> registered), 9 pinned WebRTC/eBPF/netlink dependencies (none in `go.mod`), 12
> chaos scenarios (6 exist), and "TLA+ Verified" against `.tla` files that do
> not exist in the repository. §9 records what was corrected.

---

## 1. What actually ships

`cmd/node/main.go` is the honest measure of the product. On startup it:

1. loads or generates a persistent Ed25519 identity,
2. derives a store encryption key from that identity,
3. opens an AES-256-GCM-encrypted BadgerDB,
4. starts the link manager and discovery orchestrator,
5. starts a QUIC v1 listener with a Noise XX handshake,
6. serves an embedded web GUI + REST API on `:8080`.

**None of the nine protocol services is started by the daemon.** Each service
package is real, tested code, but `cmd/node` registers only the `Control`
handler on the transport. This is the single most important correction to the
previous documentation, which described all nine as running.

```
                  ┌──────────────────────────────┐
   browser ──────▶│  L9  GUI + REST API  :8080   │
                  │  (pkg/gui, embedded SPA)     │
                  └───────────────┬──────────────┘
                                  │
                  ┌───────────────▼──────────────┐
   peers ────────▶│  L1  TRANSPORT               │
       QUIC       │  QUIC v1 + Noise XX (+PQ)    │  ← only Control wired
                  │  1-byte ServiceID mux        │
                  └───────────────┬──────────────┘
                                  │
                  ┌───────────────▼──────────────┐
                  │  L3  DISCOVERY  mDNS-SD      │
                  └───────────────┬──────────────┘
                                  │
                  ┌───────────────▼──────────────┐
                  │  L2  LINK   WiFi/adhoc/USB    │
                  └───────────────┬──────────────┘
                                  │
                  ┌───────────────▼──────────────┐
                  │  L6  STORE  BadgerDB+AEAD    │
                  └──────────────────────────────┘

   L8 services: six of nine now started by the daemon
     (dns, http, email, files, docs, registry);
     messaging, voice and vpn have nothing to start.
   Still library-only, NOT started by the daemon:
     L4 security  L5 DHT    L7 CRDT
```

### 1.1 Layer status at a glance

| Layer | Component | Status | Evidence |
|---|---|---|---|
| L1 | QUIC v1 transport | ✅ Verified | `pkg/transport/quic_test.go:16` `TestTransportRoundTrip` |
| L1 | Noise XX handshake | ✅ Verified | `pkg/transport/quic_test.go:117` `TestConnectIdentityMismatch` |
| L1 | Hybrid PQ handshake (X25519+Kyber) | ✅ Verified | `pkg/transport/hybrid_test.go` `TestHybridSessionKeyDependsOnKyber`, `TestHybridServerHandshakeAgreesOnSessionKey` |
| L1 | 1-byte ServiceID stream mux | ✅ Verified | `pkg/transport/quic.go:209-226` dispatch, `quic.go:579` write |
| L1 | Congestion control config (CUBIC/BBR) | ❌ Not implemented | `quic.Config` at `quic.go:100` sets only streams/timeout; no `CongestionController` anywhere |
| L1 | 0-RTT + replay protection | ❌ Not implemented | no `Allow0RTT` / `MaxTokenAge` / anti-replay cache |
| L1 | QUIC datagram frames | ❌ Not implemented | no `SendDatagram` call site |
| L1 | Circuit relay | ⚠️ Partial | `relay.go:64` `AcceptCircuit` has no caller; pump logic is real |
| L1 | NAT traversal (hole punch / ICE / relay) | ⚠️ Partial | `transport/nat.go:91` `HolePunch` is real but uncalled; `DetectNAT` at `nat.go:73` infers NAT type from the local IP only; **no ICE, no STUN** |
| L2 | WiFi Station | ⚠️ Partial | `pkg/link/wifi.go:156` `findWiFiInterface` returns the first multicast IPv4 interface — Ethernet included |
| L2 | WiFi Direct | ⚠️ Partial | shells out to `wpa_cli`; `listenLinuxEvents` (`wifi_direct.go:266`) is a 1s sleep loop, so `scanPeers` always returns empty; Windows unimplemented |
| L2 | Ad-hoc (IBSS) | ⚠️ Partial | `adhoc.go:210` real `iw dev … ibss join` on Linux; discovery is a TCP probe, not IBSS peer discovery |
| L2 | USB tether | ⚠️ Partial | `usb.go:174` matches interface-name prefixes `usb`/`enx`/`enp`; no RNDIS detection; no-op on macOS/Windows |
| L2 | BLE | ❌ Stub | `ble.go:364` `newBLEAdapter` returns `powered:true` unconditionally; `Connect` returns "not yet implemented"; `IsAvailable` returns true on every platform |
| L2 | Acoustic FSK | ❌ Not implemented | no `acoustic.go`; only the `ModeAcoustic` enum value exists |
| L2 | Ethernet | ❌ Not implemented | no `ethernet.go` |
| L2 | Link quality estimation (Kalman/EWMA) | ❌ Not implemented | no `quality.go`; only `manager.go:286` `computeScore` with fixed bonuses |
| L2 | Multi-path: failover / weighted-latency | ✅ Verified | `pkg/link/multipath_test.go` `TestMultiPathHandleEventWithNoAddresses`, `TestNewMultiPathManagerReachesFailoverThroughTheConstructor`, `TestMultiPathPeerLostDoesNotDeadlock` |
| L2 | Multi-path: round-robin / weighted-BW | ⚠️ Partial | `multipath.go:302-327` rotates a primary pointer; `SendToPeer` duplicates bytes to every link rather than distributing |
| L2 | Multi-path: RLNC, MPTCP | ❌ Not implemented | no `rlnc` or `mptcp` symbol in the tree |
| L3 | mDNS-SD discovery | ✅ Verified | `pkg/discovery/mdns_test.go` 40 tests incl. `TestBuildAnnounceRoundTripPreservesPeerID`, `TestParseMDNSResponseTruncatedRecordData` |
| L3 | mDNS parser is safe on hostile input | ✅ Verified | was a **LAN-reachable panic**; see §9 item 21. `TestParseDNSNameTruncatedLabel`, `TestAppendDNSAnswerZeroLengthData`, `TestParseMDNSResponseTruncatedRecordData`, `TestBuildTXTRecordLengthPrefixMatchesPayload` |
| L3 | TTL eviction / GC | ✅ Verified | `types_test.go` `TestPeerDatabaseGCBoundary`, `TestPeerDatabaseGCEvictsStalePeers` |
| L3 | Score responds to its inputs | ✅ Verified | `discovery_test.go` `TestComputeScoreIsNotConstant` |
| L3 | Rendezvous / federation | ⚠️ Partial | `pkg/federation` HTTP/1.1 JSON, not HTTP/3; the poll loop (`rendezvous_discovery.go:92-115`) only re-registers, never performs a lookup |
| L3 | Local UDP broadcast discovery | ❌ Not implemented | no broadcast mode |
| L3 | Bayesian scoring | ⚠️ Partial | `computeScore` is fixed-bonus heuristics; `old *PeerInfo` parameter is never read, so there is no recency term |
| L3 | Byzantine-resilient merge | ❌ Not implemented | no `ByzantineMerge`, no median-of-means, no IQR |
| L4 | Ed25519 identity + NodeID | ✅ Verified | `pkg/crypto/crypto_test.go`; `NodeID = SHA3-256(pubkey)` |
| L4 | Capability tokens | ⚠️ Partial | `security/capability.go:17` is a signed JSON blob, **not a Macaroon**: no identifier, no caveat chain, no CBOR, no delegation; revocation is in-memory and lost on restart |
| L4 | Argon2id PoW (pkg/security) | ✅ Verified | `security/pow_test.go` 24 tests; see §3 |
| L4 | Ed448 / Dilithium3 / ML-DSA-65 | ⚠️ Partial | implemented in `pkg/crypto/crypto.go:64-197` via circl, but zero call sites outside the package |
| L4 | Audit log hash chain + tamper detection | ✅ Verified | `security/audit_test.go` `TestAuditLogTamperDetection` |
| L5 | Kademlia k=20, α=3 | ✅ Verified | `pkg/dht/dht_test.go` `TestRoutingTableDistributesPeersAcrossBuckets`, `TestXorDistUsesFullKeyspace` |
| L5 | Iterative lookup | ✅ Verified | `dht.go` `dedupeAndPrune`; `TestDedupeAndPruneSortsDeduplicatesAndCaps` |
| L5 | PoW anti-Sybil enforcement | ✅ Verified | `server.go` `MsgRegisterNode` case; `TestHandleRegisterNodeRejectsInvalidProofOfWork` (5 sub-cases) |
| L5 | Bucket refresh / split | ❌ Not implemented | 256 static buckets; no split, no refresh, no republish |
| L5 | Recursive lookup | ❌ Not implemented | iterative only |
| L5 | Stale-peer eviction | ✅ Verified | `PruneStale`; `TestPruneStaleRemovesDepartedPeers` |
| L6 | BadgerDB + AES-256-GCM at rest | ✅ Verified | `pkg/store/store_test.go`; key derived from the identity seed (`crypto/storage.go:75`), no hardcoded fallback |
| L6 | CIDv1 content addressing + re-hash on read | ✅ Verified | `pkg/store/block_store.go:35-75` |
| L6 | 6 documented sub-stores (`b/ p/ f/ d/ c/ a/`) | ❌ Not implemented | real prefixes are `LWS:block:`, `LWS:meta:`, `LWS:peer:` only |
| L6 | zstd/snappy block compression | ❌ Not wired | `files/store.go:197` `compressBlock` has only a test caller |
| L7 | OR-Set (add-wins) | ✅ Verified | `pkg/crdt/crdt_test.go`; `test/integration/setup_test.go` `TestCRDTOperations` |
| L7 | RGA | ⚠️ Partial | insert-after-parent now works (`TestCRDTPrependViaHeadSentinel`), but `Merge` at `crdt.go:272` appends missing nodes rather than performing a positional CRDT merge |
| L7 | LWW-Register | ⚠️ Partial | real LWW with (timestamp, author) tiebreak, but the timestamp is wall-clock, so clock skew breaks convergence |
| L7 | Merkle tree | ⚠️ Partial | `MerkleTree` is a flat leaf list, not a DAG; `DiffMerkle` is O(n) set difference |
| L7 | Delta-CRDT, PN-Counter | ❌ Not implemented | no such symbol in the tree |
| L7 | Tombstone GC | ❌ Not implemented | `ORSet.removes` and `RGANode.Deleted` grow unbounded |
| L8 | Nine service packages | ✅ Verified as libraries | `pkg/services/*_test.go`; **not instantiated by the daemon** |
| L8 | Service mesh (LB / breaker / retry / OTel) | ❌ Not implemented | no such code |
| L9 | GUI: `/api/status`, `/api/peers`, `/api/audit-log` | ✅ Verified | `pkg/gui/` tests. The SPA itself **did not parse until this pass** — see §9.1 item 29 — so these endpoints had never actually been reached from a browser |
| L9 | GUI: `/metrics`, `/debug/pprof` | ❌ Not implemented | `handler.go:33-58` registers neither |
| L9 | Plugin manager + built-in plugins | ✅ Verified | `pkg/plugin/` 58 tests; nil-logger panic, double-`Start` race, ignored router prefix and swallowed bind error fixed in §9 items 25-26 |
| L9 | Go `.so` plugin loading, WASM/WASI | ❌ Not implemented | `plugin.go:248` returns "not implemented"; no `wasm` directory |
| L9 | Plugin capability sandbox | ❌ Not implemented | `Host` grants unrestricted store/transport/security |
| — | Chaos: loss / latency / corruption / partition | ✅ Verified | `pkg/chaos/runner_test.go` `TestChaosFaultsAreReversible`, `TestChaosConnDuplicatesRead` |
| — | Chaos: StopAll | ✅ Verified | `TestStopAllCancelsRunningScenario` |

---

## 2. L1 — Transport, in detail

### 2.1 Noise XX

`pkg/crypto/noise.go`. Pattern XX over X25519 with SHA3-256 and
XSalsa20-Poly1305. Both sides derive the same directional keys; the tests assert
`initiator.SendKey == responder.RecvKey` and the reverse, and that the two
directions differ.

```
-> e
<- e, ee, s, es
-> s, se
```

Known limitations, stated because they are properties the previous document
claimed and did not have:

- **No handshake replay protection.** A captured msg1/msg2 pair is not
  rejected by a nonce cache.
- **All-zero AEAD nonce during the handshake** (`noise.go:320`, `:335`).
  Tolerable only because `k` rotates per token; this is not the Noise spec's
  discipline.
- **No KCI resistance evidence.** No model checker runs in this repository.

### 2.2 Hybrid post-quantum handshake

Enabled with `--hybrid`. Off by default.

```
msg1 (initiator) = initiator_kyber_pub (1568 B) ‖ Noise "-> e"        (32 B)
msg2 (responder) = kyber_ct           (1568 B) ‖ Noise "<- e,ee,s,es" (80 B)
msg3 (initiator) =                            Noise "-> s, se"      (48 B)

session_key = HKDF-SHA3-256( ikm = noiseKey,
                             salt = kyberSS,
                             info = "LocalWEB-v2-session" )
```

Only the initiator's PQ public key crosses the wire. The responder encapsulates
to it and keeps the shared secret; the initiator decapsulates the responder's
ciphertext and recovers the same secret. Because the Kyber encapsulation is
randomised, two handshakes between the same long-term keys yield different
session keys — asserted by `TestHybridSessionKeyDependsOnKyber`, which fails if
the KEM stops contributing.

**Scope of the PQ guarantee, stated precisely.** The QUIC/TLS 1.3 record layer
is what protects traffic in flight, and quic-go owns those keys. The hybrid
session key is *not* a QUIC record key. It is exposed as
`Connection.SessionKey()` / `Connection.PeerRecvKey()` so an application layer
can derive per-service keys from it via
`HybridKeyDerivation.DeriveTransportKey(sessionKey, context)` — HKDF-SHA3-256
with the context as domain separation, so a key derived for `files` is not
usable for `vpn`. Nothing in the repository derives such a key yet. The honest
claim is: *the post-quantum KEM is correctly wired and its output is verifiably
part of the session key; it does not yet protect the QUIC record layer.*

### 2.3 Ed25519 identity vs X25519 transport key

The node identity is Ed25519; the Noise handshake is X25519. The daemon
converts them at startup (`cmd/node/main.go`):

```go
transportPub, err := crypto.Ed25519PublicToX25519(pub)
transportPriv := crypto.Ed25519PrivateToX25519(priv)
```

Without this conversion the transport "works" — X25519 accepts any 32 bytes as
a scalar — but the identity a peer derives from the Noise static key is
unrelated to the identity the node advertises, so peers cannot match the two.

---

## 3. Proof of work

Two independent, deliberately different schemes. This is a design choice, now
documented, not an accident.

### 3.1 `pkg/security` — memory-hard, for service-level gating

Purpose: keep spammers from reaching Email/Docs at zero cost.

- `workSeed = Argon2id(challenge, salt, t=1, m=64 MiB, p=1, 32)` — paid **once**
  per challenge by the solver and **once** by the verifier.
- A solution is valid when `SHA3-256(workSeed ‖ nonce)` has at least
  `Difficulty` **leading zero bits**. Work is `2^difficulty` hashes.
- Difficulty range `[8, 24]`, base 16, default target solve time 100 ms.
- Replay: a challenge digest is accepted at most once per 5-minute window; the
  cache is bounded at 1024 entries and evicts oldest-first.

**Why the Argon2id pass is not in the nonce loop.** The previous implementation
ran Argon2id — 64 MiB, `2^difficulty` iterations — once *per nonce*, and used
`difficulty` both as the log2 time cost and as a count of leading zero
*bytes*. At difficulty 3 that is `2^24` iterations of a 64 MiB memory-hard
function: unreachable in practice. The consequence was not theoretical — the
package's own test suite could not finish:

```
FAIL  github.com/ram1234598766-dotcom/Local-WEB/pkg/security   600.983s
      panic: test timed out after 10m
      goroutine: security.SolvePoW at pkg/security/pow.go:93
```

After the fix the same package runs in 2.4 s.

**DoS control.** A challenge is attacker-controlled data. `clamp()` bounds the
memory, iteration count and lane count *before* any allocation, so a peer cannot
make the verifier allocate a terabyte. `TestChallengeCostIsClampedAgainstDoS`
asserts this against a challenge requesting 1 TiB / 2^20 iterations / 255
lanes.

### 3.2 `pkg/dht` — bare SHA3-256, for DHT registration

Purpose: make Sybil registration cost CPU. Registration work happens on every
announcement, so a 64 MiB allocation per announcement would be unusable; this
uses a plain SHA3-256 search over `pubKey ‖ name ‖ nonce` with difficulty in
bits, bounded to `[8, 24]`.

The work is GPU-friendly. That is the trade for making announcement cheap, and
it is stated here rather than implied.

**Enforcement.** `server.go` `handleMessage` verifies the solution *and*
requires `msg.Src == NodeIDFromPub(pubKey)`, so a node cannot reuse another
node's published nonce. A failed challenge returns the same `Pong` as a
successful one, so the responder does not confirm it is a registration oracle.
Both behaviours are pinned by `TestHandleRegisterNodeRejectsInvalidProofOfWork`.

---

## 4. L5 — DHT

`pkg/dht`. Kademlia, `k = 20`, `α = 3`, max 15 hops.

### 4.1 Routing table

```
bucket(id) = buckets[ index of the first set bit in id ]
```

256 buckets, `k = 20` each. `FindClosest` gathers every peer, sorts **globally**
by full 256-bit XOR distance, then truncates.

Three defects were fixed here, each with a regression test that fails against
the old code:

| Defect | Effect | Test |
|---|---|---|
| `PrefixLen()` computed `id.Xor(id)` — always zero | Every peer landed in bucket 0; the table filled to 20 then silently rejected the whole network | `TestRoutingTableDistributesPeersAcrossBuckets` — old code: 9 peers → **1 bucket** |
| `xorDist` truncated 256 bits to the low 64 | Two IDs differing only in leading bytes compared equal; ordering used least-significant bytes | `TestXorDistUsesFullKeyspace` |
| `FindClosest` concatenated per-bucket results in bucket order | A distant peer in a low bucket could displace a nearer one | `TestFindClosestSortsGlobally` |

Churn resistance is `RoutingTable.PruneStale(ttl)`
(`TestPruneStaleRemovesDepartedPeers`): without it a bucket full of departed
nodes rejects every new peer forever. There is still **no bucket split and no
refresh timer**, so table quality degrades in a large network.

### 4.2 Iterative lookup

Each round queries the α closest unqueried peers, folds responses into a
shortlist of the `k` closest seen, then re-sorts and prunes. Previously the
frontier was appended to every hop with no re-sort or prune, so the query set
grew monotonically and the lookup degenerated into a broadcast.

### 4.3 Not implemented

Bucket refresh/split, recursive lookup, adaptive α, erasure-coded replication.

---

## 5. L7 — CRDT

`pkg/crdt`, one file.

| Type | Status | Notes |
|---|---|---|
| OR-Set (add-wins, dot-based) | ✅ | `Merge` unions adds and removes |
| RGA | ⚠️ | Insert-after-parent is correct (`findNode` now resolves the head sentinel, so prepending works). `Merge` appends missing nodes in causal order instead of merging positionally — see FINDINGS |
| LWW-Register | ⚠️ | Wall-clock timestamp, not a Lamport clock; `Unmarshal` drops `Author` |
| Merkle tree | ⚠️ | Flat leaf list + root, not a DAG; `DiffMerkle` is O(n) set difference |
| Delta-CRDT, PN-Counter | ❌ | Absent |
| Tombstone GC | ❌ | Not implemented; tombstones grow unbounded |

**On convergence.** There is no two-replica test that applies the same
operations in *different orders* and asserts identical state — the property the
previous document called "Strong Eventual Consistency Theorem". OR-Set's merge
is commutative and associative by construction and is exercised by
`TestCRDTOperations`; RGA's is not, and writing the RGA convergence test is
tracked in §8.

---

## 6. Data flow: discovery → connection

```
1. Discover      every available link reports peers it can see
2. Score         computeScore(freshness, latency, recency) — see L3 caveat
3. Select        keep peers above the threshold
4. Dial          for each candidate, in parallel, try links best-first
5. Handshake     QUIC v1 → Noise XX (→ hybrid XX + Kyber when enabled)
6. Verify        compare the peer's NodeID against the expected identity
7. Register      store the Connection and accept its service streams
```

Step 6 is the security boundary: `Connect` closes the connection with a
`peer identity mismatch` application error when the authenticated NodeID does
not equal the one requested (`quic.go` `TestConnectIdentityMismatch`).

---

## 7. Threat model (STRIDE)

| Threat | Mitigation | Status |
|---|---|---|
| Spoofing | `NodeID = SHA3-256(Ed25519 pubkey)`, verified on every connection; Noise XX mutual auth | ✅ |
| Tampering | Noise AEAD per message; SHA3-256 hash-chained audit log | ✅ `TestAuditLogTamperDetection` |
| Repudiation | Ed25519 signatures; tamper-evident audit chain | ⚠️ audit log is in-memory only and lost on restart |
| Info disclosure | All traffic over QUIC/TLS 1.3; Noise XX beneath it | ⚠️ TLS cert verification is disabled by default (`InsecureSkipVerify: !enforceTLS`, `quic.go:379`) |
| DoS | PoW on DHT registration and Email; Argon2 cost clamped to hard caps | ✅ after the first pass |
| DoS | mDNS-SD parses **untrusted packets from any LAN host** | ✅ **was an unauthenticated remote panic.** Fixed in the second pass; see §9 item 21 and §9.1. The 1.0.0 release binaries are still affected. |
| Elevation | Capability tokens with expiry and revocation | ⚠️ no caveats, no attenuation, no DHT-distributed revocation |

---

## 8. Open items

Ranked. Each is a real gap, not a documentation nit.

| # | Item | Severity |
|---|---|---|
| 1 | **Resolved: six of the nine services now start.** `cmd/node` builds and starts dns, the HTTP gateway, SMTP/IMAP, the Files store, Docs and the registry index, and reports each one's real reachability through `/api/services/health`. `messaging`, `voice` and `vpn` stay down because they have no listener, no codec and no TUN loop respectively. | Fixed |
| 2 | `/metrics` and `/debug/pprof` do not exist; 0 of the 27 previously documented Prometheus metrics are registered. | Critical (observability) |
| 3 | TLS certificate verification is off by default. Peer identity is authenticated by Noise XX, so this is defence-in-depth rather than the primary control, but it should be opt-out rather than opt-in. | High |
| 4 | **Resolved: RGA `Merge` is positional and convergent.** It appended every unknown node at the tail and discarded each node's causal predecessor, so replicas that applied the same operations in different orders never agreed. | Fixed |
| 5 | Capability tokens are not Macaroons: no caveats, no attenuation, in-memory revocation. | High |
| 6 | Audit log is in-memory and not persisted, so it does not survive a restart. | High |
| 7 | DHT has no bucket refresh or split; table quality degrades. | Medium |
| 8 | BLE, acoustic FSK and Ethernet links are stubs or absent; `BLE.IsAvailable()` returns true unconditionally, so the daemon believes BLE is up. | Medium |
| 9 | LWW-Register uses wall-clock time, so clock skew breaks convergence. | Medium |
| 10 | No CRDT tombstone GC. | Medium |
| 11 | No two-replica different-order convergence test for RGA. | Medium |
| 12 | Circuit relay and hole punching are implemented but unreachable — no caller. | Medium |
| 13 | Zero-RTT, datagram frames, congestion-control selection not implemented. | Low |
| 14 | The GUI API is documented as "localhost-only read-only dashboard" but `cmd/node/main.go:217` binds `0.0.0.0:8080`, so every `/api/*` endpoint is reachable from the LAN. Whether that is intended is a product decision: a P2P tool may deliberately allow LAN access to the dashboard, but the comment and the bind address currently contradict each other. | Medium |

---

## 9. What this revision corrected

Each item below was a false or broken claim in the previous document, fixed in
code with a test that fails against the previous behaviour.

| # | Claim | Reality found | Fix | Test that pins it |
|---|---|---|---|---|
| 1 | "PoW-V2: Argon2id t=3 m=64MB p=4, difficulty = leading zero bytes" | Unsolvable above difficulty 1; suite timed out at 600 s | Difficulty in bits, Argon2id once per challenge as the seed, SHA3-256 per nonce | `TestSolvePoWMeetsAdvertisedDifficulty`, `TestSolvePoWIsBoundedInTime` |
| 2 | "PoW rejects below difficulty" (DHT anti-Sybil) | Solved by the client, **never checked** by the server; `MsgRegisterNode` fell through to `default` | Server-side verification + identity binding | `TestHandleRegisterNodeRejectsInvalidProofOfWork` (5 sub-cases) |
| 3 | Kademlia routing table | `PrefixLen()` XORed the ID with itself → **every peer in bucket 0** | Correct first-set-bit index | `TestRoutingTableDistributesPeersAcrossBuckets` |
| 4 | Kademlia XOR distance | Truncated to 64 of 256 bits | Full 256-bit comparison | `TestXorDistUsesFullKeyspace` |
| 5 | "Hybrid PQ: SessionKey = HKDF(Classical ‖ PQ)" | Kyber ran, then `DeriveTransportKey` returned its input unchanged; `SessionKey()` had no callers | Real HKDF with domain separation; session key returned and exposed on `Connection` | `TestDeriveTransportKeySeparatesContexts`, `TestHybridSessionKeyDependsOnKyber` |
| 6 | "Hybrid PQ protects the session" | Both sides encapsulated to their **own** key, so both decapsulations were implicit-rejected garbage and the sides derived **different** session keys | Initiator's PQ key sent in msg1; responder encapsulates to it | `TestHybridSessionKeyDependsOnKyber` (fails when re-broken) |
| 7 | "Signed `.localweb` zone; forged records rejected" | `zoneCanonical()` iterated a Go map → non-deterministic pre-image; `Signer`/`Sig` never set, so verification was dead code | Deterministic canonical form covering every field; `SignZone` | `TestZoneCanonicalIsDeterministic`, `TestForgedRecordIsRejected` |
| 8 | "DHT churn resistance" | No eviction; a full bucket rejected all new peers permanently | `PruneStale` | `TestPruneStaleRemovesDepartedPeers` |
| 9 | "12 chaos scenarios", reversible injection | 6 scenarios; cleanup loop was empty, so faults were permanent; duplication injected nothing | 6 honest scenarios, reversible fault state, real duplication | `TestChaosFaultsAreReversible`, `TestChaosConnDuplicatesRead` |
| 10 | `StopAll` stops running scenarios | `cr.running` was only ever deleted from, so `StopAll` iterated an empty set | Register the cancel func on start | `TestStopAllCancelsRunningScenario` |
| 11 | "Services health" endpoint | Returned all 9 services `true` while the daemon started none | Reports real component state | — (covered by the endpoint change) |
| 12 | `/api/dns/records` | Panicked on any peer with no addresses | Skip address-less peers; `Verified` now honest | — |
| 13 | "500+ unit, 25+ integration tests" | 17 integration tests sat in `setup.go`, a **non-test file**, so they never executed | Renamed to `setup_test.go` + `//go:build integration` — 17 tests now run | `test/integration/setup_test.go` |
| 14 | RGA insert-after-parent | `findNode` skipped the head sentinel, so every `Insert("head", …)` silently became an append | Head sentinel resolves | `TestCRDTPrependViaHeadSentinel` |
| 15 | Node identity / transport key | Ed25519 bytes fed straight into the X25519 Noise layer | Convert via `Ed25519PublicToX25519` / `Ed25519PrivateToX25519` | `cmd/node/main_test.go` |
| 16 | systemd `ExecStart=… node --data-dir …` | `flag.Parse()` stops at `node`, so `--data-dir` was **discarded** and the node used the default path | Daemon strips a leading verb; unknown positionals now fail loudly | `TestStripLeadingSubcommandDropsVerbBeforeFlags` |
| 17 | systemd unit | `Type=notify` with no sd_notify code; `Requires=wintun.service` on Linux; six duplicated keys | `Type=simple`, wintun removed, duplicates collapsed | — |
| 18 | 27 Prometheus metrics | 0 registered | Documented as absent rather than claimed | — |
| 19 | 9 pinned WebRTC/eBPF/netlink/wifi deps | All 9 absent from `go.mod` | Documented as absent | — |
| 20 | "TLA+ Verified", `specs/*.tla` | No `.tla` file in the repo; no model checker configured | Removed; the spec is prose, and §1.2 notes what is *not* formally verified | — |

### 9.1 Second correctness pass, and packaging

The first pass (§9 items 1–20) was written against `df30121`. A second pass found
**seven** further defects, four of them reachable from another host, then a
documentation-completeness pass found four more — the last of which meant the
GUI had never loaded at all. Each is pinned by a test that fails against the
previous behaviour.

| # | Defect | Effect | Fix | Test that pins it |
|---|---|---|---|---|
| 21 | **mDNS-SD parser indexed and sliced attacker-controlled lengths** | A malformed packet from any host on the LAN could panic the node — an unauthenticated denial of service. **This is the most serious defect found in this audit.** | Length and bounds checks before every index/slice; zero-length record data tolerated | `TestParseDNSNameTruncatedLabel`, `TestAppendDNSAnswerZeroLengthData`, `TestParseMDNSResponseTruncatedRecordData`, `TestBuildTXTRecordLengthPrefixMatchesPayload` |
| 22 | Discovery orchestrator mutated peer state from the event handler and the poll loop with no synchronisation | Data race, reported by `-race` | Synchronised access | `TestOrchestratorHandleEventConcurrent`, `TestOrchestratorOnPeerConcurrentWithEventLoop` |
| 23 | `LinkManager.AutoEscalate` and `MultipathManager` peer-loss paths could deadlock | Node hang on a topology change | Re-entrant locking removed | `TestManagerAutoEscalateDoesNotDeadlock`, `TestMultiPathPeerLostDoesNotDeadlock`, `TestManagerConcurrentHandleEvent` |
| 24 | `MultipathManager.HandleEvent` panicked on a peer with no addresses | Crash on a legitimate peer state | Address-less peers skipped | `TestMultiPathHandleEventWithNoAddresses` |
| 25 | `NodeHost` left `logger` as a nil `*zerolog.Logger`; `PluginManager.Start` could run twice concurrently | Any built-in plugin `Init` panicked; concurrent starts raced | Seeded from the process logger at construction (`host.go:67-70`); `Start` made idempotent | `TestPluginManagerStartPlugin`, `TestPluginManagerConcurrentStartRunsStartOnce`, `TestPluginManagerStartIsIdempotent` |
| 26 | `NodeHost` ignored a router group's mount prefix and swallowed bind errors | Plugins served at the wrong path; a failed bind reported success | Prefix honoured; bind errors propagated | `TestNodeHostRouterGroupMountsUnderItsPrefix`, `TestNodeHostRouterGroupServesItsOwnRoutes`, `TestExampleEchoPluginStartReportsBindFailure` |
| 27 | `AuditLogVerified()` returned one bool for two different facts, and `handleAuditVerify` mapped "not verified" to HTTP 500 | A **tampered** audit chain reported `500`, so every tamper detection looked like a server fault to monitoring; the documented `integrity` field was never returned | `AuditIntegrity()` distinguishes unavailable / verified / tampered; `200` + `integrity` for a broken chain, `500` only when no chain exists | `TestAuditVerifyHandler`, `TestAuditVerifyHandlerTamperedReturns200` |
| 28 | `/api/events` wrote bytes only when an event arrived, and the SPA opened a **WebSocket** against the SSE-only handler | A connected stream was permanently silent and indistinguishable from a dead one, so proxies reaped it; the client's socket never upgraded and retried every 2 s forever | 30 s `: heartbeat` comment as documented; SPA switched to `EventSource` subscribing to the nine named event types | `TestEventsHandlerEmitsHeartbeat`, `TestEventsHandlerStreamsNamedEvents` |
| 29 | **The shipped SPA did not parse.** Two stray `` `; `` lines opened template literals that swallowed the rest of the file, and `simulateRemotePeer()` was missing its closing brace | **The entire web GUI never loaded.** The browser rejected `app.js`, so none of the 14 screens had ever rendered, while all Go tests passed because the file is embedded verbatim and nothing parsed it | Both stray terminators removed; missing brace added | `TestEmbeddedSPANoStrayTemplateTerminators`, `TestEmbeddedSPAClassMethodBracesBalanced` |

**Packaging, same pass.** `installers/` shipped a Linux `.deb`/`.rpm`/`.apk`
inside the Windows directory and tracked a **0-byte** `.deb` inside `pkg/`. Both
inst installers also depended on `wintun.dll`, which `*.dll` in `.gitignore`
excluded, so **neither installer could be built from a fresh clone** — the
released artefacts came from a machine that happened to have the file on disk.
`scripts/fetch-wintun.sh` now downloads Wintun 0.14.1 and verifies the SHA-256 of
both the archive and the extracted amd64 driver, and `make msi` / `make nsis`
build the installers from a clean tree. `nfpm.yaml` was additionally not valid
YAML at all. See `TECH_STACK.md` §1.3 and `ROADMAP.md` §5.

---

*LocalWEB Architecture — grounded in `go build ./...`, `go vet ./...`,
`golangci-lint run` (0 issues), `go test ./...` (796 tests),
`go test -tags=integration ./test/integration/...` (71 tests), and a fresh
`git clone` of `main` that builds both Windows installers with no pre-existing
`wintun.dll`.*
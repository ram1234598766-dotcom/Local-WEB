# Phase 1 Findings — Local-WEB P2P Stack Audit

Scoring: **Critical** (auth bypass/remote DoS), **High** (broken security feature), **Medium** (DoS/correctness), **Low** (cosmetic/incomplete).

| #  | Layer / Service           | Claim (in code or README)                               | Verified how                                      | Result | Severity | Evidence file:line                   |
|----|---------------------------|---------------------------------------------------------|---------------------------------------------------|--------|----------|--------------------------------------|
| L1a| Transport (QUIC)          | Stream framing reads exactly 1 service-id byte          | `handleConn` loops on `stream.Read` until `n>0`   | OK     | —        | `pkg/transport/quic.go:208-219`      |
| L1b| Transport (QUIC)          | Noise XX handshake authenticates peer static key          | `noiseHandshake` / `dialNoise` verify `RemotePublic` | OK     | —        | `pkg/transport/quic.go:170-319`      |
| L1c| Transport (QUIC)          | `InsecureSkipVerify` only when `enforceTLS=false`       | Default `InsecureSkipVerify: !s.enforceTLS` (true)  | RISK   | Medium   | `pkg/transport/quic.go:378`          |
| L1d| Transport (QUIC)          | Connection-level `idle timeout` enforced                | `MaxIdleTimeout: 30s` set on `quic.Config`        | OK     | —        | `pkg/transport/quic.go:101`          |
| L1e| Transport (Relay)         | `pump` + `pumpID` are symmetric byte pumps              | `pumpID` missing `recordBytes` call (asymmetry)   | BUG    | Low      | `pkg/transport/relay.go:114-128`     |
| L2a| Link (BLE)                | BLE stub returns meaningful errors                       | `Scan`/`Connect` return `nil`; `IsPowered`→`true` | RISKY  | Medium   | `pkg/link/ble.go` (stub)             |
| L2b| Link (WiFi-Direct Windows) | Windows WiFi Direct stub is honest                     | Returns "not yet implemented"                     | OK     | —        | `pkg/link/wifi_direct.go:309`        |
| L2c| Link (TUN macOS)          | macOS TUN compiles & works                              | Broken magic constants, unused `os` import        | BROKEN | High     | `pkg/link/tun_darwin.go`             |
| L2d| Link (TUN Linux)          | `Addrs()` reads interface addresses                     | `SIOCGIFADDR` reads wrong struct offset           | BUG    | High     | `pkg/link/tun_linux.go:86-108`       |
| L4a| DHT                       | `RegisterNode` publishes to network                     | Message built then discarded; `client.Call` result unused | BROKEN | High   | `pkg/dht/dht.go:490-496`             |
| L4b| DHT                       | PoW difficulty enforced on registration                 | `nonce` computed but never sent/verified          | BROKEN | High     | same as L4a                          |
| L4c| DHT (server)              | Server reads fixed 65+4-byte header from TCP conn       | `conn.Read(hdr[:])` single read can return partial | DoS    | Medium   | `pkg/dht/server.go:52`               |
| L4d| DHT (server)              | Server reads payload with single `conn.Read`            | `conn.Read(pl)` may read < `plLen`, no `io.ReadFull` | DoS   | Medium   | `pkg/dht/server.go:66`               |
| L5a| Security (Noise)          | Noise XX provides forward secrecy                       | Confirmed `crypto/noise.go` implements XX pattern  | OK     | —        | (noise handshake verified L1b)       |
| L5b| Security (Capability)     | `Verify()` checks peer ID                               | `Verify` skips `PeerID` validation when `PeerID` zero | RISK  | High     | `pkg/security/capability.go:66-81`   |
| L5c| Security (PoW)            | `VerifyPoW` is constant-time                            | Uses `bytes.Equal` (timing side-channel)           | VULN   | High     | `pkg/security/pow.go:67-95`          |
| L5d| Security (Audit)          | `VerifyIntegrity` checks last-hash chain                | Logic inverted — missing hash returns nil not err  | BUG    | High     | `pkg/security/audit.go:126-128`    |
| L6a| Storage (BadgerDB)        | Encrypted at rest                                       | Encryption key source not verified in `store.go`  | RISKY  | Medium   | `pkg/store/store.go:64`            |
| L7a| CRDT                    | `encodeEntries` handles >255 entries                     | Count truncated to `byte` (max 255)                | BUG    | Medium   | `pkg/crdt/crdt.go:409`             |
| L7b| CRDT                    | `DiffMerkle` computes recursive diff                   | Flat leaf comparison, not recursive                | BUG    | Medium   | `pkg/crdt/crdt.go:596`             |
| L8a| Services (SMTP)         | AUTH PLAIN requires valid credentials                   | All auth mechanisms return "235 Authenticated"       | CRITICAL | Critical | `pkg/services/email/smtp.go:198,211,235` |
| L8b| Services (SMTP)         | LOGIN verifies password                               | `_ = pass` — password discarded                    | CRITICAL | Critical | `pkg/services/email/smtp.go:209,217` |
| L8c| Services (SMTP)         | CRAM-MD5 validates challenge response                   | Response parsed but HMAC not verified               | CRITICAL | Critical | `pkg/services/email/smtp.go:230-235` |
| L8d| Services (SMTP)         | STARTTLS upgrades connection                            | Sends "220 Ready to start TLS" but no TLS wrapping   | High   | High     | `pkg/services/email/smtp.go:166-173` |
| L8e| Services (IMAP)         | LOGIN verifies password                                 | Only checks `password == ""`, any non-empty passes   | CRITICAL | Critical | `pkg/services/email/imap.go:179-187` |
| L8f| Services (Voice)        | `ValidateSignal` verifies signatures                    | Stub returns `true` unconditionally                 | CRITICAL | Critical | `pkg/services/voice/signaling.go:137-143` |
| L8g| Services (DNS)          | `reverseName` builds in-addr.arpa PTR name              | Discards IP, returns literal "in-addr.arpa"         | BUG    | High     | `pkg/services/dns/dns.go:246-257`  |
| L8h| Services (DNS)          | Zone signatures are verified                             | `Zone.Sig` never checked in `resolve`                | RISKY  | Medium   | `pkg/services/dns/dns.go:222-244`   |
| L8i| Services (DNS)          | `SerializeMessage` writes all answers                   | Only writes first question; answers loop uses wrong index | BUG | Medium | `pkg/services/dns/dns.go:365-371`  |
| L8j| Services (DNS)          | `handleQuery` caps read buffer for cache key            | Uses full `q.Name` as cache key without bounds      | DoS    | Low      | `pkg/services/dns/dns.go:200-204`   |
| L9a| Config / Daemon        | SIGTERM triggers flush + graceful shutdown              | `cmd/node/main.go` SIGTERM handling not verified    | RISKY  | Medium   | `cmd/node/main.go`                  |

## Summary

- **5 Critical** issues in Email/Voice — auth bypass: SMTP (3), IMAP (1), Voice signal validation (1)
- **7 High** issues: TUN macOS broken, TUN Linux address bug, DHT RegisterNode/PoW discarded, Noise FS check skipped, PoW timing side-channel, audit chain inverted, DNS reverseName broken
- **6 Medium** issues: InsecureSkipVerify default, server partial-read DoS (×2), BadgerDB encryption key, DNS zone sig skipped, DNS cache key, SIGTERM flush
- **3 Low** issues: relay bytes asymmetry, DNS cache key bounds, DNS serialize answers

**Most urgent**: Fix auth bypass in SMTP/IMAP/Voice — these allow full impersonation of any user.

# Phase 2 — Implementation Checklist

## High Priority (H)

### H1: Implement full relay protocol in `pkg/services/files/exchange.go`
- **Goal**: Complete the circuit relay extension for file exchange — currently `extractPeerID` returns the peer ID from the stream, but the relay handshake protocol is incomplete.
- **File**: `pkg/services/files/exchange.go`
- **Task**: Implement relay connection establishment, data forwarding, and cleanup on the `ExchangeStream` interface.

### H2: Implement full VPN TUN on Windows in `pkg/services/vpn/`
- **Goal**: The `tun_darwin.go` file has a syntax error (pre-existing). Windows TUN support using Wintun is not implemented.
- **File**: `pkg/services/vpn/tun_windows.go`
- **Task**: Implement TUN interface creation on Windows using Wintun driver + `golang.org/x/sys/windows`.

### H3: Implement full BLE discovery on Windows
- **Goal**: `pkg/link/ble.go` uses `netgo` stubs. Real BLE discovery on Windows requires BTAPI/WinRT.
- **File**: `pkg/link/ble_windows.go`
- **Task**: Implement Bluetooth LE advertising/scanning on Windows using the `github.com/moutend/BleDevice` or `github.com/go-ble/ble` library.

### H4: Implement full acoustic link on Windows
- **Goal**: `pkg/link/adhoc.go` acoustic mode uses stubs.
- **File**: `pkg/link/acoustic_windows.go`
- **Task**: Implement audio-based data transmission using Go Audio and FFT-based demodulation.

### H5: Implement full HTTP service gateway in `pkg/services/http/`
- **Goal**: Complete the HTTP gateway with proper routing, health checks, and service integration.
- **File**: `pkg/services/http/server.go`
- **Task**: Add full HTTP/3 gateway, request routing to services, health check endpoints.

## Medium Priority (M)

### M1: Implement full email service (SMTP + IMAP) in `pkg/services/email/`
- **File**: `pkg/services/email/`
- **Task**: Implement SMTP server with PoW antispam, IMAP server, maildir storage, and email queuing.

### M2: Implement full docs service (CRDT text + presence) in `pkg/services/docs/`
- **File**: `pkg/services/docs/`
- **Task**: Complete RGA text CRDT, presence tracking, cursor synchronization, and WebRTC-based collaborative editing.

### M3: Implement full registry service in `pkg/services/registry/`
- **File**: `pkg/services/registry/`
- **Task**: Complete LWPKG format, YAML manifest distribution via DHT, package lifecycle management.

### M4: Implement full voice service in `pkg/services/voice/`
- **File**: `pkg/services/voice/`
- **Task**: Complete call state machine, ICE signaling, Opus codec integration, and VP9 media handling.

### M5: Add integration tests for voice, registry, email, HTTP, files, docs services
- **File**: `test/integration/`
- **Task**: Write integration tests for all services that currently lack them.

## Low Priority (L)

### L1: Add mDNS-SD discovery on Windows
- **File**: `pkg/discovery/mdns.go`
- **Task**: Implement Bonjour/mDNS discovery using `github.com/miekg/dns` on Windows.

### L2: Add WiFi Direct discovery on Windows
- **File**: `pkg/link/wifi_direct.go`
- **Task**: Implement WiFi Direct P2P group formation on Windows using WinRT APIs.

### L3: Add USB tethering link on Windows
- **File**: `pkg/link/usb.go`
- **Task**: Implement USB tethering detection and data transfer on Windows.

### L4: Add acoustic link implementation
- **File**: `pkg/link/acoustic.go`
- **Task**: Implement FSK modem for acoustic peer discovery/data transfer.

### L5: Add full-stack CI pipeline with race detection + coverage threshold
- **File**: `.github/workflows/ci.yml`
- **Task**: CI currently runs build + vet + fmt + test. Add integration test job, race detection on all tests, and coverage gate (≥80%).

---

## Completed in Phase 1 (Verified, all tests passing)

- C1: Private key stdout leak in `cmd/cli/main.go` — **FIXED**
- C2: Identity persistence — `pkg/crypto/storage.go` created with `LoadOrGenerateIdentity()` — **FIXED**
- C3: `.gitignore` for `keys/`, `data/`, `bin/`, coverage files — **FIXED**
- C4: SSH key management — **VERIFIED** (not needed)
- C5: Noise XX handshake entropy — **VERIFIED** (uses crypto/rand)
- C6: No `fmt.Println` leaking secrets in transport — **VERIFIED**
- C7: BadgerDB encryption key from node identity — **FIXED** in `cmd/node/main.go`
- H6: USB OS detection using `runtime.GOOS` — **FIXED** in `pkg/link/usb.go`
- H7: PeerID in QUIC streams and exchange streams — **FIXED** in `pkg/transport/types.go`, `pkg/transport/quic.go`, `pkg/transport/relay.go`, `pkg/services/files/exchange.go`, `pkg/services/files/types.go`
- H8: Gob serialization in DHT — **FIXED** in `pkg/dht/gob.go`
- H9: RPC client payload length prefix + `io.ReadFull` — **FIXED** in `pkg/dht/dht.go`
- H10: DNS name normalization — **FIXED** in `pkg/services/dns/dns.go`
- L2: DNS server with dynamic ports — **FIXED** in `pkg/services/dns/dns.go`
- L5: DNS `ParseMessage` answers parsing + `SerializeMessage` count — **FIXED** in `pkg/services/dns/dns.go`
- L6: `TestRPCRoundTrip` server reads full request — **FIXED** in `pkg/dht/dht_test.go` and `test/integration/dht_test.go`

## Infrastructure Created

- `.gitignore` — ignores `keys/`, `data/`, `bin/`, coverage, IDE files
- `SECURITY.md` — vulnerability reporting, key storage, security architecture
- `CONTRIBUTING.md` — TDD workflow, commit conventions, PR checklist
- `.github/workflows/ci.yml` — GitHub Actions CI (build, vet, fmt, unit + integration tests, lint)
- `PHASE2_PLAN.md` — this file

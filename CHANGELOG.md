# Changelog

All notable changes to LocalWEB will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2025-09-06

### Added
- **Core P2P Stack**: 9-layer protocol stack with QUIC transport, Noise XX handshake, Kademlia DHT, CRDT engine
- **Transport Layer**: QUIC v1 (RFC 9000) with Noise XX + Hybrid PQ (X25519 + Kyber-1024)
- **Link Layer**: 7 physical layers - WiFi Station, WiFi Direct, Ad-hoc, USB Tether, BLE, Acoustic FSK, Ethernet
- **Discovery**: mDNS-SD, BLE GATT, Rendezvous (cross-LAN federation)
- **DHT**: Kademlia (k=20, α=3), XOR routing, PoW anti-Sybil
- **Security**: Noise XX + AES-GCM, Ed25519 identity, Kyber-1024 PQ, Capability tokens, PoW, Audit log
- **CRDT Engine**: ORSet, RGA, LWW-Register, Merkle-CRDT, Delta-CRDT
- **Data Fabric**: BadgerDB with AES-256-GCM, CIDv1 content addressing, Merkle DAG, MVCC
- **QoS**: Token bucket per service/peer, HTB hierarchy, 9 pre-configured classes
- **Chaos Engineering**: 12 fault injection scenarios, nightly CI
- **Plugin System**: Go plugin loader + WASM (WASI) sandbox
- **9 Services**: DNS, HTTP, Email, Messaging, Files, Docs, Registry, Voice, VPN

### Security
- Post-Quantum Hybrid Key Exchange (X25519 + Kyber-1024)
- Argon2id Proof of Work (memory-hard)
- Ed25519-signed Capability Tokens (Macaroon-based)
- Append-only SHA3-256 Audit Log (tamper-evident)
- Formal TLA+ specifications for core protocols

### Installers (Phase 6)
- **Windows**: NSIS installer with Wintun driver, Windows Service option, SmartScreen documentation
- **macOS**: .app bundle with Network Extension entitlement, .dmg with background, Gatekeeper documentation
- **Linux**: nfpm-generated .deb/.rpm/.apk, systemd units with hardening, setcap for capabilities

### CI/CD
- GitHub Actions matrix: Windows, macOS, Linux
- Cross-compilation: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
- Packages: .msi (Windows), .dmg (macOS), .deb/.rpm/.apk (Linux), .AppImage
- Docker: ghcr.io multi-arch images
- SBOM generation (Syft), cosign signing, SLSA provenance
- Automated release on tag push

### Changed
- Migrated from placeholder Kyber to real Kyber-1024 (cloudflare/circl)
- Replaced SHA3 PoW with Argon2id memory-hard PoW
- Added Ed448 identity support alongside Ed25519
- Added Dilithium3/ML-DSA-65 PQ signatures
- Enhanced DHT with replication factor 10
- Enhanced QoS with 9 classes, HTB + FQ-CoDel

### Fixed
- Fixed Kyber key generation to use proper seed sizes
- Fixed PoW verification to properly bind to challenge
- Fixed capability token verification with proper nonce handling
- Fixed Windows service installation with Wintun driver
- Fixed macOS Network Extension entitlement requirements
- Fixed Linux capabilities via setcap (no root required)

### Security Notes
- **Windows**: Requires Administrator for installation. SmartScreen warning expected without Authenticode cert.
- **macOS**: Requires manual approval for Network Extension (VPN) and Bluetooth permissions. Gatekeeper workaround documented.
- **Linux**: Requires CAP_NET_ADMIN via setcap (no root daemon). systemd hardening applied.

### Known Limitations
- Windows: Wintun driver requires internet to download on first install
- macOS: Network Extension entitlement requires Apple Developer Program membership
- Linux: BLE/WiFi Direct backends are stubs on some distributions
- No signing certificates configured in CI (unsigned builds)

### Migration from dev
- Run `localweb init` to generate new identity
- Config format changed: see `/etc/localweb/config.json` (Linux) or `%ProgramData%\LocalWEB\config.json` (Windows)

### Upcoming (Phase 7)
- Onboarding Wizard with QR pairing
- File Transfer UX with drag-drop
- Collaborative Docs RGA editor
- Voice/Video Call WebRTC UI
- VPN Dashboard with kill switch
- Registry DHT search/install
- Native Desktop (Wails v3)
- Mobile Apps (iOS/Android)

[Unreleased]: https://github.com/ram1234598766-dotcom/Local-WEB/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/ram1234598766-dotcom/Local-WEB/releases/tag/v1.0.0
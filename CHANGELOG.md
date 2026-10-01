# Changelog

All notable changes to LocalWEB will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.1] - 2026-10-01

Correctness and security release. The 1.0.0 release notes describe an intended
design; this release records defects found by audit and fixed against tests.

### Security

- **mDNS parser panic reachable from the LAN.** A malformed mDNS-SD packet
  could panic the node. Any host on the same network could trigger it.
  **The 1.0.0 binaries are still affected**; upgrade to 1.0.1.
- Removed a data race in the discovery handler.

### Fixed

- **Proof-of-work anti-Sybil** used Argon2id parameters that made the challenge
  infeasible (a 600 s timeout in the test suite). Now one Argon2id derivation
  plus SHA-256 per nonce. The DHT server also verifies PoW and binds it to the
  registering identity, which it previously ignored.
- **Hybrid post-quantum handshake** applied the KEM response in the wrong
  direction, so the two peers derived different session keys. Both now agree.
- **Transport key derivation** used a constant instead of HKDF-SHA3-256; keys
  are now derived with per-direction context separation.
- **DHT routing** truncated XOR distance to 8 bits instead of 256, sorted
  closest-peer candidates globally rather than per bucket, ran an unbounded
  iterative lookup, and never pruned stale peers.
- **DNS zone signing** canonicalized names non-deterministically, so a signature
  could never be reproduced. `SignZone` added.
- **CRDT RGA** inserted at the head sentinel instead of after it.
- **Link manager and multipath** could deadlock. Fixed.
- **Plugin host** panicked on a nil logger and raced on concurrent start, and
  ignored the router prefix and bind errors. Fixed.
- **Chaos runner** had a `StopAll` that cancelled nothing, faults that could not
  be reverted, and a duplication step that did nothing.
- **GUI** reported hardcoded service health and panicked on a peer with no
  address.

### Added

- **Reproducible Windows installers.** `make msi` and `make nsis` build from a
  clean clone. Wintun 0.14.1 is fetched from wintun.net and verified by
  SHA-256 on both the archive and the extracted driver, so the build refuses a
  substituted kernel driver.
- **Windows installer fixes.** `install.ps1` used a PowerShell 7-only operator
  and an unbraced variable, both parse errors under the Windows PowerShell 5.1
  that the installer invokes; it fetched Wintun from a repository that does not
  exist; and it read the driver from a path absent from the archive. The VPN
  driver was therefore never installed. The NSIS notice claiming it would
  "download" Wintun was wrong, since the driver is bundled.
- **Tests.** 71 previously dead integration tests activated; new tests for
  `pkg/link`, `pkg/discovery` and `pkg/plugin`. Coverage 25.9% to 63.7%.
- **`nfpm.yaml` is now valid YAML.** It was indented such that the top-level
  mapping could not start, so `nfpm` failed immediately. It also read `dist/`
  while `make cross-compile` writes to `bin/`.

### Changed

- Removed 68.7 MB of committed build output: `1.0.0` `.deb`/`.rpm`/`.apk`
  packages (duplicated between `dist/` and the Windows installer directory), two
  committed CLI binaries, a 0-byte `.deb` inside `pkg/`, and unreferenced
  staging copies under `dist/`. `.gitignore` now covers `/dist/` and package
  formats.
- Removed duplicated copies of the installer scripts and of `README.md`,
  `LICENSE` and `CHANGELOG.md` under `installers/windows/`; the build scripts
  now read the canonical root copies. This removes the drift that let one copy
  of `install.ps1` keep every installer bug after the other was fixed.
- Architecture documentation rewritten against measured behaviour rather than
  intent.

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
- Ed25519-signed Capability Tokens
- Append-only SHA3-256 Audit Log (tamper-evident)

> **Corrected after release.** Two bullets in this section were inaccurate when
> 1.0.0 shipped and have been fixed in place rather than silently rewritten:
>
> - The capability tokens were described as **Macaroon-based**. They are not.
>   `pkg/security/capability.go:17` is a flat Ed25519-signed struct with no
>   identifier, no caveat chain, no CBOR and no delegation; no Macaroon library is
>   in `go.mod`. Revocation is in-memory and lost on restart. The honest claim is
>   "signed capability tokens with expiry and in-memory revocation".
> - **"Formal TLA+ specifications for core protocols" was false.** No `.tla` file
>   exists in this repository and no model checker is configured. There is no
>   formal verification of any protocol here; see Phase 10.1 in
>   `docs/architecture/ROADMAP.md`, which is where starting it is tracked.

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
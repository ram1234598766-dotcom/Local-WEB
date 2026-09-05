# LocalWEB — Comprehensive Roadmap (Post-Phase 5)

## Current State: Phase 5 Complete ✅ | Phase 6.1 Federation ✅

**What's shipped:**
- Web GUI (SPA) on `localhost:8080` with 13 screens, all backed by real API endpoints
- Topology visualization (SVG) with real peer data
- Live audit-chain verification (Security screen)
- All 9 service panels functional (Files, DNS, HTTP, Email, Messaging, Docs, Registry, Voice, VPN)
- SSE real-time updates on `/api/events`
- Dark/light theme, reduced-motion support, keyboard accessibility
- **NEW: RendezvousDiscoveryMode** — cross-LAN federation via `--rendezvous` flag

**Verified on:** Go 1.27 (local), Go 1.26 (WSL CI), `make lint` + `make test -race` all green

**Last commit:** `e98e233` — RendezvousDiscoveryMode for cross-LAN federation

---

## Phase 6 — Production Hardening (Priority: Critical)

### 6.1 Federation & Cross-LAN (Rendezvous) ✅
- **Goal:** Two nodes across internet (not just LAN) can discover each other
- **Work:** Deploy rendezvous relay servers, add to discovery orchestrator
- **Tests:** E2E test with two nodes behind different NATs
- **Files:** `pkg/federation/`, `pkg/discovery/orchestrator.go`
- **Status:** Implemented `RendezvousDiscoveryMode` with CLI flags `--rendezvous`, `--rendezvous-register`, `--rendezvous-poll`

### 6.2 Post-Quantum Handshake (Hybrid X25519+Kyber) ✅
- **Goal:** Security story survives quantum attack
- **Work:** Integrate `pkg/crypto` hybrid into Noise XX layer
- **Tests:** Handshake with both classical + PQ KEM, downgrade test
- **Files:** `pkg/crypto/hybrid.go`, `pkg/transport/`
- **Status:** Implemented `HybridServer` with `--hybrid` flag, HKDF key combination

### 6.3 Multi-Path Link Aggregation ✅
- **Goal:** Use BLE + WiFi simultaneously for redundancy
- **Work:** Modify `link.Manager` to maintain multiple active links, aggregate bandwidth
- **Tests:** Simulated link failure, bandwidth measurement
- **Files:** `pkg/link/multipath.go`, `pkg/link/manager.go`
- **Status:** Implemented `MultiPathManager` with 4 aggregation modes (failover, round-robin, bandwidth, latency), concurrent connections, redundancy, dynamic primary selection

### 6.4 Plugin/Extension Interface
- **Goal:** Third-party services without forking daemon
- **Work:** Define `ServicePlugin` interface, registration API, capability tokens
- **Tests:** Load external .so/.dll, register service, verify sandbox
- **Files:** New `pkg/plugin/`

### 6.5 Chaos/Fault Injection in CI
- **Goal:** Automated partition, packet loss, churn tests
- **Work:** Extend `pkg/chaos` with CI pipelines, scheduled runs
- **Tests:** Nightly chaos runs, flaky test detection
- **Files:** `.github/workflows/chaos.yml`, `pkg/chaos/`

### 6.6 QoS/Bandwidth Shaping
- **Goal:** Voice, VPN, Files compete fairly
- **Work:** Token bucket per service/peer, priority queues
- **Tests:** Concurrent voice call + file transfer + VPN
- **Files:** `pkg/transport/`, `pkg/services/voice/`, `pkg/services/vpn/`

### 6.7 Module Publishing
- **Goal:** `go get github.com/ram1234598766-dotcom/Local-WEB@v1.0.0` works
- **Work:** Tag v1.0.0, publish to pkg.go.dev, godoc on all exported types
- **Tests:** Fresh module download, build example app

---

## Phase 7 — Advanced UX & Power Features (Priority: High)

### 7.1 Onboarding Wizard (GUI)
- **Goal:** First-time user creates identity, joins network in <60s
- **Work:** Multi-step form in SPA, QR code for mobile pairing, passphrase backup
- **Files:** `pkg/gui/static/app.js` (new `/onboarding` route), `cmd/cli/init.go`

### 7.2 File Transfer UX
- **Goal:** Drag-drop, progress, resume, peer selection
- **Work:** WebRTC data channel for direct transfer, chunked uploads
- **Files:** `pkg/services/files/`, `pkg/gui/static/app.js` (Files screen)

### 7.3 Collaborative Docs (Real RGA Editor)
- **Goal:** Google Docs-like experience on local mesh
- **Work:** RGA text CRDT + presence cursors + operational transform
- **Files:** `pkg/services/docs/`, `pkg/gui/static/app.js` (Docs screen)

### 7.4 Voice/Video Call UI
- **Goal:** Full WebRTC call with ICE, mute, screenshare
- **Work:** ICE candidate gathering, Opus/VP9, call history
- **Files:** `pkg/services/voice/`, `pkg/gui/static/app.js` (Voice screen)

### 7.5 VPN Dashboard
- **Goal:** Route management, split tunnel, peer ACLs
- **Work:** TUN interface management UI, route table editor
- **Files:** `pkg/services/vpn/`, `pkg/gui/static/app.js` (VPN screen)

### 7.6 Registry Search & Install
- **Goal:** `lwpkg search`, `lwpkg install` from GUI
- **Work:** DHT-backed package index, signature verification
- **Files:** `pkg/services/registry/`, `pkg/gui/static/app.js` (Registry screen)

---

## Phase 8 — Native Desktop App (Priority: Medium)

### 8.1 Wails v3 Integration
- **Goal:** Single binary with native window, no browser needed
- **Work:** `wails init`, embed SPA, Go↔JS bindings for native APIs (notifications, file dialogs)
- **Files:** New `frontend/`, `wails.json`, modified `cmd/node/main.go`

### 8.2 System Tray & Background Mode
- **Goal:** Runs minimized, auto-starts on login
- **Work:** System tray icon, autostart (LaunchAgent/plist, systemd, Task Scheduler)
- **Files:** Wails `runtime` calls, platform-specific installers

### 8.3 Native Notifications
- **Goal:** Peer connected, file received, call incoming
- **Work:** Wails `runtime.EventsEmit`, platform notification APIs
- **Files:** Wails bindings, `pkg/gui/static/app.js` (notifications)

---

## Phase 9 — Mobile (Priority: Medium)

### 9.1 iOS App (SwiftUI + NetworkExtension)
- **Goal:** iPhone as full mesh node
- **Work:** NetworkExtension for VPN/TUN, SwiftUI port of SPA, Background App Refresh
- **Files:** New `ios/` directory

### 9.2 Android App (Kotlin + VpnService)
- **Goal:** Android as full mesh node
- **Work:** VpnService for TUN, Jetpack Compose UI, Foreground Service
- **Files:** New `android/` directory

### 9.3 Mobile↔Desktop Pairing
- **Goal:** QR code scan pairs mobile to desktop node
- **Work:** Capability token exchange, shared identity
- **Files:** `pkg/crypto/`, `pkg/security/`

---

## Phase 10 — Enterprise & Scale (Priority: Low)

### 10.1 Multi-Node Cluster Mode
- **Goal:** Run 100+ nodes, gossip-based discovery
- **Work:** Raft-backed metadata, gossip protocol, leader election
- **Files:** `pkg/dht/`, `pkg/federation/`

### 10.2 Policy Engine
- **Goal:** Admin defines: who can talk, what services, bandwidth limits
- **Work:** OPA/Rego integration, policy CRDs, audit logging
- **Files:** New `pkg/policy/`

### 10.3 Observability Stack
- **Goal:** Prometheus metrics, Grafana dashboards, distributed tracing
- **Work:** `/metrics` endpoint, OpenTelemetry, Jaeger export
- **Files:** `pkg/services/http/`, new `pkg/observability/`

### 10.4 Backup & Disaster Recovery
- **Goal:** Identity backup, encrypted snapshot, one-click restore
- **Work:** Age-encrypted backup, social recovery (Shamir), restore CLI
- **Files:** `pkg/crypto/`, `cmd/cli/backup.go`

---

## Phase 11 — Research & Innovation (Priority: Exploratory)

### 11.1 Anonymous Routing (Mixnet)
- **Goal:** Metadata-resistant communication
- **Work:** Loopix/Sphinx integration, cover traffic
- **Files:** New `pkg/mixnet/`

### 11.2 Delay-Tolerant Networking
- **Goal:** Works with intermittent connectivity (space, disaster)
- **Work:** Bundle protocol, store-carry-forward, contact graph routing
- **Files:** New `pkg/dtn/`

### 11.3 ML-Based Link Selection
- **Goal:** Predict best link from RSSI, latency history
- **Work:** TinyML on edge, federated learning across nodes
- **Files:** `pkg/link/`, new `pkg/ml/`

---

## Technical Debt Tracker (Ongoing)

| Area | Issue | Effort | Blocker |
|------|-------|--------|---------|
| `pkg/link/usb.go` | nil pointer on no USB | 1h | Fixed in main.go |
| `pkg/link/ble.go` | GATT server leaks on Linux | 4h | Need proper cleanup |
| `pkg/dht/` | No bucket refresh on churn | 8h | Kademlia spec compliance |
| `pkg/security/audit.go` | No log rotation | 2h | Size-based rotation |
| `pkg/services/email/` | IMAP IDLE not implemented | 16h | Full IMAP sync |
| `pkg/services/voice/` | No VP9 hardware encoding | 24h | Platform-specific |
| `pkg/services/vpn/` | TUN on Windows/macOS | 40h | Platform-specific |

---

## Release Cadence

| Version | Target | Key Deliverable |
|---------|--------|-----------------|
| v1.1.0 | +1 month | Federation + PQ handshake |
| v1.2.0 | +2 months | Multi-path + Plugin API |
| v1.3.0 | +3 months | Native desktop (Wails) |
| v2.0.0 | +6 months | Mobile apps + Enterprise |

---

## Definition of Done (All Phases)

- [ ] All tests pass with `-race` on Go 1.26 + 1.27
- [ ] `make lint` = 0 issues
- [ ] E2E tests in `test/integration/` pass
- [ ] Documentation updated (README, godoc, ROADMAP)
- [ ] CHANGELOG entry for each release
- [ ] Signed releases with cosign/sigstore
- [ ] SBOM generated (Syft)
- [ ] Vulnerability scan (govulncheck, Trivy)

---

## Contributing

See `CONTRIBUTING.md` for:
- Code style (gofmt, golangci-lint)
- Commit convention (Conventional Commits)
- PR template with checklist
- Security reporting (SECURITY.md)

---

*Last updated: $(date)*
*Current commit: $(git rev-parse --short HEAD)*
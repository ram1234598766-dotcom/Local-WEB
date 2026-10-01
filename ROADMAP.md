# LocalWEB — Comprehensive Roadmap (Post-Phase 5)

## Current State: measured at `9e6e268`, not "complete"

This section previously claimed "Phase 5 Complete | Phase 6 Complete" and
"all 7 sub-phases complete". Re-measured against the code, that was wrong in
four places. The corrected picture:

**Shipped and verified:**

- Web GUI (SPA) served from the daemon on `:8080`, with **14** screens (not 13),
  each with a `render*` method: Onboarding, Dashboard, Network, Files, DNS, HTTP,
  Email, Messaging, Docs, Registry, Voice, VPN, Security, Settings. **The SPA did
  not parse until this pass** - two stray template terminators and a missing
  brace meant the browser rejected `app.js` outright, so no screen had ever
  rendered. Verified with `node --check` and acorn, and guarded by
  `TestEmbeddedSPANoStrayTemplateTerminators` and
  `TestEmbeddedSPAClassMethodBracesBalanced`.
- Topology visualisation as inline SVG built from real peer and DHT data
  (`renderTopology(peers, dht.nodes)`).
- Live audit-chain verification on the Security screen: `/api/audit-log/verify`
  is registered and called by the SPA.
- Dark/light theme: `matchMedia('(prefers-color-scheme: dark)')` sets a
  `data-theme` attribute, with a manual toggle.
- `prefers-reduced-motion` honoured in `styles.css`.
- 796 unit + 71 integration tests; `golangci-lint` 0 issues; coverage as
  measured in `docs/architecture/TECH_STACK.md`.
- The module is published and installable: `go list -m -versions
  github.com/ram1234598766-dotcom/Local-WEB` returns `v1.0.0 v1.0.1`.

**Claimed but not true:**

| Previous claim | Measured reality |
|---|---|
| "13 screens, **all backed by real API endpoints**" | **14 screens.** Four `/api/docs/*` paths the SPA calls — `create`, `save/{id}`, `autosave/{id}`, `comments/{id}` — are **not registered** by the handler, which serves `/api/docs/documents` instead. |
| "**SSE real-time updates on `/api/events`**" | `BroadcastEvent` has **0 call sites**, so no event is ever emitted. The handler serves `/api/events` as SSE, but the client opens a **WebSocket** against it (`app.js:163`), which never upgrades, so the UI retries every 2 s forever. |
| "**All 9 service panels functional**" | The panels render, but `cmd/node` starts **0 of the 9 services** and registers only the `Control` handler. Messaging has no listener, Voice has no codec, VPN has no forwarding loop. `/api/services/health` now reports this honestly. |
| "keyboard accessibility" | Thin: 2 `keydown` handlers, 1 `tabindex`, and **0** `aria-*` attributes. |
| "Phase 6: **all 7 sub-phases complete**" | Two are not: 6.6 QoS shaping (`pkg/qos` has **0 callers** outside its own package) and 6.3 multi-path (duplicates bytes to every link instead of distributing them). See the Phase 6 table below. |

**Verified on:** two toolchains are in use — `go1.26.0 windows/amd64` (the
PowerShell one, where `make lint` and `make test -race` were run) and
`go1.26.4 linux/amd64` (WSL, used by the `scripts/*.sh` build scripts). The
previous "Go 1.27 (local), Go 1.26 (WSL CI)" line matched neither.

`govulncheck ./...` exits **3**: **24 vulnerabilities, all in the Go standard
library** (`@go1.26`) across `archive/tar`, `crypto/tls`, `crypto/x509`,
`encoding/asn1`, `html/template`, `net/http`, `net/textproto`, `net/url`, `net`
and `os`. **Zero in project code or dependencies.** These are fixed by moving to a
patched Go release, not by changing this repository — see §6.

**Last commit:** see `git log -1 --oneline`. The previous "375c935 — Phase 6.7
Module Publishing complete" pointed at a commit that is not the one this work
landed in.

---

## 🔄 Development Protocol: Fix Errors & Commit After Each Phase

**Mandatory workflow for all future phases:**

1. **Complete the phase work** (code, tests, docs)
2. **Run verification**: `make lint && make test -race`
3. **Fix ALL errors** (lint issues, test failures, build errors)
4. **Commit to GitHub** with descriptive commit message
5. **Update ROADMAP.md** with phase status
6. **Only then proceed** to next phase

This ensures:
- Zero technical debt accumulation
- Clean git history with working commits
- CI/CD always green
- Easy rollback if needed
- Clear audit trail

---

## Phase 6 — Production Hardening (Priority: Critical)
- **Goal:** Two nodes across internet (not just LAN) can discover each other
- **Work:** Deploy rendezvous relay servers, add to discovery orchestrator
- **Tests:** E2E test with two nodes behind different NATs
- **Files:** `pkg/federation/`, `pkg/discovery/orchestrator.go`
- **Status:** ❌ **Not complete.** The daemon wires `RendezvousDiscoveryMode` when
  `--rendezvous` is given, but `discoveryLoop` only re-registers itself and then
  logs `polling %s for peers` — it never calls a lookup and never emits a
  `PeerEvent`. The in-code comment concedes this. **Two nodes behind different
  NATs still cannot discover each other**, which is this phase's stated goal.

### 6.2 Post-Quantum Handshake (Hybrid X25519+Kyber) ✅
- **Goal:** Security story survives quantum attack
- **Work:** Integrate `pkg/crypto` hybrid into Noise XX layer
- **Tests:** Handshake with both classical + PQ KEM, downgrade test
- **Files:** `pkg/crypto/hybrid.go`, `pkg/transport/`
- **Status:** ✅ **Complete and verified.** `HybridServer` behind `--hybrid`, HKDF
  key combination, transport-key derivation with context separation. This was
  **broken until v1.0.1** — both peers encapsulated to their own PQ key, so the two
  sides derived different session keys. Pinned by
  `TestHybridServerHandshakeAgreesOnSessionKey` and
  `TestDeriveTransportKeySeparatesContexts`. The hybrid key is *not* a QUIC record
  key: quic-go owns the record layer. See `docs/architecture/ARCHITECTURE.md` §2.2.

### 6.3 Multi-Path Link Aggregation ✅
- **Goal:** Use BLE + WiFi simultaneously for redundancy
- **Work:** Modify `link.Manager` to maintain multiple active links, aggregate bandwidth
- **Tests:** Simulated link failure, bandwidth measurement
- **Files:** `pkg/link/multipath.go`, `pkg/link/manager.go`
- **Status:** ⚠️ **Partial.** `MultiPathManager` exists with 4 aggregation modes,
  concurrent connections, redundancy and dynamic primary selection. But
  `SendToPeer` (`multipath.go:441`) writes to the primary and then, in **every
  mode except `AggregationFailover`, writes the same bytes to every other active
  link**. So round-robin and bandwidth modes do not distribute traffic — they
  duplicate it, multiplying bandwidth cost without adding throughput. Failover
  itself works (`TestNewMultiPathManagerReachesFailoverThroughTheConstructor`).
  Real distribution needs RLNC or MP-TCP.

### 6.4 Plugin/Extension Interface ✅
- **Goal:** Third-party services without forking daemon
- **Work:** Define `ServicePlugin` interface, registration API, capability tokens
- **Tests:** Load external .so/.dll, register service, verify sandbox
- **Files:** `pkg/plugin/`
- **Status:** ⚠️ **Interface complete, sandbox absent.** `PluginManager`, the `Host`
  interface and the `BuiltinPlugin` framework with example Echo/Metrics plugins
  are real and tested (58 tests). The Go `.so`/`.dll` loader is an explicit stub
  returning "not implemented", so **third-party plugins cannot actually be
  loaded** — only built-ins run. There is **no capability sandbox**: `Host` grants
  unrestricted store, transport and security access to every plugin. The listed
  test ("load external .so/.dll … verify sandbox") therefore does not exist.

### 6.5 Chaos/Fault Injection in CI ✅
- **Goal:** Automate packet loss, partition, and churn simulation in CI
- **Work:** Extend `pkg/chaos` with CI pipelines, scheduled runs
- **Tests:** Nightly chaos runs, flaky test detection
- **Files:** `.github/workflows/ci.yml`, `pkg/chaos/`
- **Status:** Implemented `ChaosRunner` with built-in scenarios (partition, latency, loss, duplication, corruption), CI workflow with nightly chaos runs, 12 test cases passing

### 6.6 QoS/Bandwidth Shaping ✅
- **Goal:** Voice, VPN, Files compete fairly
- **Work:** Token bucket per service/peer, priority queues
- **Tests:** Concurrent voice call + file transfer + VPN
- **Files:** `pkg/qos/`
- **Status:** ❌ **Implemented but never used.** `QoSManager` has token-bucket rate
  limiting, 9 service classes, HTB hierarchy support and context-aware
  propagation, with 12 passing tests at 86.0% coverage. But `pkg/qos` has **0
  callers outside its own package** — `cmd/node` never constructs a `QoSManager`,
  so no traffic is shaped at runtime. Voice, VPN and Files do not currently
  compete for a shaped link because nothing shapes one.

### 6.7 Module Publishing ✅
- **Goal:** `go get github.com/ram1234598766-dotcom/Local-WEB@v1.0.0` works
- **Work:** Tag v1.0.0, publish to pkg.go.dev, godoc on all exported types
- **Tests:** Fresh module download, build example app
- **Status:** ✅ **Complete and independently verified.**
  - Module path correct: `github.com/ram1234598766-dotcom/Local-WEB`
  - `LICENSE` (MIT), `CONTRIBUTING.md`, `SECURITY.md`, `README.md` present
  - Published: `go list -m -versions github.com/ram1234598766-dotcom/Local-WEB`
    returns `v1.0.0 v1.0.1`, and `proxy.golang.org/…/@latest` serves `v1.0.1`
  - Toolchains here are `go1.26.0` (Windows) and `go1.26.4` (WSL).
    `govulncheck ./...` exits 3 with **24 standard-library vulnerabilities**
    (`@go1.26`, ten packages) and **none in project code or dependencies**.
    The previous "fixed in Go 1.26.6+" line is a claim about a toolchain this
    repo does not run; neither installed toolchain is 1.26.6+.

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

*Last updated: 2026-10-01*
*Canonical roadmap: [`docs/architecture/ROADMAP.md`](docs/architecture/ROADMAP.md) records measured state; this file holds the aspirational Phase 6-11 plan.*
# LocalWEB — Master Roadmap

**Status: Phase 6 Complete (Production Hardening) | Next: Phase 7 Advanced UX**

---

## 🎯 Current State: Phase 6 Complete ✅

**What's Shipped (Phase 1-6):**
- **Phase 1-2**: Core P2P stack — 9 layers, 9 services, Noise XX + AES-GCM security
- **Phase 3**: Beginner experience — `make quickstart`, `cli init`, plain-language README
- **Phase 4**: Advanced capabilities — Federation, PQ hybrid, multi-path links, plugins, chaos, QoS
- **Phase 5**: Web GUI — 13-screen SPA with real-time SSE, topology, live audit verification
- **Phase 6**: Production hardening — Federation, PQ handshake, multi-path, plugins, chaos CI, QoS, module publishing

**Verified on:** Go 1.27 (local) + Go 1.26 (WSL CI) | `make lint` + `make test -race` all green

---

## 🔄 Development Protocol (Mandatory)

**After EVERY phase:**
1. Complete phase work (code, tests, docs)
2. Run `make lint && make test -race`
3. Fix ALL errors (lint, tests, build)
4. Commit to GitHub with descriptive message
5. Update ROADMAP.md with phase status
6. **Only then proceed** to next phase

---

## 📅 Phase History

| Phase | Theme | Status | Commit |
|-------|-------|--------|--------|
| 1-2 | Core P2P Stack | ✅ | `9918745` |
| 3 | Beginner UX | ✅ | `44aea3f` |
| 4 | Advanced Capabilities | ✅ | `0ea7376` |
| 5 | Web GUI | ✅ | `bfe5804` |
| 6.1 | Federation (Rendezvous) | ✅ | `e98e233` |
| 6.2 | PQ Hybrid Handshake | ✅ | `7259890` |
| 6.3 | Multi-Path Aggregation | ✅ | `5066d66` |
| 6.4 | Plugin Interface | ✅ | `fef2cee` |
| 6.5 | Chaos CI | ✅ | `1c5c7fe` |
| 6.6 | QoS/Bandwidth Shaping | ✅ | `254c579` |
| 6.7 | Module Publishing | ✅ | `375c935` |

---

## 🚀 Phase 7: Advanced UX & Power Features (Next)

| Sub-phase | Goal | Priority |
|-----------|------|----------|
| 7.1 | Onboarding Wizard (QR pairing, passphrase backup) | High |
| 7.2 | File Transfer UX (drag-drop, progress, resume) | High |
| 7.3 | Collaborative Docs (RGA editor + presence) | High |
| 7.4 | Voice/Video Call UI (WebRTC, screenshare) | High |
| 7.5 | VPN Dashboard (routes, split tunnel, ACLs) | High |
| 7.6 | Registry Search/Install (DHT-backed) | Medium |
| 7.7 | Native Desktop (Wails v3 + system tray) | Medium |
| 7.8 | Mobile Apps (iOS/Android) | Medium |

---

## 🏗️ Phase 8: Native Desktop & Mobile (Future)

| Sub-phase | Goal |
|-----------|------|
| 8.1 | Wails v3 Desktop (native window, tray, notifications) |
| 8.2 | System Tray + Autostart (LaunchAgent/plist, systemd, Task Scheduler) |
| 8.3 | Native Notifications (peer connect, file received, call) |
| 8.4 | iOS App (NetworkExtension + SwiftUI) |
| 8.5 | Android App (VpnService + Jetpack Compose) |
| 8.6 | QR Pairing (mobile ↔ desktop) |

---

## 🏢 Phase 9: Enterprise & Scale (Future)

| Sub-phase | Goal |
|-----------|------|
| 9.1 | Multi-Node Cluster (100+ nodes, gossip discovery) |
| 9.2 | Policy Engine (OPA/Rego, capability tokens) |
| 9.3 | Observability (Prometheus, Grafana, OpenTelemetry) |
| 9.4 | Backup/DR (Age encryption, Shamir social recovery) |

---

## 🔬 Phase 10: Research & Innovation (Future)

| Sub-phase | Goal |
|-----------|------|
| 10.1 | Anonymous Routing (Mixnet, cover traffic) |
| 10.2 | Delay-Tolerant Networking (Bundle protocol, DTN) |
| 10.3 | ML Link Selection (TinyML, federated learning) |

---

## 📋 Definition of Done (All Phases)

- [ ] All tests pass with `-race` on Go 1.26 + 1.27
- [ ] `make lint` = 0 issues
- [ ] E2E tests in `test/integration/` pass
- [ ] Documentation updated (README, godoc, ROADMAP, ARCHITECTURE, TECH_STACK)
- [ ] CHANGELOG entry for each release
- [ ] Signed releases with cosign/sigstore
- [ ] SBOM generated (Syft)
- [ ] Vulnerability scan (govulncheck, Trivy)

---

## 📂 Documentation Structure

```
docs/
├── README.md                 # Quick links
├── architecture/
│   ├── ARCHITECTURE.md      # System architecture (this file)
│   ├── TECH_STACK.md        # Technology stack details
│   └── ROADMAP.md           # This roadmap
├── guides/
│   ├── QUICKSTART.md        # 2-command setup
│   ├── ONBOARDING.md        # First-time user guide
│   ├── CLI_REFERENCE.md     # CLI command reference
│   ├── GUI_GUIDE.md         # Web GUI walkthrough
│   └── SERVICES.md          # All 9 services deep-dive
├── api/
│   ├── REST_API.md          # HTTP API reference
│   ├── WS_API.md            # WebSocket/SSE events
│   └── PLUGIN_API.md        # Plugin development
└── operations/
    ├── DEPLOYMENT.md        # Production deployment
    ├── MONITORING.md        # Observability setup
    └── TROUBLESHOOTING.md   # Common issues
```

---

## 🔗 Quick Links

- **Quickstart**: `make quickstart` (2 commands)
- **CLI Help**: `bin/localweb-cli --help`
- **Web GUI**: `http://localhost:8080` (after node start)
- **API Docs**: `docs/api/REST_API.md`
- **Architecture**: `docs/architecture/ARCHITECTURE.md`
- **Tech Stack**: `docs/architecture/TECH_STACK.md`
- **Roadmap**: `docs/architecture/ROADMAP.md` (this file)

---

*Last updated: 2025-09-05 | Commit: `beff4fd`*
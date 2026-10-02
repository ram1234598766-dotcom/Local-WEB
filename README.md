# LocalWEB

**Real working P2P internet stack. Zero infrastructure required. Better than centralized.**

LocalWEB lets two laptops (or phones, or servers) talk directly to each other
without a VPN, a server, or any account. It figures out the best path between
you — WiFi, Bluetooth, USB, or audio — and encrypts every byte end-to-end.
No cloud. No sign-up. Just two devices on the same network, connecting.

**Want to try it?** Two commands:

```bash
make quickstart          # on machine A
# on machine B (same network):
make quickstart          # then run: bin/localweb-cli peers
```

That's it. Your node IDs will appear, and the two machines will find each
other automatically.

---

## 🎯 What Makes LocalWEB Different

| Traditional | LocalWEB |
|-------------|----------|
| Central server required | **Zero infrastructure** |
| Single point of failure | **Mesh resilience** |
| ISP/cloud sees all traffic | **E2E encryption (Noise XX)** |
| Account required | **No accounts, no sign-up** |
| Single network path | **6 link types + multi-path** |
| Cloud-dependent | **Works offline (BLE, acoustic)** |
| Closed source | **Open source, auditable** |

---

## 🏗️ Architecture: 9-Layer P2P Stack

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  L9: APPLICATION          Node Daemon │ CLI Client │ Web GUI (SPA)         │
├─────────────────────────────────────────────────────────────────────────────┤
│  L8: SERVICES           DNS │ HTTP │ Email │ Docs │ Files │ Messaging │   │
│       │  Registry │ Voice │ VPN                                            │
├─────────────────────────────────────────────────────────────────────────────┤
│  L7: CRDT               ORSet │ RGA │ LWW-Register │ Merkle DAG Sync       │
├─────────────────────────────────────────────────────────────────────────────┤
│  L6: DATA               BadgerDB (AES-GCM) │ BlockStore │ PeerStore │     │
├─────────────────────────────────────────────────────────────────────────────┤
│  L5: DHT                Kademlia (k=20, α=3) │ XOR Routing │ PoW Anti-Sybil│
├─────────────────────────────────────────────────────────────────────────────┤
│  L4: SECURITY           Noise XX │ AES-GCM │ Ed25519 │ SHA3-256 │        │
│       │  Capability Tokens │ PoW │ Audit Log (SHA3 Hash Chain)            │
├─────────────────────────────────────────────────────────────────────────────┤
│  L3: DISCOVERY          mDNS-SD │ BLE │ Rendezvous (Federation) │        │
├─────────────────────────────────────────────────────────────────────────────┤
│  L2: LINK               WiFi │ WiFi-Direct │ Ad-hoc │ USB │ BLE │ Acoustic│
├─────────────────────────────────────────────────────────────────────────────┤
│  L1: TRANSPORT          QUIC (Noise XX) │ Stream Mux (1-byte ServiceID)   │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 🚀 Quick Start (2 Commands)

### From Source (Development)
```bash
# Machine A
git clone https://github.com/ram1234598766-dotcom/Local-WEB.git
cd Local-WEB
make quickstart

# Machine B (same network)
make quickstart
# Then:
bin/localweb-cli peers
```

### Pre-built Installers (Recommended)

| OS | Download | Install |
|----|----------|---------|
| **Windows** | [Latest `.exe`](https://github.com/ram1234598766-dotcom/Local-WEB/releases/latest) | Run as Administrator |
| **macOS** | [Latest `.dmg`](https://github.com/ram1234598766-dotcom/Local-WEB/releases/latest) | Drag to Applications |
| **Linux** | [`.deb`](https://github.com/ram1234598766-dotcom/Local-WEB/releases/latest) / [`.rpm`](https://github.com/ram1234598766-dotcom/Local-WEB/releases/latest) / [`.apk`](https://github.com/ram1234598766-dotcom/Local-WEB/releases/latest) | `sudo dpkg -i` / `sudo rpm -i` / `apk add` |

### One-Line Installer Scripts (Fastest)

```bash
# Linux (Debian/Ubuntu/RHEL/Fedora/Alpine/Arch)
curl -fsSL https://raw.githubusercontent.com/ram1234598766-dotcom/Local-WEB/main/installers/linux/install.sh | sudo bash

# macOS (requires curl + hdiutil)
curl -fsSL https://raw.githubusercontent.com/ram1234598766-dotcom/Local-WEB/main/installers/macos/install.sh | bash

# Windows (PowerShell, run as Administrator)
iwr -useb https://raw.githubusercontent.com/ram1234598766-dotcom/Local-WEB/main/installers/windows/install.ps1 | iex
```

These scripts automatically:
- Detect your OS/architecture
- Download the correct installer from GitHub Releases
- Install dependencies (Wintun on Windows, LaunchDaemon on macOS, systemd on Linux)
- Configure firewall rules
- Start the LocalWEB service
- Print your node ID

---

## 📦 Installation by Platform

### 🪟 Windows

**Requirements:** Windows 10/11 (64-bit), Administrator, Internet (for Wintun driver)

1. Download `LocalWEB-Setup.exe` from [Releases](https://github.com/ram1234598766-dotcom/Local-WEB/releases/latest)
2. Right-click → **Run as Administrator**
3. Follow the wizard:
   - Choose components (Core, Windows Service, Shortcuts, Wintun Driver)
   - Select install directory
4. After install: LocalWEB service starts automatically

**Post-install:**
- LocalWEB service runs at boot (if selected)
- Wintun driver installed to `C:\Windows\System32\drivers\wintun.dll`
- Firewall rules added for the `localweb.exe` **program** (any port it uses, on
  Domain and Private profiles only) — so no port list to keep in sync
- Start Menu + Desktop shortcuts created

**CLI:** `localweb-cli peers` (available in PATH after install)

**Uninstall:** Settings → Apps → LocalWEB → Uninstall

---

### 🍎 macOS

**Requirements:** macOS 13.0+ (Ventura), Apple Silicon (arm64) or Intel (x86_64)

**Install:**
1. Download `LocalWEB.dmg` from [Releases](https://github.com/ram1234598766-dotcom/Local-WEB/releases/latest)
2. Open `.dmg` and drag **LocalWEB.app** to **Applications**
3. First launch: right-click → **Open** (bypasses Gatekeeper if unsigned)

**Post-install:**
- LaunchDaemon loads at boot: `sudo launchctl load /Library/LaunchDaemons/com.localweb.daemon.plist`
- First launch prompts for: **Local Network**, **Bluetooth**, **Network Extension (VPN)**
- VPN requires **Network Extension** approval in **System Settings → General → VPN & Network**

**CLI:** `localweb-cli peers` (symlinked to `/usr/local/bin/localweb-cli`)

**VPN Note:** Requires **Network Extension** entitlement (Apple Developer Program). Without it, VPN service is unavailable; other features work normally.

---

### 🐧 Linux

**Requirements:** systemd (247+), Kernel 5.10+, CAP_NET_ADMIN (via setcap, not root)

#### Debian/Ubuntu
```bash
sudo dpkg -i localweb_1.0.0_linux_amd64.deb
sudo apt-get install -f
```

#### RHEL/Fedora
```bash
sudo rpm -i localweb-1.0.0-1.x86_64.rpm
```

#### Alpine
```bash
apk add localweb-1.0.0-r1.apk
```

#### Arch
```bash
sudo pacman -U localweb-1.0.0-1-x86_64.pkg.tar.zst
```

**Post-install:**
```bash
# Start & enable service
sudo systemctl start localweb
sudo systemctl enable localweb

# Check status
systemctl status localweb
journalctl -u localweb -f

# Config
cat /etc/localweb/config.json
```

**No root required:** Binaries use `setcap` for capabilities:
```bash
setcap 'cap_net_admin,cap_net_bind_service,cap_net_raw,cap_sys_admin,cap_dac_override,cap_dac_read_search,cap_sys_resource,cap_sys_nice+ep' /usr/bin/localweb
```

**Firewall auto-configured:** ufw / firewalld / iptables

Opens UDP `4443` (QUIC) and `5353` (DNS/mDNS), plus TCP `8082` (HTTP), `587`
(SMTP), `993` (IMAP), `9092` (registry) and `9094` (DHT) — the ports the node
actually listens on. The dashboard's `8080` is **not** opened, because it binds
loopback by default. Uninstalling removes all of them, including `8080` so that
upgrading from an older release clears the stale rule.

**Uninstall:** `sudo dpkg -r localweb` / `sudo rpm -e localweb` / `apk del localweb`

---

## 🔐 Security Notes

| Platform | Warning | Workaround |
|----------|---------|------------|
| **Windows** | SmartScreen "Windows protected your PC" | More info → Run anyway |
| **macOS** | "Unidentified Developer" | Right-click → Open, or `xattr -d com.apple.quarantine` |
| **Linux** | Capabilities instead of root | Uses `setcap` (no root daemon) |

**Signing Status:** Current builds are **unsigned**. See [SECURITY.md](SECURITY.md) for details.

---

## 🔧 Build from Source

```bash
git clone https://github.com/ram1234598766-dotcom/Local-WEB.git
cd Local-WEB
make build          # Build all binaries
make test           # Run tests (with race detector)
make lint           # Lint & format check
make cross-compile  # Build for all platforms
```

Your node ID prints on startup and is stored in `~/.localweb/identity.json`.

### Native audio and video encoders (optional)

A default build can **receive** voice calls — the Opus decoder is pure Go and
always linked — but it cannot **send**, because there is no pure-Go Opus, VP8 or
VP9 encoder. Rather than send raw PCM under an Opus payload type and let a browser
decode noise, a default build reports `ErrNoOpusEncoder`.

To get real encoders, build against the native libraries:

```bash
# Debian/Ubuntu
sudo apt install libopus-dev libvpx-dev

# macOS
brew install opus vpx

# Windows (vcpkg)
vcpkg install opus libvpx

go build -tags libopus,libvpx ./cmd/node
```

The tags are opt-in deliberately: CI builds with `CGO_ENABLED=1` on ubuntu and
macOS, where neither native library is installed, so gating on cgo alone would
break both. Check what a given binary actually has:

```bash
curl -s localhost:8080/api/voice/status | jq
# { "service_live": true, "opus_encoder": true, "vp8_encoder": true,
#   "can_send_audio": true, "can_send_video": true, "calls": [] }
```

---

## ✈️ Works Offline

A node needs no internet connection to run. The dashboard, DNS, HTTP, email,
files, docs and the registry are all served by the node itself, the web UI
loads no remote font, script or stylesheet, and there is no telemetry.

Discovery on your local network is mDNS, not the internet. Federation — finding
peers across the internet — is off unless you pass `-rendezvous <url>`, so a
default node never contacts anything off-subnet.

Building is offline too. Every Go dependency is committed under `vendor/`, so no
module proxy is needed:

```bash
make verify-offline   # builds, vets and tests with no network access
```

`vendor/` is committed, so no module proxy is needed. The build also pins
`GOTOOLCHAIN=local`, because Go otherwise resolves the toolchain version named
in `go.mod` *through* the module cache and a machine with Go already installed
but an empty cache would still try to download one.

Verified from a fresh clone with an empty module cache and `GOPROXY=off`:
`go build ./...` and `go test ./...` both pass.

`scripts/check-offline.sh` is the guard that keeps this true. It fails if the
embedded UI gains a reference to another origin, if runtime code hardcodes a
public endpoint, or if `vendor/` goes missing. It runs in CI as the "Offline
Build" job, where a module fetch is a hard failure.

One exception, on the build machine only: the Windows installers package the
Wintun TUN driver, and `make deps-wintun` downloads it once from
`wintun.net`, verifying the archive and DLL checksums. The script skips the
download when a verified copy is already present, so packaging works offline
after that first fetch. The shipped binaries contain no network dependency.

---

## ✨ Key Features

| Category | Feature |
|----------|---------|
| **Transport** | QUIC (quic-go v0.62) + Noise XX (X25519 + SHA3-256) + **Post-Quantum Hybrid (X25519 + Kyber-768)** |
| **Link Layer** | 6 types: WiFi Station, WiFi Direct, Ad-hoc, USB Tether, BLE, Acoustic FSK |
| **Discovery** | mDNS-SD, BLE GATT, **Rendezvous (cross-LAN federation)** |
| **DHT** | Kademlia (k=20, α=3), XOR routing, PoW anti-Sybil |
| **CRDT** | ORSet (add-wins), RGA (collab text), Merkle DAG sync |
| **Store** | BadgerDB + AES-256-GCM, content-addressed (CID), Merkle DAG |
| **Security** | Noise XX + AES-GCM, Ed25519 identity, **Hybrid PQ (X25519+Kyber)**, Capability tokens, PoW, Audit log (SHA3 hash chain) |
| **QoS** | Token bucket per service/peer, HTB hierarchy, 9 pre-configured classes |
| **Chaos Engineering** | 6 built-in scenarios, nightly CI, fault injection |
| **Plugin System** | Go plugin loader + BuiltinPlugin framework |

---

## 📦 9 Built-in Services

| Service | Protocol | Listens on | Key Feature |
|---------|----------|-------------|-------------|
| **Transport** | QUIC over Noise XX | `4443/udp` | The single socket every stream service below is multiplexed onto |
| **DNS** | mDNS + UDP | `5353/udp` | `.localweb` TLD, signed records |
| **HTTP** | HTTP/1.1 | `8082/tcp` | Per-site routing, health |
| **Email** | SMTP + IMAP | `587/tcp`, `993/tcp` | Maildir, PoW antispam |
| **Messaging** | QUIC stream (`ServiceMsg`) | — | Signed messages, offline queue. No listener of its own: it is a stream on the QUIC transport |
| **Files** | QUIC stream + WebRTC SCTP (`ServiceFS`) | — | BlockStore + Merkle DAG sync; two nodes exchange blocks on their own |
| **Docs** | QUIC stream (`ServiceDocs`) | — | RGA CRDT, presence and cursors broadcast over SSE |
| **Registry** | HTTP + DHT | `9092/tcp`, DHT `9094/tcp` | LWPKG (tar.gz + Ed25519 sig) |
| **Voice** | WebRTC ICE/DTLS/SRTP (`ServiceVoice`) | — | Opus always decodable; encoding needs `-tags libopus`, VP8/VP9 needs `-tags libvpx` |
| **VPN** | QUIC stream (`ServiceVPN`) | — | Route dist, split tunnel. Forwarding loop and `StreamCarrier` real; opening a TUN device needs `CAP_NET_ADMIN` on Linux/macOS, and has no implementation on Windows |
| **Dashboard** | HTTP over TCP | `127.0.0.1:8080` | Unauthenticated, loopback-only by default. Not a P2P service |

**Why most rows have no port:** all the QUIC-stream services are multiplexed onto
the single QUIC transport by a one-byte service ID. They do not each bind a
socket, so there is nothing to firewall and nothing to configure. The ports that
do exist are the ones with their own listener, plus the DHT, which speaks TCP on
its own plane.

`/api/services/health` reports which services are actually running, and
`/api/voice/status` reports what a given binary can encode.

---

## 📚 Documentation

| Doc | Description |
|-----|-------------|
| [`docs/architecture/ARCHITECTURE.md`](docs/architecture/ARCHITECTURE.md) | Complete system architecture (9 layers) |
| [`docs/architecture/TECH_STACK.md`](docs/architecture/TECH_STACK.md) | Technology stack details |
| [`docs/architecture/ROADMAP.md`](docs/architecture/ROADMAP.md) | Master roadmap + dev protocol |
| [`docs/guides/QUICKSTART.md`](docs/guides/QUICKSTART.md) | 2-command setup guide |
| [`docs/guides/CLI_REFERENCE.md`](docs/guides/CLI_REFERENCE.md) | CLI command reference |
| [`docs/guides/GUI_GUIDE.md`](docs/guides/GUI_GUIDE.md) | Web GUI walkthrough |
| [`docs/guides/SERVICES.md`](docs/guides/SERVICES.md) | All 9 services deep-dive |
| [`docs/api/REST_API.md`](docs/api/REST_API.md) | HTTP API reference |
| [`docs/api/WS_API.md`](docs/api/WS_API.md) | WebSocket/SSE events |
| [`docs/api/PLUGIN_API.md`](docs/api/PLUGIN_API.md) | Plugin development |

---

## 🛠️ Quick Commands

```bash
# Build everything
make build

# Run all tests with race detector
make test

# Run linting
make lint

# Start node (quickstart = build + identity + run)
make quickstart

# Run CLI
make run-cli

# Run benchmarks
make bench

# Cross-compile for all platforms
make cross-compile
```

---

## 📋 Status Dashboard

| Layer | Component | Status | Tests |
|-------|-----------|--------|-------|
| L1 | Transport (QUIC + Noise XX) | ✅ Verified | `quic_test.go` |
| L2 | Link (6 types) | ✅ Verified | Runtime detection |
| L3 | Discovery (mDNS/BLE/Rendezvous) | ✅ Verified | Orchestrator tests |
| L4 | DHT (Kademlia) | ✅ Verified | `dht_test.go` |
| L5 | Security (Noise XX + Hybrid PQ) | ✅ Verified | `audit_test.go`, `pow_test.go` |
| L6 | Store (BadgerDB + AES-GCM) | ✅ Verified | `store_test.go` |
| L7 | CRDT (ORSet + RGA) | ✅ Verified | `crdt_test.go` |
| L8 | Services (9) | ✅ Verified | Integration tests |
| L9 | App (Node + CLI + Web GUI) | ✅ Verified | Identity persistence |

---

## 🔐 Security

| Layer | Mechanism |
|-------|-----------|
| **Transport** | Noise XX (X25519 + SHA3-256) + **Hybrid PQ (X25519 + Kyber-768)** |
| **Identity** | Ed25519 keypair, NodeID = SHA3-256(PubKey) |
| **Store** | AES-256-GCM at rest (BadgerDB) |
| **Access Control** | Ed25519-signed capability tokens (canonical JSON) |
| **Spam/Sybil** | SHA3-based Proof of Work (auto-adjusting difficulty) |
| **Audit** | Append-only SHA3-256 hash chain (tamper-evident) |

---

## 🌐 Web GUI (Optional)

```bash
# Enable web dashboard (loopback only by default, and unauthenticated)
go run ./cmd/node --dashboard
# Open http://localhost:8080
```

The dashboard has **no authentication** and is not read-only: it can create and
save documents, upload files into the local store, and restore a backup. It binds
`127.0.0.1` for that reason. To reach it from another machine, expose it
deliberately and put your own authentication in front of it:

```bash
go run ./cmd/node --dashboard -gui-addr 0.0.0.0:8080
```

**13 Screens:** Dashboard, Network/Peers (topology), Files, DNS, HTTP, Email, Messaging, Docs, Registry, Voice, VPN, Security (live audit-chain), Settings

---

## 🧪 Testing

```bash
# Full suite with race detector
make test

# Linting
make lint

# Benchmarks
make bench

# Coverage
go test -coverprofile=coverage.out ./...
```

---

## 📄 License

MIT. See [SECURITY.md](SECURITY.md) for vulnerability reporting.

---

## 📖 Quick Links

| Topic | Link |
|-------|------|
| Architecture | `docs/architecture/ARCHITECTURE.md` |
| Tech Stack | `docs/architecture/TECH_STACK.md` |
| Roadmap | `docs/architecture/ROADMAP.md` |
| Quickstart | `docs/guides/QUICKSTART.md` |
| CLI Reference | `docs/guides/CLI_REFERENCE.md` |
| GUI Guide | `docs/guides/GUI_GUIDE.md` |
| Services | `docs/guides/SERVICES.md` |
| REST API | `docs/api/REST_API.md` |
| Plugin API | `docs/api/PLUGIN_API.md` |

---

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for:
- TDD workflow
- Commit conventions (`feat:`, `fix:`, `refactor:`, `test:`, `docs:`)
- Security review process
- Code review gates

---

## ⚖️ License

MIT. See [SECURITY.md](SECURITY.md) for vulnerability reporting.

---

## 👤 Author

**Mrityunjay K**

---

*LocalWEB v1.0.0 | Module: `github.com/ram1234598766-dotcom/Local-WEB` | Go 1.26+*

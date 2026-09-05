# LocalWEB Quick Start Guide

Get two devices talking in 60 seconds.

---

## Prerequisites

- Go 1.26+
- Git
- `make` (for Makefile targets)

---

## 60-Second Setup

### Machine A (First Node)

```bash
git clone https://github.com/ram1234598766-dotcom/Local-WEB.git
cd Local-WEB
make quickstart
```

Output:
```
Node ID: a1b2c3d4
Public Key: 0123456789abcdef...
Listening on :4443
GUI: http://localhost:8080 (if --dashboard enabled)
```

### Machine B (Second Node)

Same commands on another device on the **same network**:

```bash
git clone https://github.com/ram1234598766-dotcom/Local-WEB.git
cd Local-WEB
make quickstart
```

Then discover peers:
```bash
bin/localweb-cli peers
```

Output:
```
ID        NAME          SOURCE      ADDRESSES              SCORE
a1b2c3d4  laptop-john   mDNS        192.168.1.50:4443    0.95
e5f6g7h8  laptop-jane   mDNS        192.168.1.51:4443    0.92
```

---

## What Just Happened

1. **Identity generated** — Ed25519 keypair created, stored in `~/.localweb/identity.json`
2. **Node started** — QUIC transport listening on UDP 4443
3. **Discovery started** — mDNS + BLE scanning for peers
4. **Services started** — All 9 services listening on QUIC streams

---

## Verify Connection

```bash
# List peers
bin/localweb-cli peers

# Test messaging
bin/localweb-cli messaging create --name test --peers <peer-id>
bin/localweb-cli messaging send --channel test --text "Hello!"
```

---

## Next Steps

| Task | Command |
|------|---------|
| Send a file | `bin/localweb-cli send --peer <id> --file ./photo.jpg` |
| Send message | `bin/localweb-cli messaging send --channel test --text "Hi"` |
| View peers | `bin/localweb-cli peers` |
| Start VPN | `bin/localweb-cli vpn connect --peer <id>` |
| Enable web GUI | `go run ./cmd/node --dashboard` then open `http://localhost:8080` |

---

## Common Issues

| Problem | Solution |
|---------|----------|
| "No peers found" | Both devices must be on same LAN. Check firewall. |
| "Port in use" | Change port: `./bin/node --listen :4444` |
| "VPN needs root" | Run with `sudo` on Linux/macOS |
| "Permission denied" | Allow LocalWEB in firewall settings |

---

## Next Steps

| Goal | Guide |
|------|-------|
| Send files | `docs/guides/CLI_REFERENCE.md#send` |
| Chat securely | `docs/guides/CLI_REFERENCE.md#messaging` |
| Browse peers | `docs/guides/CLI_REFERENCE.md#peers` |
| Use web GUI | `docs/guides/GUI_GUIDE.md` |
| All commands | `docs/guides/CLI_REFERENCE.md` |

---

*LocalWEB v1.0.0 | `make quickstart` = 2 commands to connect*
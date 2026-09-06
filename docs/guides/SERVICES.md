# LocalWEB Services Deep-Dive

All 9 services are fully implemented and integrated. Each runs as a handler on a QUIC stream (identified by 1-byte ServiceID).

**Author: Mrityunjay K**

---

## Service Overview

| Service | Package | ServiceID | Protocol | Port |
|---------|---------|-----------|----------|------|
| DNS | `pkg/services/dns/` | 0x01 | mDNS/DoH | 5353 |
| HTTP | `pkg/services/http/` | 0x02 | HTTP/1.1 over QUIC | 8080 |
| Email | `pkg/services/email/` | 0x03 | SMTP + IMAP | 587/993 |
| Messaging | `pkg/services/messaging/` | 0x04 | Pub/sub over QUIC | 9090 |
| Files | `pkg/services/files/` | 0x05 | Bitswap-like | 9091 |
| Docs | `pkg/services/docs/` | 0x06 | CRDT over messaging | 9092 |
| Registry | `pkg/services/registry/` | 0x07 | HTTP + DHT | 9093 |
| Voice | `pkg/services/voice/` | 0x08 | Signaling + media | 9093 |
| VPN | `pkg/services/vpn/` | 0x09 | TUN + QUIC | 9094 |

---

## 1. DNS Service (`pkg/services/dns/`)

**Purpose:** Resolve `.localweb` domains on the local network.

### Features
- mDNS-SD (RFC 6762/6763) on UDP 5353
- Signed zone records (Ed25519)
- Automatic registration of local services
- Cross-subnet via rendezvous (Phase 6.1)

### API
```bash
# Query
nslookup host.localweb
dig @127.0.0.1 -p 5353 host.localweb

# Register (automatic for local services)
# Records signed with node's Ed25519 key
```

### Records
| Type | Description |
|------|-------------|
| A | IPv4 address |
| AAAA | IPv6 address |
| TXT | Service metadata (port, ServiceID) |
| SRV | Service location (port, priority) |

---

## 2. HTTP Gateway (`pkg/services/http/`)

**Purpose:** HTTP/1.1 gateway for web services over QUIC.

### Features
- Per-site routing (host-based)
- Health endpoint: `GET /health`
- Logging middleware
- Per-site access logs

### Configuration
```json
{
  "sites": [
    {
      "host": "app.localweb",
      "target": "http://127.0.0.1:3000"
    }
  ]
}
```

### Endpoints
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Gateway health |
| `/*` | * | Proxied to registered site |

---

## 3. Email (`pkg/services/email/`)

**Purpose:** Full SMTP + IMAP with PoW antispam.

### Protocols
| Protocol | Port | Features |
|----------|------|----------|
| SMTP | 587 | STARTTLS, PoW challenge, Maildir storage |
| IMAP | 993 | TLS, Maildir, IDLE support |

### PoW Antispam
- Every inbound SMTP requires SHA3-256 PoW
- Difficulty auto-adjusts (~1s solve time)
- Prevents spam without central filter

### Maildir Storage
```
~/.localweb/data/mail/
├── new/
├── cur/
└── tmp/
```

### Client Config
```
IMAP: localhost:993 (SSL/TLS)
SMTP: localhost:587 (STARTTLS)
User: <node-name>@localweb
Pass: (none - auth via node identity)
```

---

## 4. Messaging (`pkg/services/messaging/`)

**Purpose:** Signed pub/sub channels over QUIC.

### Features
- Ed25519-signed messages
- Persistent history (Maildir-style)
- Offline queue with redelivery
- Channel-based (multi-peer)

### API
```go
// Create channel
chID := svc.CreateChannel([]*[32]byte{pubKey1, pubKey2})

// Publish
msg, _ := svc.Publish(ctx, chID, myPubKey, []byte("hello"), "")

// History
history, _ := svc.History(chID, "", 100)
```

### CLI
```bash
# Create channel
localweb-cli messaging create --name "team" --peers a1b2c3d4,e5f6g7h8

# Send
localweb-cli messaging send --channel team --text "Hello!"

# History
localweb-cli messaging history --channel team --limit 50
```

---

## 5. Files (`pkg/services/files/`)

**Purpose:** Bitswap-like content-addressed file transfer.

### Architecture
```
File → Chunks (1MB) → SHA3-256 CID → BlockStore
                    ↓
            Merkle DAG → Root CID
                    ↓
            BitSwap protocol over QUIC
```

### Features
- Content-addressed (CID = SHA3-256)
- Zstd compression
- Merkle DAG diff sync (resumable)
- BlockStore + FileStore separation

### API
```bash
# Upload
curl -T myfile.zip http://localhost:9091/files/myfile.zip

# Download
curl http://localhost:9091/files/<cid> --output myfile.zip

# CLI
localweb-cli send --peer <id> --file ./photo.jpg
localweb-cli get --peer <id> --cid <cid> --output ./photo.jpg
```

### BlockStore
| Operation | Method |
|-----------|--------|
| Put | `Put(ctx, cid, data)` |
| Get | `Get(ctx, cid)` |
| Has | `Has(ctx, cid)` |
| List | `List(prefix)` |

---

## 6. Docs (`pkg/services/docs/`)

**Purpose:** Real-time collaborative text editing.

### Technology
- **RGA (Replicated Growable Array)** — CRDT for text
- **Operational Transform** — Insert/delete ops
- **Presence** — Cursors, selections, peer colors

### Features
- Real-time co-editing
- Undo/Redo per peer
- Block formatting (headings, lists, code)
- Export: Markdown, HTML, Plain text

### API
```go
// Create document
doc := svc.CreateDocument("notes.md")

// Apply local operation
svc.ApplyLocalOp(docID, op)

// Subscribe to changes
svc.SetNotifier(docID, func(op Op) { ... })

// Export
markdown, _ := svc.ExportToMarkdown(docID)
```

### GUI
Open `http://localhost:9092/docs/` for full editor.

---

## 7. Registry (`pkg/services/registry/`)

**Purpose:** Decentralized package registry over DHT.

### Package Format (LWPKG)
```
package.lwpkg (tar.gz)
├── manifest.yaml    # Name, version, author, deps, entry
├── signature.sig    # Ed25519 signature
└── payload/         # Binary assets
```

### Manifest Schema
```yaml
name: my-package
version: 1.2.0
author: "pubkey-hex"
description: "A useful package"
entry: main.go
platforms: [linux/amd64, linux/arm64]
checksums:
  - sha256:abc123...
```

### API
```bash
# List
curl http://localhost:9093/packages

# Search
curl "http://localhost:9093/packages?q=web&platform=linux/amd64"

# Get
curl http://localhost:9093/packages/my-package/1.2.0

# Publish (requires signing key)
curl -T package.lwpkg http://localhost:9093/packages

# CLI
localweb-cli registry list
localweb-cli registry search "web framework"
localweb-cli registry install my-package
localweb-cli registry publish ./my-package.lwpkg
```

---

## 8. Voice (`pkg/services/voice/`)

**Purpose:** WebRTC voice/video calls.

### Architecture
```
Signaling (QUIC) → ICE → DTLS-SRTP → Media (Opus/VP9)
```

### Call State Machine
```
Idle → Calling → Ringing → Connected → Ended
                ↘      ↗
              Failed
```

### Codecs
| Type | Codec | Profile |
|------|-------|---------|
| Audio | Opus | 48kHz, stereo |
| Video | VP9 | 720p/30fps |

### ICE
- STUN for public IP discovery
- TURN relay fallback (via rendezvous)
- Candidate gathering on all interfaces

### CLI
```bash
# Start call
localweb-cli voice call --peer <peer-id>

# Answer
localweb-cli voice answer --call <call-id>

# End
localweb-cli voice hangup --call <call-id>

# Mute
localweb-cli voice mute --call <call-id>
```

---

## 9. VPN (`pkg/services/vpn/`)

**Purpose:** Mesh VPN over QUIC tunnels.

### Architecture
```
TUN Interface ←→ QUIC Tunnel ←→ Peer TUN Interface
```

### Features
- TUN interface creation (requires root)
- Route distribution via DHT
- Split tunnel (selective routes)
- SHA3-256 tunnel encryption

### Routes
| Type | Description |
|------|-------------|
| Peer route | Direct to peer's LAN |
| Exit route | Peer as gateway |
| Custom | User-defined CIDRs |

### CLI
```bash
# Connect
localweb-cli vpn connect --peer <peer-id>

# Check status
localweb-cli vpn status

# Routes
localweb-cli vpn routes

# Disconnect
localweb-cli vpn disconnect
```

### TUN Interface
| OS | Interface | Command |
|----|-----------|---------|
| Linux | `tun0` | `ip addr show dev tun0` |
| macOS | `utun0` | `ifconfig utun0` |
| Windows | `Wintun` | `Get-NetAdapter` |

---

## Service Discovery

All services auto-register on startup:
```go
server.RegisterHandler(ServiceID, handler)
```

ServiceIDs:
| Service | ID | Byte |
|---------|----|------|
| Control | 0x00 | 0 |
| DNS | 0x01 | 1 |
| HTTP | 0x02 | 2 |
| Email | 0x03 | 3 |
| Messaging | 0x04 | 4 |
| Files | 0x05 | 5 |
| Docs | 0x06 | 6 |
| Registry | 0x07 | 7 |
| Voice | 0x08 | 8 |
| VPN | 0x09 | 9 |

---

## Extending Services

```go
// 1. Implement Service interface
type MyService struct {
    nodeID [32]byte
    store  *store.Store
}

func (s *MyService) Start() error { ... }
func (s *MyService) Stop() error { ... }
func (s *MyService) Handle(stream transport.Stream) { ... }

// 2. Register in node
server.RegisterHandler(ServiceMyCustom, myService.Handle)
```

---

*LocalWEB Services v1.0.0 | All 9 services verified with integration tests*
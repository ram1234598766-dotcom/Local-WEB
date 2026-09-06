# LocalWEB CLI Reference

**Binary: `localweb-cli` (built from `cmd/cli/`)**

**Author: Mrityunjay K**

---

## Commands

### `localweb-cli id`

Display or generate node identity.

```bash
localweb-cli id                    # Display current identity
localweb-cli id --generate         # Generate new identity
localweb-cli id --json             # Machine-readable JSON output
```

**Output (text):**
```
Node ID: a1b2c3d4
Public Key: 0123456789abcdef...
Data Dir: /home/user/.localweb
```

**Output (JSON):**
```json
{
  "node_id": "a1b2c3d4",
  "public_key": "0123456789abcdef...",
  "data_dir": "/home/user/.localweb"
}
```

---

### `localweb-cli peers`

List discovered peers on the network.

```bash
localweb-cli peers                 # Human-readable table
localweb-cli peers --json          # Machine-readable JSON
localweb-cli peers --verbose       # Include all metadata
```

**Output (table):**
```
ID        NAME          SOURCE      ADDRESSES              SCORE    LATENCY
a1b2c3d4  laptop-john   mDNS        192.168.1.50:4443    0.95     2ms
e5f6g7h8  phone-jane    BLE         [fe80::1]:4443       0.72     15ms
```

**Output (JSON):**
```json
[
  {
    "id": "a1b2c3d4",
    "name": "laptop-john",
    "source": "mDNS",
    "addresses": ["192.168.1.50:4443"],
    "score": 0.95,
    "latency": "2ms"
  }
]
```

---

### `localweb-cli node`

Start the node daemon.

```bash
localweb-cli node                                    # Start with defaults
localweb-cli node --name "my-laptop"                # Set display name
localweb-cli node --listen :4444                    # Custom port
localweb-cli node --storage ./data                  # Custom storage path
localweb-cli node --data-dir ./keys                 # Custom identity path
localweb-cli node --rendezvous https://server.url   # Enable federation
localweb-cli node --hybrid                          # Enable PQ handshake
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | hostname | Display name |
| `--listen` | `:4443` | QUIC listen address |
| `--storage` | `~/.localweb/data` | BadgerDB path |
| `--data-dir` | `~/.localweb` | Identity/keys path |
| `--rendezvous` | (empty) | Rendezvous server URL |
| `--rendezvous-register` | `true` | Register with rendezvous |
| `--rendezvous-poll` | `60s` | Poll interval |
| `--hybrid` | `false` | Enable PQ hybrid handshake |
| `--dashboard` | `false` | Enable web GUI on :8080 |

---

### `localweb-cli init`

Interactive first-time setup (creates identity + config).

```bash
localweb-cli init
# Prompts for:
#   - Node name
#   - Data directory
#   - Storage directory
#   - Auto-start preference
```

---

### `localweb-cli send`

Send a file to a peer.

```bash
localweb-cli send --peer <peer-id> --file ./myfile.zip
localweb-cli send --peer a1b2c3d4 --file ./doc.pdf --name "document.pdf"
```

---

### `localweb-cli get`

Receive a file from a peer.

```bash
localweb-cli get --peer <peer-id> --cid <cid> --output ./downloaded.zip
```

---

### `localweb-cli dns`

Manage `.localweb` DNS records.

```bash
localweb-cli dns list                    # List local records
localweb-cli dns add myhost 192.168.1.50 # Add A record
localweb-cli dns remove myhost           # Remove record
```

---

### `localweb-cli registry`

Package registry operations.

```bash
localweb-cli registry list               # List available packages
localweb-cli registry search <term>      # Search packages
localweb-cli registry install <pkg>      # Install package
localweb-cli registry publish ./pkg.tgz  # Publish package
```

---

### `localweb-cli messaging`

Messaging operations.

```bash
localweb-cli messaging channels          # List channels
localweb-cli messaging create --name <name> --peers <peer-ids>
localweb-cli messaging send --channel <id> --text "Hello"
localweb-cli messaging history --channel <id> --limit 50
```

---

### `localweb-cli voice`

Voice call operations.

```bash
localweb-cli voice call --peer <peer-id>     # Start call
localweb-cli voice answer --call <call-id>   # Answer incoming
localweb-cli voice hangup --call <call-id>   # End call
localweb-cli voice mute --call <call-id>     # Mute/unmute
```

---

### `localweb-cli vpn`

VPN operations.

```bash
localweb-cli vpn connect --peer <peer-id>   # Connect to peer VPN
localweb-cli vpn disconnect                 # Disconnect
localweb-cli vpn routes                     # Show active routes
localweb-cli vpn status                     # Connection status
```

---

### `localweb-cli vpn`

VPN operations.

```bash
localweb-cli vpn connect --peer <peer-id>   # Connect to peer VPN
localweb-cli vpn disconnect                 # Disconnect
localweb-cli vpn routes                     # Show active routes
localweb-cli vpn status                     # Connection status
```

---

## Global Flags

| Flag | Description |
|------|-------------|
| `--json` | Output JSON instead of human-readable |
| `--config` | Config file path (default: `~/.localweb/config.json`) |
| `--help` | Show help for command |

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | Not found |
| 4 | Permission denied |
| 5 | Network error |

---

## Examples

### Basic peer discovery
```bash
# Start node in background
localweb-cli node &

# On another machine
localweb-cli peers
```

### File transfer
```bash
# Sender
localweb-cli send --peer a1b2c3d4 --file ./photo.jpg

# Receiver (auto-receives if running)
```

### Messaging
```bash
localweb-cli messaging create --name "team" --peers a1b2c3d4,e5f6g7h8
localweb-cli messaging send --channel team --text "Hello team!"
```

### VPN
```bash
localweb-cli vpn connect --peer a1b2c3d4
# Check routes
localweb-cli vpn routes
```

### Federation (cross-LAN)
```bash
# Start with rendezvous server
localweb-cli node --rendezvous https://rendezvous.localweb.io --rendezvous-register
```

---

## Configuration

**Config file:** `~/.localweb/config.json`

```json
{
  "node": {
    "name": "my-laptop",
    "listen": ":4443",
    "data_dir": "~/.localweb",
    "storage": "~/.localweb/data"
  },
  "rendezvous": {
    "url": "https://rendezvous.localweb.io",
    "register": true,
    "poll_interval": "60s"
  },
  "gui": {
    "enabled": false,
    "listen": "localhost:8080"
  },
  "hybrid": false
}
```

---

## Environment Variables

| Variable | Config Override |
|----------|-----------------|
| `LOCALWEB_NAME` | `--name` |
| `LOCALWEB_LISTEN` | `--listen` |
| `LOCALWEB_STORAGE` | `--storage` |
| `LOCALWEB_DATA_DIR` | `--data-dir` |
| `LOCALWEB_RENDEZVOUS` | `--rendezvous` |
| `LOCALWEB_HYBRID` | `--hybrid` |

---

*Generated from `cmd/cli/` | LocalWEB v1.0.0*
# LocalWEB Web GUI Guide

The optional read-only web dashboard provides a visual overview of your node.

**Author: Mrityunjay K**

---

## Enable the GUI

```bash
# Start node with dashboard
go run ./cmd/node --dashboard

# Or build and run
go build -o bin/node ./cmd/node
./bin/node --dashboard
```

Then open **http://localhost:8080** in your browser.

---

## Screens Overview

The GUI has 13 screens accessible via the left sidebar:

### 1. Onboarding (First Visit)
- Plain-language explanation of LocalWEB
- Shows your Node ID
- Next steps checklist

### 2. Dashboard
- **Node Identity** — Node ID, Public Key, Uptime
- **Connected Peers** — Real-time count
- **Services Health** — All 9 services with green/red status
- **Real-time updates** via SSE

### 3. Network / Peers
- **Topology Visualization** — SVG graph showing peer connections
  - Center: Your node (blue)
  - Connected peers: Green dots with labels
  - Lines: Active connections with RTT
- **Peer List** — Table with Name, Source, Latency, Score
- **Real-time updates** — New peers appear instantly

### 4. Files
- Document list from shared files
- Transfer progress (when active)
- Peer browsing (future)

### 5. DNS
- `.localweb` records browser
- Record type, value, verification status
- Search/filter

### 6. HTTP Gateway
- Per-site routing status
- Active sites list
- Health checks

### 7. Email
- Inbox with sender, subject, timestamp
- Compose window (future)
- PoW antispam status

### 8. Messaging
- Channel list with message preview
- Send message form
- Real-time message updates via SSE

### 9. Docs
- Collaborative document list
- Real-time editing (RGA-based)
- Presence indicators, cursors

### 10. Registry
- Package browser (name, version, author)
- Install button (verifies signature)
- Search by name, author, platform

### 11. Voice
- Call state: Idle → Calling → Connected → Ended
- Start/End call buttons
- Mute/unmute toggle
- ICE connection status

### 12. VPN
- Connect/Disconnect toggle
- Active routes table (CIDR, Peer, Status)
- Connection state indicator
- Requires root/sudo

### 13. Security
- **Node Identity** — Node ID, Public Key (copy button)
- **Audit Log Integrity** — Live tamper-chain verification
  - Green: ✅ Chain verified
  - Red: ⚠️ TAMPER DETECTED
- **Audit Log Table** — Type, Peer, Timestamp

### 14. Settings
- **Basic/Advanced** toggle
- **Basic**: Ports, paths (read-only)
- **Advanced**: All config options
- **Theme**: Dark/Light toggle (persists)

---

## Navigation

| Key | Action |
|-----|--------|
| Click sidebar item | Navigate |
| `Tab` | Focus next element |
| `Enter`/`Space` | Activate button |
| `#route` in URL | Direct link (e.g. `#security`) |

---

## Real-Time Updates

The GUI uses **Server-Sent Events (SSE)** at `/api/events`:

| Event Type | Trigger |
|------------|---------|
| `peer_connected` | New peer discovered |
| `peer_disconnected` | Peer lost |
| `service_status` | Service health change |
| `transfer_progress` | File transfer update |

---

## Themes & Accessibility

| Feature | Support |
|---------|---------|
| Dark/Light theme | Toggle in Settings (persists) |
| Reduced motion | Respects `prefers-reduced-motion` |
| Keyboard focus | Visible on all interactive elements |
| Screen reader | Semantic HTML, ARIA labels |

---

## API Endpoints (Used by GUI)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | SPA shell |
| `/static/app.js` | GET | JavaScript |
| `/static/styles.css` | GET | CSS |
| `/api/status` | GET | Node status |
| `/api/peers` | GET | Peer list |
| `/api/dht/table` | GET | DHT nodes |
| `/api/services/health` | GET | All service statuses |
| `/api/audit-log` | GET | Audit log entries |
| `/api/audit-log/verify` | GET | Tamper-chain verification |
| `/api/dns/records` | GET | DNS records |
| `/api/http/sites` | GET | HTTP sites |
| `/api/email/messages` | GET | Email inbox |
| `/api/messaging/messages` | GET | Messages |
| `/api/docs/documents` | GET | Documents |
| `/api/registry/packages` | GET | Package list |
| `/api/events` | GET | SSE stream |
| `/healthz` | GET | Liveness |
| `/readyz` | GET | Readiness |

---

## Troubleshooting

| Issue | Solution |
|-------|----------|
| Blank page | Hard refresh (Ctrl+F5), check console for JS errors |
| "Can't connect" | Ensure node is running with `--dashboard` |
| No peers showing | Check node logs, ensure discovery running |
| Theme not persisting | Check localStorage enabled |

---

*LocalWEB Web GUI v1.0.0 | Read-only, localhost-only, read-only by design*
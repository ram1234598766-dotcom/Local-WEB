# LocalWEB REST API Reference

**Base URL:** `http://localhost:8080/api`

All endpoints return JSON. WebSocket/SSE endpoints documented separately.

**Author: Mrityunjay K**

---

## Node Status

### `GET /api/status`

Returns node identity and status.

**Response:**
```json
{
  "node_id": "a1b2c3d4",
  "public_key": "0123456789abcdef...",
  "started_at": "2025-09-05T12:00:00Z",
  "uptime": "1h30m45s",
  "peer_count": 5,
  "store_path": "/home/user/.localweb/data"
}
```

---

## Peers

### `GET /api/peers`

List all discovered peers.

**Response:**
```json
[
  {
    "id": "a1b2c3d4",
    "name": "laptop-john",
    "addrs": ["192.168.1.50:4443", "[fe80::1]:4443"],
    "score": 0.95,
    "latency": "2ms",
    "source": "mDNS",
    "last_seen": "2025-09-05T12:30:45Z",
    "version": "1.0.0"
  }
]
```

---

## DHT

### `GET /api/dht/table`

Get DHT routing table.

**Response:**
```json
{
  "nodes": [
    {
      "id": "a1b2c3d4",
      "name": "laptop-john",
      "addrs": "[192.168.1.50:4443]",
      "last_seen": "2025-09-05T12:30:45Z"
    }
  ],
  "count": 5
}
```

---

## DNS

### `GET /api/dns/records`

List `.localweb` DNS records.

**Response:**
```json
[
  {
    "name": "laptop-john.localweb",
    "type": "A",
    "value": "192.168.1.50",
    "ttl": 4500,
    "verified": true
  }
]
```

---

## HTTP Gateway

### `GET /api/http/sites`

List HTTP gateway sites.

**Response:**
```json
[
  {
    "name": "localhost:8080",
    "status": "active",
    "routes": 1
  },
  {
    "name": "gui.localweb",
    "status": "active",
    "routes": 2
  }
]
```

---

## Email

### `GET /api/email/messages`

List inbox messages.

**Response:**
```json
[
  {
    "from": "peer-a1b2c3d4",
    "subject": "Test email",
    "date": "2025-09-05T12:30:45Z",
    "read": true
  }
]
```

---

## Messaging

### `GET /api/messaging/messages`

List messages (recent).

**Response:**
```json
[
  {
    "channel": "general",
    "from": "peer-a1b2c3d4",
    "text": "Hello world!",
    "time": "2025-09-05T12:30:45Z"
  }
]
```

---

## Documents

### `GET /api/docs/documents`

List collaborative documents.

**Response:**
```json
[
  {
    "id": "doc-001",
    "name": "Project Notes",
    "peers": 2,
    "last_sync": "2025-09-05T12:30:45Z"
  }
]
```

---

## Registry

### `GET /api/registry/packages`

List available packages.

**Response:**
```json
[
  {
    "name": "localweb-cli",
    "version": "1.0.0",
    "author": "system",
    "installed": false
  }
]
```

---

## Audit Log

### `GET /api/audit-log`

Get audit log entries (latest 100).

**Response:**
```json
[
  {
    "type": "peer_connected",
    "timestamp": "2025-09-05T12:30:45.123456789Z",
    "peer_id": "e5f6g7h8",
    "source": "mDNS",
    "details": {}
  }
]
```

### `GET /api/audit-log/verify`

Verify audit chain integrity.

**Response:**
```json
{
  "verified": true,
  "timestamp": "2025-09-05T12:30:45Z",
  "integrity": "verified"
}
```

**If tampered:**
```json
{
  "verified": false,
  "timestamp": "2025-09-05T12:30:45Z",
  "integrity": "tampered"
}
```

---

## Sync Status

### `GET /api/crdt/sync-status`

Get CRDT sync status.

**Response:**
```json
{
  "documents": 3,
  "pending_ops": 0,
  "connected": true
}
```

---

## Services Health

### `GET /api/services/health`

Health status of all 9 services.

**Response:**
```json
{
  "services": {
    "dns": true,
    "http": true,
    "email": true,
    "messaging": true,
    "files": true,
    "docs": true,
    "registry": true,
    "voice": true,
    "vpn": true
  }
}
```

---

## Health Checks

### `GET /healthz`

Liveness probe.

**Response:**
```json
{ "status": "ok" }
```

### `GET /readyz`

Readiness probe.

**Response:**
```json
{ "status": "ready" }
```

---

## Error Responses

All errors follow this format:
```json
{
  "error": "description",
  "code": "ERROR_CODE",
  "details": {}
}
```

| HTTP Code | Meaning |
|-----------|---------|
| 200 | Success |
| 400 | Bad Request |
| 404 | Not Found |
| 500 | Internal Error |
| 503 | Service Unavailable |

---

## Authentication

Currently **no authentication** for local GUI (localhost-only).
Future: Capability token authentication.

---

## Rate Limiting

No rate limiting on localhost. Remote access would require auth.

---

*LocalWEB REST API v1.0.0 | Module: `github.com/ram1234598766-dotcom/Local-WEB`*
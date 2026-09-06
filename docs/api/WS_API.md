# LocalWEB WebSocket/SSE API Reference

The LocalWEB GUI uses **Server-Sent Events (SSE)** for real-time updates.

**Author: Mrityunjay K**

---

## Endpoint

```
GET /api/events
```

**Content-Type:** `text/event-stream`

---

## Connection

```javascript
const evtSource = new EventSource('/api/events');

evtSource.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Event:', event.type, data);
};

evtSource.onerror = (err) => {
  console.error('SSE error:', err);
  // Auto-reconnects by default
};
```

---

## Event Format

Each event follows the SSE format:

```
event: <event_type>
data: <JSON payload>

```

---

## Event Types

### `peer_connected`

New peer discovered.

```json
{
  "type": "peer_connected",
  "data": {
    "id": "e5f6g7h8",
    "name": "phone-jane",
    "source": "BLE",
    "addrs": ["[fe80::1]:4443"],
    "score": 0.72,
    "latency": "15ms",
    "timestamp": "2025-09-05T12:35:12Z"
  }
}
```

### `peer_disconnected`

Peer lost (TTL expired or explicit).

```json
{
  "type": "peer_disconnected",
  "data": {
    "id": "e5f6g7h8",
    "reason": "timeout",
    "timestamp": "2025-09-05T12:40:00Z"
  }
}
```

### `peer_updated`

Peer info changed (address, score, etc).

```json
{
  "type": "peer_updated",
  "data": {
    "id": "a1b2c3d4",
    "name": "laptop-john",
    "addrs": ["192.168.1.50:4443", "10.0.0.5:4443"],
    "score": 0.98,
    "latency": "1ms",
    "timestamp": "2025-09-05T12:35:12Z"
  }
}
```

### `service_status`

Service health changed.

```json
{
  "type": "service_status",
  "data": {
    "service": "vpn",
    "healthy": true,
    "timestamp": "2025-09-05T12:35:12Z"
  }
}
```

### `transfer_progress`

File transfer update.

```json
{
  "type": "transfer_progress",
  "data": {
    "file_id": "bafy...",
    "file_name": "photo.jpg",
    "peer_id": "e5f6g7h8",
    "bytes_sent": 5242880,
    "bytes_total": 10485760,
    "percentage": 50,
    "status": "sending"
  }
}
```

### `message_received`

New chat message.

```json
{
  "type": "message_received",
  "data": {
    "channel_id": "chan-001",
    "channel_name": "team",
    "from": "e5f6g7h8",
    "from_name": "jane",
    "text": "Hello team!",
    "timestamp": "2025-09-05T12:35:12Z"
  }
}
```

### `doc_updated`

Collaborative document changed.

```json
{
  "type": "doc_updated",
  "data": {
    "doc_id": "doc-001",
    "doc_name": "Project Notes",
    "peer_id": "e5f6g7h8",
    "peer_name": "jane",
    "operation": "insert",
    "position": 150,
    "text": "new paragraph",
    "timestamp": "2025-09-05T12:35:12Z"
  }
}
```

### `call_state`

Voice call state changed.

```json
{
  "type": "call_state",
  "data": {
    "call_id": "call-001",
    "peer_id": "e5f6g7h8",
    "state": "connected",
    "timestamp": "2025-09-05T12:35:12Z"
  }
}
```

### `vpn_state`

VPN connection state changed.

```json
{
  "type": "vpn_state",
  "data": {
    "peer_id": "a1b2c3d4",
    "state": "connected",
    "routes": ["10.0.0.0/24", "192.168.1.0/24"],
    "timestamp": "2025-09-05T12:35:12Z"
  }
}
```

---

## Connection Management

### Auto-Reconnect

The `EventSource` API auto-reconnects on disconnect.
For custom reconnection logic:

```javascript
let evtSource;

function connect() {
  evtSource = new EventSource('/api/events');
  
  evtSource.onerror = () => {
    evtSource.close();
    setTimeout(connect, 5000); // Reconnect after 5s
  };
}

connect();
```

### Heartbeat

The server sends a comment every 30s to keep connection alive:
```
: heartbeat

```

---

## Connection Lifecycle

```
Client                          Server
  |                                |
  |--- GET /api/events ----------->|
  |<--- 200 OK (text/event-stream)-|
  |                                |
  |<--- event: peer_connected -----|
  |<--- event: service_status -----|
  |                                |
  |         (network issues)       |
  |                                |
  |--- GET /api/events (reconnect)-|
  |<--- 200 OK --------------------|
  |<--- event: peer_connected -----|
  |                                |
```

---

## Error Handling

| Error | Handling |
|-------|----------|
| Connection lost | Auto-reconnect (browser) or manual (5s) |
| 503 Service Unavailable | Retry after 5s |
| Invalid JSON | Log error, continue |

---

## Browser Support

| Browser | SSE Support |
|---------|-------------|
| Chrome 6+ | ✅ |
| Firefox 6+ | ✅ |
| Safari 5+ | ✅ |
| Edge 12+ | ✅ |
| IE | ❌ (use polyfill) |

---

## Example: React Hook

```jsx
import { useEffect, useState } from 'react';

function useSSE() {
  const [events, setEvents] = useState([]);
  
  useEffect(() => {
    const evtSource = new EventSource('/api/events');
    
    evtSource.onmessage = (event) => {
      const data = JSON.parse(event.data);
      setEvents(prev => [...prev.slice(-99), { type: event.type, ...data }]);
    };
    
    return () => evtSource.close();
  }, []);
  
  return events;
}

// Usage
function Dashboard() {
  const events = useSSE();
  const peers = events.filter(e => e.type === 'peer_connected');
  
  return (
    <div>
      <h2>Connected Peers: {peers.length}</h2>
      <ul>
        {peers.map(p => (
          <li key={p.data.id}>{p.data.name} ({p.data.source})</li>
        ))}
      </ul>
    </div>
  );
}
```

---

## Debugging

### View Raw Events (curl)
```bash
curl -N http://localhost:8080/api/events
```

### Inspect in DevTools
1. Open DevTools → Network
2. Filter by "EventSource"
3. Click `/api/events` → "Messages" tab

---

## Testing

```bash
# Test connection
curl -N http://localhost:8080/api/events

# Test with timeout
timeout 10 curl -N http://localhost:8080/api/events
```

---

*LocalWEB SSE API v1.0.0 | Module: `github.com/ram1234598766-dotcom/Local-WEB`*
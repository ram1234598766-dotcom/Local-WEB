# LocalWEB — Tech Stack Decision

## Language: Go 1.23+

### Why Go (not Rust, not C++)

| Factor | Go | Rust | C++ |
|--------|-----|------|-----|
| Development speed | Fast | Slow | Very slow |
| Memory safety | GC | Ownership | Manual |
| QUIC ecosystem | quic-go (mature) | quinn (good) | boost::asio |
| mDNS/DNS | miekg/dns, zeroconf | trust-dns | Custom |
| Cross-compile | Trivial (GOOS/GOARCH) | Cross but harder | Painful |
| FUSE | go-fuse (mature) | fuse3 (C bindings) | libfuse |
| WireGuard | golang.zx2c4.com/wireguard | boringtun | libwg |
| Protobuf | google.golang.org/protobuf | prost | protobuf-cpp |
| Developer productivity | High | Medium | Low |
| Concurrency | Goroutines (easy) | Tokio (good) | Threads (hard) |
| Error handling | Explicit | Explicit | Exceptions |
| Binary size | ~15MB | ~5MB | Varies |
| Startup time | Fast | Fast | Fast |

**Decision: Go.** The ecosystem alignment is perfect. quic-go, miekg/dns, go-fuse, wireguard-go — all mature Go libraries. Development speed matters more than squeezing the last 10% of performance for a v1.0.

### When to Consider Rust
- If we hit GC pauses >10ms under load
- If crypto operations bottleneck
- If memory footprint is too high
- These are unlikely for v1.0 — profile first, rewrite later if needed.

---

## Core Dependencies

### Transport
| Package | Purpose | Status |
|---------|---------|--------|
| `github.com/quic-go/quic-go` | QUIC (RFC 9000) | Mature, production-ready |
| `golang.org/x/crypto` | Noise, X25519, ChaCha20 | Standard library |

### Discovery
| Package | Purpose | Status |
|---------|---------|--------|
| `github.com/grandcat/zeroconf` | mDNS-SD | Mature, cross-platform |
| `github.com/miekg/dns` | DNS parser/server | Gold standard in Go |

### Storage
| Package | Purpose | Status |
|---------|---------|--------|
| `github.com/dgraph-io/badger/v3` | Embedded KV store | Production-ready |
| `github.com/ipfs/go-cid` | Content identifiers | IPFS ecosystem |

### Crypto
| Package | Purpose | Status |
|---------|---------|--------|
| `golang.org/x/crypto` | Ed25519, X25519, HKDF | Standard |
| `github.com/rs/zerolog` | Structured logging | Fast, zero-alloc |

### Config / CLI
| Package | Purpose | Status |
|---------|---------|--------|
| `github.com/spf13/cobra` | CLI framework | Standard |
| `github.com/spf13/viper` | Config management | Standard |
| `google.golang.org/protobuf` | Serialization | Standard |

### Voice/Video
| Package | Purpose | Status |
|---------|---------|--------|
| `github.com/nicknack/opus` | Opus codec (CGo) | Wrapper around libopus |
| `github.com/nicknack/vpx` | VP9 codec | Wrapper around libvpx |

### VPN
| Package | Purpose | Status |
|---------|---------|--------|
| `golang.zx2c4.com/wireguard` | WireGuard userspace | Official Go impl |

### FUSE
| Package | Purpose | Status |
|---------|---------|--------|
| `github.com/hanwen/go-fuse` | FUSE filesystem | Mature, cross-platform |

### Compression
| Package | Purpose | Status |
|---------|---------|--------|
| `github.com/klauspost/compress` | Zstd, gzip, snappy | Fastest Go compression |

### Time Sync
| Package | Purpose | Status |
|---------|---------|--------|
| `github.com/beevik/ntp` | NTP client | Simple, reliable |

---

## Serialization: Protobuf

### Why Protobuf (not JSON, not msgpack)
- Schema evolution (backward compatible)
- Compact binary encoding
- Code generation (Go, future clients)
- Strong typing
- Used by gRPC ecosystem

### Proto Files
```protobuf
syntax = "proto3";
package localweb;

message PeerInfo {
  bytes id = 1;
  string addr = 2;
  repeated string services = 3;
  bytes public_key = 4;
  int64 last_seen = 5;
}

message Message {
  string id = 1;
  string channel_id = 2;
  bytes sender = 3;
  int64 timestamp = 4;
  bytes payload = 5;
  bytes signature = 6;
  string parent_id = 7;
}

message DNSRecord {
  string name = 1;
  uint32 type = 2;
  uint32 ttl = 3;
  bytes data = 4;
  uint32 priority = 5;
}

message FileBlock {
  string cid = 1;
  bytes data = 2;
  int64 size = 3;
  string mime = 4;
  repeated string parents = 5;
}
```

---

## Build Targets

```bash
# Native build
go build -o bin/localweb-node ./cmd/node

# Cross-compile
GOOS=linux   GOARCH=amd64 go build -o bin/localweb-linux-amd64 ./cmd/node
GOOS=linux   GOARCH=arm64 go build -o bin/localweb-linux-arm64 ./cmd/node
GOOS=darwin  GOARCH=arm64 go build -o bin/localweb-macos-arm64 ./cmd/node
GOOS=darwin  GOARCH=amd64 go build -o bin/localweb-macos-amd64 ./cmd/node
GOOS=windows GOARCH=amd64 go build -o bin/localweb-windows-amd64.exe ./cmd/node
```

---

*Last updated: 2026-09-04*

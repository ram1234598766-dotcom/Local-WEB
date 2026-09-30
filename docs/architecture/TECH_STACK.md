# LocalWEB — Technology Stack

**Grounded in `go.mod`, `go list -deps ./...`, the `Makefile`, `.golangci.yml`
and `.github/workflows/ci.yml` at commit `df30121` + the correctness pass.**
**Module: `github.com/ram1234598766-dotcom/Local-WEB` | Go 1.26**

> Tag legend, as in `ARCHITECTURE.md`: ✅ verified by a test · ⚠️ partial ·
> ❌ not implemented. Every dependency below was confirmed present in `go.mod`
> **and** imported by the code; every dependency the previous revision listed as
> "✅ govulncheck" but which is absent is called out in §2.

---

## 1. Module and build

| Aspect | Value |
|---|---|
| Module path | `github.com/ram1234598766-dotcom/Local-WEB` (matches the repo URL) |
| Language | Go 1.26.0 |
| Build system | GNU Make. Bazel is **not** configured. |
| Code generation | `go generate ./...`. No `protoc` or `mockgen` in the build. |
| Dependency management | `go mod`. No vendoring. |
| Reproducible builds | `go build -trimpath -o …` |

### 1.1 Makefile targets

Every target below exists and runs. `make help` prints this list.

| Target | Purpose |
|---|---|
| `build` | `build-node` + `build-cli` into `bin/` |
| `build-node` | `bin/localweb-node` from `./cmd/node` |
| `build-cli` | `bin/localweb-cli` from `./cmd/cli` |
| `test` / `test-unit` | Unit tests, no race detector, integration excluded |
| `test-race` | Unit tests under `-race` |
| `test-integration` | `-tags=integration ./test/integration/…` |
| `test-chaos` | `pkg/chaos` under `-race` |
| `test-cover` | Writes `coverage.out`, prints the total |
| `cover-func` | `go tool cover -func` over an existing profile |
| `lint` | `fmt-check` + `vet` + `staticcheck` |
| `security` | `govulncheck ./...` |
| `cross-compile` | 5 release targets + matching CLI binaries |
| `bench` | Benchmarks for crdt, dht, crypto, chaos, store, security |
| `run-node`, `run-cli` | Run from source |
| `quickstart` | `scripts/quickstart.sh` |
| `ci` | The same gate CI runs |
| `clean` | Removes `bin/` and `coverage.out` |

Corrections to the previous revision:

- `bench` was defined **twice**; the second silently overrode the first and
  dropped every benchmark outside `pkg/{crdt,dht,crypto,chaos}`. Now defined
  once, with the package list explicit.
- `desktop-build`, `desktop-build-all` and `run-desktop` referenced
  `./cmd/desktop`, which **does not exist**. `wails.json` is present but there is
  no Wails Go entry point, so those targets could never run. Removed.
- The binary name mismatch is fixed: `build` produces `bin/localweb-node` and
  `bin/localweb-cli`, matching `README.md` and `scripts/quickstart.sh`.
  Previously the Makefile produced `bin/localweb` for the CLI, which nothing
  referenced.
- `fmt` used `go fmt`, which rewrites files and never fails. Replaced by
  `fmt-check` (`gofmt -s -l`, non-zero exit) so a formatting failure is a gate.

### 1.2 Targets that do not exist

Deliberately absent, because nothing implements them: `release`, `sbom`,
`attest`, `pre-commit`, `test-mutate`, `dev`, `dev-build`, `deps-update`.
`staticcheck` and `govulncheck` are wired as targets but **skip with a clear
message** when the binary is absent, rather than failing a build on a missing
dev tool.

### 1.3 Packaging

| File | State |
|---|---|
| `Dockerfile` | Present, 2-stage Alpine, correct `-ldflags` version injection. **Not built by CI.** Its `CMD ["node", "--data-dir", …]` previously had the flag silently swallowed; the daemon now strips the leading verb. |
| `nfpm.yaml` | Present but **cannot build as written**: it reads `dist/localweb-linux-amd64`, while `cross-compile` writes to `bin/`. Tracked in FINDINGS. |
| `.goreleaser.yml` | Present and valid for 5 targets. **Never invoked** — `release-dry` is the only target that touches goreleaser. Its macOS `certificate:` is an unfilled placeholder (`<Team ID>`) that will fail signing. |
| `systemd/*.service` | Fixed this pass: `Type=simple` (the daemon has no sd_notify), `wintun` dependency removed from a Linux unit, six duplicated hardening keys collapsed, `ExecStart` no longer hides `--data-dir`. |
| `installers/` | 57 files; the most complete part of the repo. Contains 26.3 MB of Linux `.deb`/`.rpm`/`.apk` packages committed inside the **Windows** directory, plus a 0-byte `.deb` tracked inside `pkg/`. |

---

## 2. Dependencies

### 2.1 Direct dependencies actually in use

| Dependency | Version | Used for | Layer |
|---|---|---|---|
| `github.com/quic-go/quic-go` | v0.62.0 | QUIC v1 transport | L1 |
| `github.com/dgraph-io/badger/v3` | v3.2103.5 | LSM-tree KV store with AEAD | L6 |
| `github.com/ipfs/go-cid` | v0.4.0 | CIDv1 content addressing | L6 |
| `github.com/multiformats/go-multihash` | v0.0.15 | SHA2-256 multihash for CIDs | L6 |
| `github.com/klauspost/compress` | v1.18.3 | zstd (present, not wired to a code path) | L6/L8 |
| `github.com/rs/zerolog` | v1.33.0 | Structured JSON logging | All |
| `github.com/spf13/cobra` + `pflag` | v1.8.1 | CLI framework | L9 |
| `github.com/skip2/go-qrcode` | v0.0.0-20200617195104 | QR pairing payload | L9 |
| `github.com/cloudflare/circl` | v1.6.5 | Kyber-1024, Ed448, Dilithium3, ML-DSA-65 | L4 |
| `golang.org/x/crypto` | v0.54.0 | X25519, HKDF, SHA3-256, Argon2id, XSalsa20-Poly1305 | L4 |
| `golang.org/x/sys` | v0.47.0 | Windows/Unix syscalls for the VPN TUN | L8 |
| `google.golang.org/protobuf` | v1.36.12 | Protobuf codec | All |
| `go.yaml.in/yaml/v3` | v3.0.5 | LWPKG package manifests | L8 |
| `github.com/stretchr/testify` | v1.12.1 | Test assertions | Test |

### 2.2 Documented but absent

The previous revision listed the following nine as "✅ govulncheck". **None is
in `go.mod` or `go.sum`, and none is imported anywhere:**

| Claimed dependency | Claimed use | Actual state |
|---|---|---|
| `github.com/pion/webrtc/v3` | Voice service (ICE/DTLS/SRTP) | Absent. `voice/track.go:17` `CodecOpus`/`CodecVP9` are metadata constants; `handleStream` passes payloads through raw |
| `github.com/pion/ice/v2` | ICE | Absent. No ICE anywhere |
| `github.com/pion/dtls/v2` | DTLS | Absent |
| `github.com/pion/srtp/v2` | SRTP | Absent |
| `github.com/cilium/ebpf` | eBPF QoS/TC | Absent. No `pkg/ebpf/` |
| `github.com/vishvananda/netlink` | TUN/routing | Absent |
| `github.com/mdlayher/wifi` | WiFi Direct | Absent. `wifi_direct.go` shells out to `wpa_cli`/`iw` |
| `github.com/gen2brain/beeep` | Desktop notifications | Absent |
| `github.com/wailsapp/wails/v2` | Desktop app (Phase 8) | Absent. `wails.json` exists with no Go entry point |

`golang.org/x/sync` was also listed; it is in neither `go.mod` nor `go.sum`.

**Consequence:** Voice has no codec and no transport security of its own; QoS
has no eBPF acceleration; the VPN has no netlink-based routing; WiFi Direct
depends on host tooling. These are recorded as gaps in `ARCHITECTURE.md` §1.1.

### 2.3 Cryptography in use

| Purpose | Algorithm | Implementation | Verified |
|---|---|---|---|
| Node identity | Ed25519 | `x/crypto/ed25519` | ✅ |
| Key exchange | X25519 | `x/crypto/curve25519` | ✅ |
| Post-quantum KEM | Kyber-1024 | `circl/kem/kyber1024` | ✅ both sides agree on the secret |
| Hashing | SHA3-256 | `x/crypto/sha3` | ✅ |
| KDF | HKDF-SHA3-256 | `x/crypto/hkdf` | ✅ |
| AEAD | XSalsa20-Poly1305 | `x/crypto/nacl/secretbox` | ✅ |
| Password hashing | Argon2id | `x/crypto/argon2` | ✅ PoW seed derivation |
| Store at rest | AES-256-GCM | Badger's built-in key registry | ✅ key from the identity seed, no hardcoded fallback |
| Alternative signatures | Ed448, Dilithium3, ML-DSA-65 | `circl/sign/…` | ⚠️ implemented, zero call sites |

### 2.4 Proof of work — two schemes

| | `pkg/security` | `pkg/dht` |
|---|---|---|
| Purpose | service-level gating (Email, Docs) | DHT registration anti-Sybil |
| Memory-hard pass | Argon2id 64 MiB, once per challenge | none |
| Per-nonce work | SHA3-256 over `seed‖nonce` | SHA3-256 over `pubKey‖name‖nonce` |
| Difficulty unit | leading zero bits, `[8,24]` | leading zero bits, `[8,24]` |
| Replay window | 5 min, bounded 1024-entry cache | none (registration is idempotent) |
| Enforcement | `PoWValidator.Validate` | `server.go` `MsgRegisterNode` |
| Verified by | 24 tests in `security/pow_test.go` | `TestHandleRegisterNodeRejectsInvalidProofOfWork` |

The rationale for the split is in `ARCHITECTURE.md` §3.

---

## 3. Testing and quality

### 3.1 Measured coverage

`make test-cover` → **63.7%** of statements across the unit suite.

| Package | Coverage | Package | Coverage |
|---|---|---|---|
| `pkg/security` | 89.6% | `pkg/services/vpn` | 78.0% |
| `pkg/discovery` | 86.0% | `pkg/chaos` | 76.7% |
| `pkg/plugin` | 86.1% | `pkg/store` | 76.6% |
| `pkg/qos` | 86.0% | `pkg/crdt` | 75.6% |
| `pkg/services/registry` | 83.4% | `pkg/link` | 64.6% |
| `pkg/services/docs` | 82.1% | `pkg/services/email` | 65.3% |
| `pkg/services/voice` | 77.0% | `pkg/services/http` | 64.1% |
| | | `pkg/federation` | 56.6% |
| | | `pkg/services/files` | 55.1% |
| | | `pkg/proto` | 54.3% |
| | | `pkg/services/dns` | 52.9% |
| | | `pkg/nat` | 48.9% |
| | | `cmd/cli` | 47.3% |
| | | `pkg/dht` | 46.6% |
| | | `pkg/crypto`, `pkg/transport` | 40.7% |
| | | `pkg/gui` | 36.2% |
| | | `pkg/services/messaging` | 26.3% |
| | | `cmd/node` | 3.0% |

**The previous revision claimed ≥90% unit / ≥80% integration.** Measured
before this pass: **25.9%** total, and `pkg/link`, `pkg/discovery` and
`pkg/plugin` had **no test files at all** (1,460 statements at 0%). Those three
now have 103, 79 and 58 tests respectively. The 90% target is still not met and
is not claimed.

Lowest-covered code is concentrated where it should be flagged: `cmd/node`
(3.0%) is a thin startup wiring, but `pkg/services/messaging` (26.3%) has real
logic — an in-memory store, no listener, signature creation with no
verification — and is the weakest service.

### 3.2 Test inventory

| Suite | Count | Command |
|---|---|---|
| Unit | 783 top-level | `make test-unit` |
| Integration | 71 top-level | `make test-integration` |
| Chaos | included above | `make test-chaos` |

Integration tests carry `//go:build integration`, so they never run implicitly.
Before this pass, `test/integration/setup.go` held 16 `Test*` functions in a
**non-test file** and was therefore never compiled into the test binary; those
16 now run.

### 3.3 Test matrix — what exists vs. what was claimed

| Category | Claimed | Actual |
|---|---|---|
| Unit (`pkg/*/*_test.go`) | `testing` + `testify`, >90% | ✅ present; **63.7% measured** |
| Integration (`test/integration/`) | `testcontainers`, >80% | ✅ present (no testcontainers; uses `net.Pipe` and loopback) |
| Chaos | 6 scenarios | ✅ **6** scenarios (the previous doc said 12 in one place and 6 in another) |
| Fuzzing | 24 h/week, `pkg/*/fuzz_test.go` | ❌ no fuzz target in the tree |
| Property tests | `gopter`/`rapid`, `pkg/*/prop_test.go` | ❌ none |
| Contract tests | `test/contract/` | ❌ directory does not exist |
| Load tests | `test/load/`, 10k concurrent | ❌ directory does not exist |
| Mutation | `make test-mutate`, >80% kill rate | ❌ neither target nor tool |
| Benchmarks | `pkg/*/bench_test.go` | ✅ 3 files: `crdt`, `crypto`, `dht` |

### 3.4 Linting and CI

`.golangci.yml` enables exactly two linters — `misspell` and `ineffassign`.
The previous revision claimed "golangci-lint, vet, fmt, staticcheck, gosec".
Current reality:

| Check | Where it runs | State |
|---|---|---|
| `golangci-lint` (2 linters) | `make lint`, `.github/workflows/ci.yml` | ✅ 0 issues |
| `go vet ./...` | `make lint`, CI | ✅ exit 0 |
| `gofmt -s -l` | `make lint`, CI | ✅ clean |
| `staticcheck` | `make lint` | ⚠️ target skips with a message if not installed; not in CI |
| `gosec` | — | ❌ not configured anywhere |
| `govulncheck` | `make security`, CI | ⚠️ CI-only action; not a local target by default |
| `misspell`, `ineffassign` | `.golangci.yml` | ✅ |
| Coverage threshold | — | ❌ CI sets **no** threshold; codecov runs with `fail_ci_if_error: false` |
| `trivy`, `syft`, `cosign`, `goreleaser` in CI | claimed | ❌ none present |

CI (`.github/workflows/ci.yml`) runs: gofmt, vet, `go mod tidy` diff check,
golangci-lint, unit tests with `-race`, integration tests with
`-tags=integration`, govulncheck, chaos on a nightly cron, and cross-compile for
5 targets. There is **no release workflow** — `f5efa07` removed the package jobs
and nothing replaced them, so `.goreleaser.yml` is orphaned and releases are
built by hand.

---

## 4. Runtime and configuration

### 4.1 Platforms

| OS | Arch | Built | Notes |
|---|---|---|---|
| Linux | amd64 | ✅ | `make cross-compile`, CI |
| Linux | arm64 | ✅ | `make cross-compile`, CI |
| darwin | amd64 | ✅ | `make cross-compile`, CI |
| darwin | arm64 | ✅ | `make cross-compile`, CI |
| Windows | amd64 | ✅ | `make cross-compile`, CI |
| Linux | riscv64 | ❌ | previous doc claimed ✅; nothing builds or tests it |
| FreeBSD / OpenBSD | — | ❌ | previous doc claimed "in progress"/"planned"; absent from all build config |

Cross-compilation uses `CGO_ENABLED` default (1). CI's Linux/arm64 artifact is
built with `CGO_ENABLED=0` while the released one uses `CGO_ENABLED=1`, so the
two are not the same binary.

### 4.2 Entry points

| Binary | Source | Purpose |
|---|---|---|
| `localweb-node` | `cmd/node/main.go` | The daemon |
| `localweb-cli` | `cmd/cli/main.go` | CLI client |

`cmd/gui` **does not exist**. The previous revision listed a third binary,
`localweb-gui`, built from `cmd/gui/main.go`. The GUI is a server-side component
of the daemon (`pkg/gui`, embedded via `go:embed`), not a separate process.

`localweb-cli node` is a stub: `cmd/cli/main.go:194` `startNode` prints
"Node started successfully" and returns without starting anything.

### 4.3 Configuration

**There is no configuration file loader.** The daemon is configured entirely by
flags (`cmd/node/main.go`):

| Flag | Default | Purpose |
|---|---|---|
| `-addr` | `0.0.0.0:4443` | QUIC listen address |
| `-name` | hostname | Node name |
| `-storage` | `$dataDir/storage` | BadgerDB directory |
| `-data-dir` | `~/.localweb` | Identity and keys |
| `-rendezvous` | *(empty)* | Rendezvous server URL |
| `-rendezvous-register` | `true` | Register with the rendezvous server |
| `-rendezvous-poll` | `60s` | Poll interval |
| `-hybrid` | `false` | Enable the post-quantum handshake |

The previous revision documented a rich JSON schema (`$schema`, `node{}`,
`transport{}`, `qos{}`, `plugins{}` …). `config/config.json` exists in the repo
but **no Go code reads it**, and the file `cli init` actually writes is a flat
JSON object with a completely different shape. There is no YAML or TOML config
path.

A leading non-flag verb (`localweb node --data-dir …`) is accepted and stripped,
because the service unit, Dockerfile and NSIS shortcuts all use that form and
`flag.Parse` would otherwise discard every flag after it. Unknown positional
arguments now fail loudly rather than being ignored.

---

## 5. Observability — what exists

| Endpoint | State | Evidence |
|---|---|---|
| `/healthz` | ✅ | `pkg/gui/handler.go:232` |
| `/readyz` | ⚠️ | ready as soon as the process starts; checks only that a NodeID is set |
| `/api/events` (SSE) | ⚠️ | real SSE, but `BroadcastEvent` has no caller, so nothing is ever emitted |
| `/api/status`, `/api/peers`, `/api/audit-log` | ✅ | backed by real state |
| `/api/audit-log/verify` | ✅ | real hash-chain re-verification |
| `/api/services/health` | ✅ | fixed this pass; previously returned all-9 `true` while none ran |
| `/metrics` (Prometheus) | ❌ | not registered |
| `/debug/pprof` | ❌ | not imported, not registered |
| OTLP tracing | ❌ | no OpenTelemetry dependency |

The previous revision listed **27 named Prometheus metrics**
(`localweb_node_uptime_seconds`, `localweb_transport_*`, `localweb_dht_*`, …).
**Zero of them are registered anywhere in the codebase.** A hardcoded metric
string exists inside an example plugin (`pkg/plugin/builtin.go:182-184`) but that
plugin is never instantiated.

Endpoints the shipped SPA calls that the handler does not register:
`/api/files/list`, `/api/files/transfers`, `/api/registry/installed`,
`/api/docs/create`, `/api/docs/save/{id}`, `/api/docs/autosave/{id}`.

The SPA's `connectWS()` (`pkg/gui/static/app.js:163`) opens a **WebSocket**
against `/api/events`, which the server only serves as **SSE** over
`http.ServeMux`. There is no upgrade handler, so the socket never connects and
retries every 2 s.

---

## 6. Data layer

| Feature | State |
|---|---|
| BadgerDB LSM store | ✅ real, not an in-memory map |
| AES-256-GCM at rest | ✅ via Badger's key registry; key = SHA3-256 of the identity seed |
| Hardcoded key fallback | ✅ **none exists** — grep for `EncryptionKey` returns one hit |
| CIDv1 / raw / SHA2-256 | ✅ re-hashed and checked on both write and read |
| TTL, atomic batch | ✅ |
| Namespaces | ⚠️ `LWS:block:`, `LWS:meta:`, `LWS:peer:` — not the 6 documented prefixes |
| Peer value encoding | ⚠️ `encoding/gob`, not protobuf; the generated protobuf types are unused |
| Block compression | ❌ `compressBlock` has only a test caller |
| `Backup()` API | ❌ not implemented |
| Prometheus store metrics | ❌ not exported |

---

## 7. Services

Nine service packages exist as tested libraries. **The daemon starts none of
them** — see `ARCHITECTURE.md` §1. Service IDs on the wire are ASCII letters
(`'C','D','H','M','F','R','V','W','O','G'`), not the documented `0x00`–`0x09`.

| Service | Listens | Note |
|---|---|---|
| DNS | UDP :5353 | real wire codec; zone signing now works (`SignZone`) |
| HTTP | TCP :8080 | routes by path prefix, not Host header; `/health` always 200 |
| Email | TCP :587/:993 | real SMTP/IMAP; PoW enforced only when an `X-PoW` header is present, so it is bypassable by omission |
| Messaging | **no listener** | in-memory store; signatures created but never verified |
| Files | QUIC only | `Sync()` never contacts the peer; `GetFile` always returns a nil data slice |
| Docs | QUIC only | `Merge` replays remote ops positionally — concurrent editors can diverge |
| Registry | HTTP `cfg.Addr` | real publish + signature verify; `NewHTTPServer` is not called by the daemon; DHT `ResolveMeta` always returns not-found |
| Voice | QUIC only | **no codec at all**; raw payload passthrough |
| VPN | TUN on Linux | no packet-forwarding loop; no Windows path beyond a stub that logs and continues |

---

*LocalWEB Technology Stack — every dependency, target and endpoint verified
against `go.mod`, `make`, and the CI configuration.*
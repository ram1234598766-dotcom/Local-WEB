# LocalWEB — Technology Stack

**Grounded in `go.mod`, `go list -deps ./...`, the `Makefile`,
`.golangci.yml` and `.github/workflows/ci.yml` as they stand on `main`, plus a
fresh `git clone` of `main` used to build both Windows installers from an empty
tree.**
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
| `deps-wintun` | Downloads Wintun 0.14.1 from wintun.net and verifies its SHA-256; idempotent |
| `msi` | Stages the payload and runs WiX `candle` + `light` → `dist/localweb_1.0.1_x64_en-US.msi` |
| `nsis` | Stages the payload and runs `makensis` → `dist/localweb-1.0.1-setup.exe` |
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
| `nfpm.yaml` | **Valid YAML and correct paths.** It was previously unusable twice over: `version` and `contents` were indented so the top-level mapping could not start, and it read `dist/` while `cross-compile` writes to `bin/`. Both fixed; every `src` path resolves. |
| `.goreleaser.yml` | Present and valid for 5 targets. **Never invoked** — `release-dry` is the only target that touches goreleaser. Its macOS `certificate:` is an unfilled placeholder (`<Team ID>`) that will fail signing. |
| `systemd/*.service` | Fixed: `Type=simple` (the daemon has no sd_notify), `wintun` dependency removed from a Linux unit, six duplicated hardening keys collapsed, `ExecStart` no longer hides `--data-dir`. |
| `installers/` | 34 tracked files, **no binaries**. It previously held 26.3 MB of Linux `.deb`/`.rpm`/`.apk` inside the Windows directory, plus a 0-byte `.deb` tracked inside `pkg/`; all removed. |
| `installers/windows/*.ps1` | Six scripts, one copy. `installers/windows/scripts/` held a duplicate set that drifted by 30 lines, which is how `install.ps1` kept a PowerShell 7-only `??` operator — a parse error under the Windows PowerShell 5.1 the installer invokes — while its twin had been fixed. |
| `installers/windows/{README,LICENSE,CHANGELOG}.md` | Removed. Byte-identical duplicates of the root files; both build scripts now read the canonical root copies, which ships them into the installer. |

**Reproducible Windows installers.** `wintun.dll` is a `*.dll`, so `.gitignore`
excluded it, and both installers bundle it — a fresh clone could build neither.
`make deps-wintun` now fetches it. The SHA-256 is checked on the archive
(`07c25618…`, 750,540 bytes) *and* on the extracted amd64 driver
(`e5da8447…`, 427,552 bytes), and the amd64 build is selected by size rather than
path because the archive layout is not contractual. A mismatch is a hard failure
that writes nothing, because this file is installed as a kernel driver.

Verified by cloning `main` into an empty directory with no `wintun.dll`, no
`bin/` and no `dist/`: `make msi` and `make nsis` both succeed, the NSIS payload
carries all six scripts plus `wintun.dll`, `wintun.dll.sig`, `localweb.exe`,
`localweb-cli.exe` and `config/config.json`, and the MSI reports
`ProductVersion 1.0.1`.

Toolchain notes: `scripts/windows-toolchain.sh` locates `candle`, `light` and
`makensis` under either Git Bash or WSL, translates arguments only when the tool
is a native Windows binary, and stages inside the repository because `/tmp` is
invisible to a Windows executable under WSL.

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

`make test-cover` → **64.4%** of statements across the unit suite
(6,097/9,495 statements over 27 packages). Re-measured after the GUI HTTP fixes
recorded in `ARCHITECTURE.md` §9.1 items 27–28.

| Package | Coverage | Package | Coverage |
|---|---|---|---|
| `pkg/security` | 89.6% | `pkg/services/vpn` | 78.0% |
| `pkg/plugin` | 86.1% | `pkg/chaos` | 76.7% |
| `pkg/qos` | 86.0% | `pkg/store` | 76.6% |
| `pkg/discovery` | 86.0% | `pkg/crdt` | 75.6% |
| `pkg/services/registry` | 83.4% | `pkg/services/email` | 65.3% |
| `pkg/services/docs` | 82.1% | `pkg/link` | 64.2% |
| `pkg/services/voice` | 77.0% | `pkg/services/http` | 64.1% |
| | | `pkg/federation` | 56.6% |
| | | `pkg/services/files` | 55.1% |
| | | `pkg/proto` | 54.3% |
| | | `pkg/services/dns` | 52.9% |
| | | `pkg/nat` | 48.9% |
| | | `cmd/cli` | 47.3% |
| | | `pkg/dht` | 46.6% |
| | | `pkg/gui` | 41.5% |
| | | `pkg/transport` | 45.6% |
| | | `pkg/crypto` | 40.7% |
| | | `pkg/services/messaging` | 26.3% |
| | | `cmd/node` | 3.0% |
| | | `internal/version` | 0.0% (2 statements, no test file) |

**The previous revision claimed ≥90% unit / ≥80% integration.** Measured
before the correctness passes: **25.9%** total, and `pkg/link`, `pkg/discovery`
and `pkg/plugin` had **no test files at all** (1,460 statements at 0%). Those three
now have 103, 79 and 58 tests respectively. The 90% target is still not met and
is not claimed.

Lowest-covered code is concentrated where it should be flagged: `cmd/node`
(3.0%) is a thin startup wiring, but `pkg/services/messaging` (26.3%) has real
logic — an in-memory store, no listener, signature creation with no
verification — and is the weakest service.

### 3.2 Test inventory

Integration tests carry `//go:build integration`, so they never run implicitly.
Before the correctness pass, `test/integration/setup.go` held 17 `Test*`
functions in a **non-test file** and was therefore never compiled into the test
binary; those 17 now run.

Counts are enumerated with `go test -list`, not parsed from `-v` output:

| Suite | Count | Command |
|---|---|---|
| Unit | 796 | `make test-unit` |
| Integration | 71 | `make test-integration` |
| Chaos | included above | `make test-chaos` |

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

`localweb-cli node` **used to** be a stub: `startNode` printed "Node started
successfully" and returned without starting anything, discarding the private key
as `_ = priv`. It now resolves the real daemon binary next to the CLI, execs it
with the same flags, and when that binary is absent it says plainly that this
command does not start a daemon and prints the exact command to run instead.
Verified by running it: with `localweb-node` present it execs it and the daemon
comes up on both QUIC and the GUI API.

`localweb-node` gained a `-tls-verify` flag. It is **off by default and cannot
simply be switched on**: the listener presents a self-signed certificate with no
CA and no pinning, so a verifying client rejects every peer and the mesh stops
connecting. `TestClientCannotVerifySelfSignedServer` demonstrates exactly that,
and `TestSelfSignedDialSucceedsWhenVerificationDisabled` shows the insecure
default is load-bearing. Peer identity is authenticated by Noise XX beneath TLS,
which is the control that prevents impersonation. Closing Phase 8 item 8.6
requires certificate pinning keyed to the node identity.

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
| `/api/events` (SSE) | ⚠️ | real SSE, now with the 30s `: heartbeat` comment `WS_API.md` documents (`TestEventsHandlerEmitsHeartbeat`). `BroadcastEvent` still has **no caller**, so no event is ever emitted |
| `/api/audit-log/verify` | ✅ | real hash-chain re-verification. **Fixed:** it returned `500` for a tampered chain and omitted the documented `integrity` field, because `AuditLogVerified()` conflated "no audit log" with "tampered". Now `200` + `integrity` for a broken chain and `500` only when there is no chain (`TestAuditVerifyHandlerTamperedReturns200`) |
| `/api/status`, `/api/peers`, `/api/audit-log` | ✅ | backed by real state |
| `/api/services/health` | ✅ | fixed; previously returned all-9 `true` while none ran |
| `/metrics` (Prometheus) | ❌ | not registered |
| `/debug/pprof` | ❌ | not imported, not registered |
| OTLP tracing | ❌ | no OpenTelemetry dependency |

The previous revision listed **27 named Prometheus metrics**
(`localweb_node_uptime_seconds`, `localweb_transport_*`, `localweb_dht_*`, …).
**Zero of them are registered anywhere in the codebase.** A hardcoded metric
string exists inside an example plugin (`pkg/plugin/builtin.go:182-184`) but that
plugin is never instantiated.

Endpoints the SPA calls that the handler does not register. **This list was wrong
twice and is now derived from observed HTTP traffic, not from reading the source.**
The handler registers 33 routes. Driving all 14 screens in a real browser against
a running daemon now produces **34 API calls with zero 4xx and zero console
errors**, against 3 screen-load 404s and 4 action-only 404s before:

| Result | Count | Paths |
|---|---|---|
| `200` | all | `/api/status`, `/api/peers`, `/api/dht/table`, `/api/audit-log`, `/api/audit-log/verify`, `/api/crdt/sync-status`, `/api/services/health`, `/api/dns/records`, `/api/http/sites`, `/api/email/messages`, `/api/messaging/messages`, `/api/docs/documents`, `/api/registry/packages`, `/api/files/list`, `/api/files/transfers`, `/api/registry/installed` |
| `404` | 0 | — |

The seven paths that previously 404'd are now registered:
`/api/files/list`, `/api/files/transfers`, `/api/registry/installed`,
`/api/docs/create`, `/api/docs/save/{id}`, `/api/docs/autosave/{id}`,
`/api/docs/comments/{id}`. Two more surfaced only on the document-editor route
and were also added: `/api/docs/documents/{id}` and `/api/docs/presence/{id}`.
`/api/docs/content/{id}` was added with them.

Two SPA defects were found while verifying this by hand and are fixed: the DHT
search modal's inline style declared `display` twice (`none` then `flex`), so a
full-screen overlay covered the app permanently and swallowed every click on
every screen; and `navigate()` dispatched only through a static route table, so
`#doc-editor-<id>` rendered a blank page on reload, on a shared link, or on
browser back.

Two earlier versions of this endpoint list were wrong. The first was written by
reading `app.js`; the second "correction" removed `/api/files/list`,
`/api/files/transfers` and `/api/registry/installed` on the grounds that a regex
over the source found no reference to them. That was wrong because `fetchAPI`
prepends `/api`, so the literals in the source are `/files/list`,
`/files/transfers` and `/registry/installed`. A browser proves what a regex
cannot.

**The SPA's transport mismatch is fixed.** `connectWS()` opened a **WebSocket**
against `/api/events`, which the server serves only as **SSE** over
`http.ServeMux` with no upgrade handler, so the socket never connected and the UI
retried every 2 s forever. It now uses `EventSource` and subscribes to the nine
documented event names, since a named `event:` frame does not fire the default
`onmessage`. Still true: `BroadcastEvent` has no caller, so the stream carries
only heartbeats until Phase 8 item 8.3 wires real events.

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

Nine service packages exist. **Six of them are now started by the daemon** and
report their real state through `/api/services/health`; three deliberately stay
`false` because they have nothing to start — reporting them up would be theatre.
Service IDs on the wire are ASCII letters (`'C','D','H','M','F','R','V','W','O','G'`),
not the documented `0x00`–`0x09`.

| Service | Listens | Started? | Note |
|---|---|---|---|
| DNS | UDP :5353 (`-dns-port`) | ✅ | real wire codec; zone signing works (`SignZone`). 5353 is mDNS and is usually already held by the OS resolver, so the port is a flag and a bind failure is reported rather than swallowed |
| HTTP gateway | TCP :8082 (`-http-addr`) | ✅ | routes by path prefix, not Host header; `/health` always 200. Not 8081, which the example echo plugin binds and its own tests need free |
| Email | TCP :587/:993 (`-smtp-addr`, `-imap-addr`) | ✅ | real SMTP/IMAP; PoW enforced only when an `X-PoW` header is present, so it is bypassable by omission |
| Files | block store + metadata index | ✅ | store is real and backs `/api/files/list`; `Sync()` still never contacts the peer and `GetFile` still returns a nil data slice |
| Docs | in-process CRDT | ✅ | `RGA.Merge` now converges: nodes are placed at their causal position with siblings ordered by `(Timestamp, Author)` |
| Registry | HTTP :9092 (`-registry-addr`) | ✅ | real publish + signature verify; DHT `ResolveMeta` still returns not-found on its DHT paths |
| Messaging | **no listener** | ❌ | in-memory store; signatures created but never verified. Nothing to start |
| Voice | QUIC only | ❌ | **no codec at all**; no WebRTC/ICE/Opus/VP9 in `go.mod`. Nothing to start |
| VPN | TUN on Linux | ❌ | no packet-forwarding loop; no Windows path beyond a stub that logs and continues |

A live node reports `dns`, `docs`, `email`, `files`, `gui`, `http` and `registry`
healthy and `messaging`, `voice`, `vpn` not. Health is driven by probing
reachability rather than by a `Start` return value, because `Gateway.Start`
blocks in `ListenAndServe` and trusting its return marked a serving gateway down.

---

*LocalWEB Technology Stack — every dependency, target and endpoint verified
against `go.mod`, `make`, and the CI configuration.*
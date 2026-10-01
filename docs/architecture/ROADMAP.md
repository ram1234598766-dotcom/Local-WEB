# LocalWEB — Roadmap

**Grounded in `main`, covering two correctness passes and the packaging and
GUI-HTTP work described in `ARCHITECTURE.md` §9 and §9.1.**
**Module: `github.com/ram1234598766-dotcom/Local-WEB`**

> The previous revision of this roadmap reported "Phase 6 Complete" with a
> checklist asserting ≥90% coverage, `make lint` covering five linters, an SBOM
> from Syft, a clean Trivy scan, and GPG-signed commits. None of those held: the
> measured coverage was 25.9%, `.golangci.yml` enabled two linters, neither Syft
> nor Trivy was configured, and no commit was signed. This revision reports what
> is true.

---

## 1. Where the project actually is

| Metric | Value | How it was measured |
|---|---|---|
| Core protocol | 9 layers designed; L1/L2/L3/L4/L5/L6/L7 substantially implemented; **L8 services not wired into the daemon** | `ARCHITECTURE.md` §1.1 |
| Services | 9/9 exist as tested libraries; **0/9 started by the daemon** | `cmd/node/main.go` registers only `Control` |
| Security | Noise XX + hybrid PQ verified; capability tokens are not Macaroons | `pkg/transport/hybrid_test.go`, `pkg/security/capability.go` |
| Links | 3 partially functional (WiFi/adhoc/USB); BLE stubbed; acoustic and Ethernet absent | `pkg/link` 103 tests |
| Tests | **796 unit + 71 integration**, enumerated with `go test -list` | `make test-unit`, `make test-integration` |
| Coverage | **64.4%** (was 25.9%) | `make test-cover` |
| Quality gates | `go build`, `go vet`, `golangci-lint` (0 issues), `gofmt -s -l` all clean | verified |
| Race detector | clean across the unit suite | `make test-race` |
| Platforms built | 5 (linux/darwin/windows × amd64/arm64 minus windows/arm64) | `make cross-compile` |
| CI | runs on push; **no release workflow** | `.github/workflows/ci.yml` |
| Formal verification | **none** — no `.tla` file exists, no model checker is configured | — |

The honest summary: this is a well-tested **library** with a thin daemon on
top. The next phase should not be new features.

---

## 2. Phase 8 (current) — Close the gap between library and daemon

**Rationale.** Every remaining severity-1 item is a wiring or honesty problem,
not a missing feature. Adding Phase 9 UX on top would multiply the gap.

| # | Item | Severity | Acceptance criterion |
|---|---|---|---|
| 8.1 | Start the nine services in `cmd/node` | Critical | `make run-node` shows `/api/services/health` reporting the services it actually started; each has a live round-trip test |
| 8.2 | `/metrics` with real Prometheus instrumentation; `/debug/pprof` | Critical | `curl :8080/metrics` returns the metrics the docs name; a test asserts the registry is non-empty |
| 8.3 | Serve the SPA's event stream as SSE (or upgrade the client to WS) | High | A peer connecting in the GUI produces a visible live event in an E2E test |
| 8.4 | Persist the audit log across restarts | High | Restart a node, mutate a historical entry, and confirm the chain verification fails |
| 8.5 | Capability tokens: Macaroon caveat chain with attenuation and DHT-distributed revocation | High | Delegate a token, exceed its scope, confirm rejection; verify offline with a third-party key |
| 8.6 | TLS certificate verification on by default | High | **Partly done, and blocked.** `-tls-verify` now exists and the default is stated in the startup log rather than being an accident of a zero value. It cannot be flipped to secure yet: the listener serves a self-signed cert with no CA and no pinning, so a verifying client rejects every peer and the mesh stops connecting. `TestClientCannotVerifySelfSignedServer` demonstrates exactly that. Closing this needs certificate pinning keyed to the node identity. |
| 8.7 | RGA `Merge` as a true positional CRDT merge | High | Two replicas, same ops, different orders → identical state |
| 8.8 | Config file loader matching `config/config.json` | Medium | `localweb --config` works and precedence is documented and tested |
| 8.9 | DHT bucket refresh + split | Medium | Table quality holds after 1k joins/leaves |
| 8.10 | Close the coverage gap on `pkg/services/messaging`, `pkg/gui`, `pkg/dht`, `pkg/services/dns` | Medium | Total ≥75% |
| 8.11 | ~~Repair `nfpm.yaml` output paths; untrack the committed build artifacts~~ | **Done** | `nfpm.yaml` parses and every `src` resolves; `dist/` and `installers/` hold no binaries (68.7 MB removed) — see §5 |
| 8.12 | Add a release workflow invoking goreleaser + cosign + syft | Medium | Tagging produces a signed release with an SBOM |
| 8.13 | Fix the SPA's WebSocket/SSE mismatch and its missing endpoints | Low | **Partly done.** The SPA used a WebSocket against an SSE-only handler and never connected; it now uses `EventSource`. Four `/api/docs/*` paths it calls are still unregistered. |
| 8.14 | ~~`cmd/cli node` subcommand, or remove it~~ | **Done** | It printed "Node started successfully" without starting anything. It now execs the real `localweb-node` binary when present, and otherwise says plainly that it does not start a daemon. Verified by running both paths. |

### Definition of done for Phase 8

```
go build ./...
go vet ./...
golangci-lint run          # 0 issues
gofmt -s -l .              # no output
make test-race             # all pass, no races
make test-integration      # all pass
make test-cover            # >= 75%
make cross-compile         # 5 targets
make ci                    # the gate CI runs
```

Plus, for each of 8.1–8.5, a named test that fails against the current code.

**Items 8.11 and 8.14 are complete. 8.6 and 8.13 are partly complete.** Items
8.1–8.5, 8.7–8.10 and 8.12 are open.

---

## 2a. Phase 6 leftovers

| # | Item | State |
|---|---|---|
| 6.1 | Federation: two nodes across the internet can find each other | ❌ open. The daemon wires `RendezvousDiscoveryMode`, but `discoveryLoop` only re-registers and logs "polling for peers" — it never performs a lookup or emits a `PeerEvent` |
| 6.3 | Multi-path aggregation | ⚠️ open. Failover works; round-robin, bandwidth and latency modes **duplicate** bytes to every active link rather than distributing them |
| 6.6 | ~~QoS / bandwidth shaping~~ | **Done.** `pkg/qos` had zero callers outside its own tests, so nothing was shaped. `transport.TrafficShaper` now gates every outbound service frame and the daemon installs `qos.NewQoSManager` behind `-qos` / `-qos-policy`. Pinned by `TestSendToPassesFramesThroughShaper` and `TestSendToHonoursShaperRejection` |

---

## 3. Phase 9 (planned) — Make the documented physical layers real

Only after Phase 8. These require platform bindings that pure Go does not
provide.

| # | Item | Notes |
|---|---|---|
| 9.1 | BLE via a real backend | Needs cgo or an external helper. `IsAvailable()` must stop returning true unconditionally. |
| 9.2 | Acoustic FSK | Needs an audio I/O binding plus a round-trip encode→audio→decode test at a stated bit-error rate |
| 9.3 | WiFi Direct peer discovery | `listenLinuxEvents` is currently a sleep loop; needs real event parsing |
| 9.4 | Link quality estimation | Kalman/EWMA over RTT, jitter, loss, bandwidth — the input multipath scheduling needs |
| 9.5 | Real multi-path distribution | Round-robin currently duplicates bytes to every link; needs RLNC or MP-TCP |
| 9.6 | Voice codecs | Opus/VP9 via a library, plus ICE/DTLS/SRTP |
| 9.7 | VPN packet forwarding | A read loop on the TUN fd; documented root/`CAP_NET_ADMIN` requirement |
| 9.8 | Files diff sync | `Sync()` must actually contact the peer; benchmark diff vs. full transfer |
| 9.9 | Registry cross-node resolution | `ResolveMeta` currently always returns not-found after a real lookup |
| 9.10 | Plugin sandbox | Per-plugin capability grants; WASM/WASI as the isolation boundary |

---

## 4. Phase 10 (exploratory) — Research

Unchanged in intent from the previous roadmap, with one addition.

| # | Item | Area |
|---|---|---|
| 10.1 | Formal verification | TLA+/Coq for the Noise XX and hybrid KEM state machines. **Note:** the previous roadmap claimed shipped TLA+ specs; none exist. Start from scratch. |
| 10.2 | Anonymous routing | Mixnets, cover traffic |
| 10.3 | Delay-tolerant networking | RFC 5050 Bundle Protocol |
| 10.4 | ML link selection | TinyML, bandits |
| 10.5 | Hardware acceleration | P4, FPGA |
| 10.6 | Quantum networking | QKD |

---

## 5. Documentation — actual state

The previous roadmap listed a 31-file `docs/` tree. **Ten files exist; 21 are
missing**, including all four `.tla` specs and the entire `operations/` and
`development/` trees.

### Exists

```
docs/
├── api/{PLUGIN_API.md, REST_API.md, WS_API.md}
├── architecture/{ARCHITECTURE.md, TECH_STACK.md, ROADMAP.md}
└── guides/{CLI_REFERENCE.md, GUI_GUIDE.md, QUICKSTART.md, SERVICES.md}
```

Plus at the repo root: `README.md`, `CHANGELOG.md`, `CONTRIBUTING.md`,
`SECURITY.md`, `LICENSE` (MIT, genuine), `AGENTS.md`, `PHASE1_FINDINGS.md`,
`PHASE2_PLAN.md`, and a **second, conflicting `ROADMAP.md`**.

### Missing, and whether it should be written

| File | Should it exist? |
|---|---|
| `docs/README.md` (index) | ✅ yes — there is no index today |
| `docs/guides/TROUBLESHOOTING.md` | ✅ yes — "no peers found", port conflicts, VPN privileges |
| `docs/guides/ONBOARDING.md` | ✅ yes — `cli init` has no guide |
| `docs/guides/PLUGINS.md` | ✅ yes — `PLUGIN_API.md` exists but there is no tutorial |
| `docs/guides/FEDERATION.md` | ⚠️ only after the rendezvous poll loop actually looks peers up |
| `docs/api/OPENAPI.yaml` | ✅ yes — the REST API is hand-rolled and drifting |
| `docs/api/GRPC_API.md` | ❌ no — there is no gRPC in the project |
| `docs/operations/*` (5 files) | ⚠️ partially — `SECURITY.md` exists at the root; DEPLOYMENT/MONITORING are premature before the release workflow exists |
| `docs/development/*` (5 files) | ⚠️ `CONTRIBUTING.md` exists at the root; the rest duplicate it |
| `docs/specs/*.tla` | ❌ no — not until Phase 10.1 actually produces them |

There are also two `ROADMAP.md` files with different content. **Partly
resolved:** the root `ROADMAP.md` now names this file as canonical and holds only
the aspirational Phase 6–11 plan, so neither is orphaned and neither contradicts
the other. They are still separate documents by design — measured state here,
intent there. A full merge would either lose the Phase 9–11 planning or dilute
this file with unverified completion claims.

### Committed build artifacts — RESOLVED

68.7 MB of build output used to be tracked in git across 17 files. All of it is
now removed and `.gitignore` covers `/dist/`, `*.deb`, `*.rpm` and `*.apk`:

| Removed | Why it was stale |
|---|---|
| `dist/localweb_1.0.0-1_amd64.deb`, `dist/localweb-1.0.0-1.x86_64.rpm`, `dist/localweb_1.0.0_p1_x86_64.apk` | `1.0.0` packages, rebuilt by CI and by `nfpm package` |
| the same three again under `installers/windows/` | Linux packages committed inside the **Windows** installer directory |
| `dist/localweb-cli-linux-amd64`, `dist/localweb-cli-darwin-arm64` | rebuilt by `make cross-compile` into `bin/`; CI builds its own copy |
| `pkg/localweb_1.0.0-1_amd64.deb` | **0 bytes** — an empty artifact inside the Go source tree |
| `installers/windows/wintun.zip` | superseded by `scripts/fetch-wintun.sh`, which downloads Wintun 0.14.1 and verifies its SHA-256 |
| `installers/windows/wintun/wintun/` | a stray partial extraction: a duplicate `LICENSE.txt` (byte-identical to the `LICENSE` already kept), the upstream `README.md`, and `wintun.h` — none used by either installer |
| `dist/README.md`, `dist/LICENSE`, `dist/CHANGELOG.md`, `dist/config/config.json` | unreferenced staging copies; `nfpm.yaml` and the build scripts read the root `README.md`, `LICENSE`, `CHANGELOG.md` and `config/config.json` |

Verified after removal: `make msi` and `make nsis` both still build, and the
built NSIS installer still contains all six PowerShell scripts, `wintun.dll`,
`wintun.dll.sig`, `localweb.exe` and `localweb-cli.exe`.

Two related version inconsistencies were also corrected: `wails.json` declared
`1.0.0` while `nfpm.yaml` and the Makefile declare `1.0.1`.

### Documentation debt found while auditing

Still open:

- `docs/api/REST_API.md` and `docs/api/WS_API.md` now match the code (see below),
  but no test asserts that every documented endpoint exists and every documented
  field is actually returned. The two mismatches found were found by reading, not
  by tooling; a drift check would catch the next one.

Now fixed:

- `CHANGELOG.md` claimed "Formal TLA+ specifications for core protocols" shipped.
  No `.tla` file exists and no model checker is configured, so the claim was
  removed and a correction note added in place. The same section also called the
  capability tokens "Macaroon-based"; they are a flat Ed25519-signed struct with
  no caveat chain and no Macaroon library in `go.mod`.
- `docs/api/REST_API.md` documented `/api/audit-log/verify` returning `200` with
  an `integrity` field. The handler returned `500` on tampering and had no such
  field, because `AuditLogVerified()` conflated "no audit log" with "tampered".
  Now returns `200` with `integrity` for a broken chain and `500` only when there
  is no chain to verify. Pinned by `TestAuditVerifyHandlerTamperedReturns200`,
  which fails against the previous handler.
- `docs/api/WS_API.md` documented a `: heartbeat` comment every 30 s that the
  handler never sent, so a connected-but-silent stream was the only observable
  behaviour. Implemented, pinned by `TestEventsHandlerEmitsHeartbeat`. The same
  investigation found the SPA opened a **WebSocket** against that SSE-only
  endpoint, so it never connected and retried every 2 s forever; it now uses
  `EventSource`.
- `AGENTS.md` said there was no `LICENSE`, no CI, no `SECURITY.md` and no
  `CONTRIBUTING.md`. All four exist; the claims are corrected.
- Version split: `wails.json` declared `1.0.0` while `nfpm.yaml` and the Makefile
  declared `1.0.1`; `wails.json` is now `1.0.1`. `internal/version` still defaults
  to `"dev"`, which is correct — CI overrides it with `-ldflags -X …Version=${{
  github.ref_name }}`. `CHANGELOG.md` had no `1.0.1` entry although v1.0.1
  shipped; one is now recorded.
- Root `ROADMAP.md` claimed "Phase 5 Complete | Phase 6 Complete" and "all 7
  sub-phases complete". Re-measured: 6.6 QoS has no callers, 6.3 multi-path
  duplicates rather than distributes, and 6.1 federation never performs a peer
  lookup. Each status line now carries its evidence. It also claimed "13 screens"
  (there are 14), "all backed by real API endpoints" (four `/api/docs/*` paths are
  not registered), and "SSE real-time updates" (`BroadcastEvent` has no callers).
  Its stale `$(date)` placeholders were also expanded.
- The **v1.0.0** release page now carries a prominent warning that its assets
  contain the mDNS panic and points to v1.0.1. The binaries are unchanged so
  their published checksums still verify.
- `docs/architecture/ROADMAP.md` itself: the committed-artifact table, the
  coverage figures, the test counts, the `pkg/link` count and the number of tests
  revived from `setup.go` (17, not 16) were all stale. All re-measured.

---

## 6. Release process — what exists

| Step | State |
|---|---|
| Cross-compile 5 targets | ✅ `make cross-compile` |
| Windows MSI + EXE | ✅ `make msi`, `make nsis`, reproducible from a clean clone |
| Checksums | ⚠️ generated by hand for the release; **no make target** produces them |
| GitHub Release | ⚠️ v1.0.1 published manually with verified assets and checksums; **still no release workflow**, so this is not repeatable by CI |
| SBOM | ❌ no Syft in the toolchain or CI |
| Signing | ⚠️ `.goreleaser.yml` uses GPG; no key, no cosign, and the macOS certificate is an unfilled placeholder |
| Docker images | ❌ `Dockerfile` exists, nothing builds or pushes it |
| Homebrew / pkg.go.dev | ❌ not wired |
| Tagged commit signing | ❌ no commit in the history is signed |
| Trivy scan | ❌ not configured |

Phase 8 item 8.12 addresses this.

**Outstanding on the last release.** ✅ Resolved. The **v1.0.0** assets are still
published and still contain the LAN-reachable mDNS panic fixed in v1.0.1
(`ARCHITECTURE.md` §9 item 21). The release description now opens with a warning
naming the defect and linking to v1.0.1; the binaries themselves are unchanged so
their published checksums still verify. Replacing or deleting them would break
those checksums, so the warning is the chosen mitigation.

---

## 7. Milestones

| Milestone | Scope | Status |
|---|---|---|
| Current | library-complete, daemon thin | ✅ measured above |
| Phase 8 | services wired, observability real, security honest | 📋 next |
| Phase 9 | physical link layers and codecs real | 📋 planned |
| Phase 10 | formal verification and research | 🔬 exploratory |

No target dates. The previous roadmap carried v3.1.0 (2025-11-28) and v3.2.0
(2026-03-20) as "Planned" while both dates were already ten and eighteen months
in the past, and marked a v3.0.0 milestone "Done" that never shipped under that
name. Dates without a team behind them are noise; the milestones above are
scoped by what must be true, not by when.

---

*LocalWEB Roadmap — coverage, test counts and gate results measured, not
estimated.*
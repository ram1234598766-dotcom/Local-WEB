# Local-WEB — Audit, Beginner Experience & Advanced Power Charter (v3)

You are a senior distributed-systems engineer, security reviewer, and product
engineer working on this repo — a Go P2P networking stack, 9 layers, 9
services. The README marks everything "production-ready." **Treat that as a
hypothesis, not a fact** — ground truth is executed tests and read code.

**Design philosophy for everything after Phase 2:** simple by default,
powerful underneath. A newcomer should get two laptops talking in one
command without reading the architecture diagram; an advanced user should
still be able to reach every dial. These are not in tension — sequence them:
correctness/security first, then beginner experience, then advanced power.
Don't skip ahead.

## Non-negotiables
1. Never mark anything ✅ verified without naming the test run and its actual
   output.
2. Cite `file:line` for every claim, positive or negative.
3. Don't refactor for style before correctness/security findings are fixed.
4. If something's genuinely solid, say so and move on.
5. Never weaken an assertion or delete a test to make a suite pass.
6. Work phase by phase. Stop and report after Phase 1 and after Phase 3;
   wait for confirmed priorities before continuing.

## How to use this
Drop this in as `AGENTS.md` in the repo root so OpenCode loads it
automatically, or paste it as the first message in a fresh session. If you
already have a Phase 1 findings report from an earlier run, hand it back
instead of re-auditing from scratch — pick up at Phase 2. Phase 5 (the
graphical interface) only needs Phase 2's fixes in place — run it before or
after Phase 3/4's CLI polish and advanced backlog, whichever you want to use
day to day.

---

## Phase 0 — Baseline (read-only)
```
go version
go build ./...
make lint
make test                     # -race enabled? coverage tracked?
go test ./test/integration/... -v
govulncheck ./...
```

---

## Phase 1 — Correctness & security audit (L1–L9)

### Project hygiene (confirmed gaps — check and fix, not hypothetical)
- **Module path mismatch risk**: the README's own Go example imports
  `github.com/mrityunjay/LocalWEB/pkg/services/messaging`, but the repo lives
  at `github.com/ram1234598766-dotcom/Local-WEB`. Check what `go.mod`
  actually declares — if it doesn't match the real repo URL, `go get`/
  `go install` silently fails for anyone outside the original dev machine.
  Fix the mismatch (module path or docs, whichever is wrong).
- **No `LICENSE` file** — the README states "MIT" three times but there is
  no license file in the repo. A README claim is not a grant; add the file.
- **No `.github/workflows/`** — zero CI currently runs on this repo. Nothing
  here should be called "production-ready" without CI proving it stays that
  way on every push.
- **No `SECURITY.md`, no standalone `CONTRIBUTING.md`** — the README embeds
  a short contributing note but there's no real doc for either.

### Per-layer audit
For each layer: check the stated properties, run or write the minimum test
needed, flag red flags. Assume nothing holds because a comment says it does.

**L1 Transport** (QUIC, 1-byte ServiceID mux, circuit relay, NAT hole-punch)
Stream demux under concurrent load; backpressure bounds memory (slow-consumer
test); context cancellation tears down cleanly. NAT traversal verified
against simulated symmetric NAT or documented as a limitation. Red flag: no
read/write deadlines on streams.

**L2 Link layer** (WiFi Station/Direct, BLE, USB tether, ad-hoc, acoustic FSK)
Highest risk of being stubbed — pure Go has no native cross-platform BLE or
WiFi Direct. Check `go.mod`/build tags for a real per-platform backend. For
acoustic FSK, confirm a real audio I/O binding and a round-trip encode→audio→
decode test at a stated bit-error rate. Red flag: `Send`/`Receive` that
unconditionally returns nil or silently delegates to another link type.

**L3 Discovery** (mDNS-SD, BLE discovery, orchestrator score)
Dedup by identity across transports; TTL eviction enforced; "score" from
real signal (RTT, link type, recency), not a constant.

**L4 DHT** (Kademlia, k=20, α=3, iterative lookup, PoW anti-Sybil)
Churn test: nodes joining/leaving mid-lookup still converge; buckets refresh
and split. PoW actually rejected below difficulty, not computed and ignored.

**L5 Security** (Noise XX/X25519/SHA3-256, Ed25519 identity, capability
tokens, PoW, append-only audit log) — strictest bar, bugs here are
exploitable.
Full handshake with forward secrecy + replay protection tested; tamper/
expire/replay a capability token and confirm rejection; canonical JSON
serializer checked for deterministic key ordering; PoW comparison is
constant-time; audit-log tamper test (mutate a past entry, confirm chain
detects it). Grep for keys/seeds near any log call.

**L6 Store** (BadgerDB, AES-GCM, content-addressed blocks)
Real source of the encryption key confirmed (no hardcoded fallback reachable
in prod); no nonce reuse possible under concurrent GCM writes; blocks
re-hashed and checked on read.

**L7 CRDT** (OR-Set add-wins, RGA text, Merkle DAG diff sync)
Convergence test: same ops, different orders, two replicas → identical
state. OR-Set add-wins under concurrent add+remove. RGA concurrent inserts
converge to the same order; tombstones don't grow unboundedly. Diff sync
benchmarked against naive full sync. Red flag: any CRDT test that only runs
sequentially on one replica.

**L8 Services** — verify each independently, end-to-end, against a live node:

| Service | Verify |
|---|---|
| DNS (.localweb, UDP 5353) | Signed records resolve; forged records rejected |
| HTTP gateway (:8080) | `/health` reflects real status; per-site routing isolation |
| Email (SMTP :587 / IMAP :993) | Real round trip; PoW gates acceptance, not decorative |
| Messaging (:9090) | Signatures checked; offline queue persists and redelivers |
| Files (Bitswap-like, zstd) | Resumable/diff transfer measurably smaller than full transfer |
| Docs (:9091, RGA) | Concurrent editors converge; presence/cursors broadcast |
| Registry (:9092, LWPKG+DHT) | Publish→fetch round trip via DHT from a *different* node |
| Voice (:9093, ICE+Opus/VP9) | Codec is library-backed, not raw PCM passthrough |
| VPN (:9094, TUN) | Real IP routing across tunnel; root/CAP_NET_ADMIN requirement documented |

**L9 App** (daemon, CLI)
Config precedence matches docs; SIGINT/SIGTERM flushes store cleanly — kill
mid-write in a test, check for corruption on restart.

### Phase 1 output (required, stop here)
| Layer/Service/Hygiene item | README/repo claim | Verified how | Result | Severity | Evidence (file:line) |
|---|---|---|---|---|---|

Result: ✅ Verified / ⚠️ Partial or untested / ❌ Stub, broken, or missing.
Rank: Critical (security/data-loss) → Correctness gaps → Missing coverage →
False "production-ready" claims → Missing hygiene → Polish.

---

## Phase 2 — Fix (TDD, only after Phase 1 is reviewed)
Top-down through the ranked list: failing test first → fix → `make lint` and
`make test -race` pass → commit (`feat:`/`fix:`/`refactor:`/`test:`/`docs:/
chore:`, one logical change per commit). This phase explicitly includes the
confirmed hygiene gaps: add `LICENSE`, add `.github/workflows/ci.yml`
(build+vet+lint+test(-race)+govulncheck on push/PR), add `SECURITY.md`,
add `CONTRIBUTING.md`, and resolve the module-path mismatch. Update the
README status table and `docs/architecture/ROADMAP.md` to distinguish
implemented-and-tested vs. implemented-untested vs. not-yet-implemented.

---

## Phase 3 — Interface & beginner experience (only once Phase 2's critical
items are resolved)

**3.1 One-command quickstart**
`make quickstart` (or `scripts/quickstart.sh`): build, generate identity,
start the node, print `Your node ID is <id> — run 'cli peers' on another
machine on the same network to find it.` Replaces today's four separate
manual steps (clone → build → run node in one terminal → run cli in
another) with one.

**3.2 Plain-language front door**
Add 3–5 sentences in plain English — *what this is and why someone would
want it* — above the 9-layer architecture diagram in the README. Right now
a newcomer hits a dense ASCII architecture diagram before any explanation of
the problem this solves for them. Add a short Troubleshooting/FAQ section:
"no peers found" (same-subnet requirement, firewall), "port already in use",
and the VPN service's elevated-privilege requirement (currently undocumented
anywhere).

**3.3 Guided CLI onboarding**
`cli init`: plain-language prompts, sensible defaults, one line explaining
what a "node identity" and "data dir" are before creating them. `--help` on
every command with a one-line description and an example. Friendly,
actionable errors — e.g. *"Could not bind to :4443 — another process is
using this port. Try --listen :4444."* instead of a bare Go error.

**3.4 One interface, two audiences (progressive disclosure)**
CLI stays the default, with `--json` on every command for scripting.
Optional, opt-in, localhost-only read-only web dashboard: status, peers,
service health — approachable for a newcomer, while `/metrics`
(Prometheus format) and `/healthz`/`/readyz` per service stay available for
anyone who wants to script or monitor it directly.

### Phase 3 output
Report what shipped and each new surface's default exposure/auth. Wait for
confirmation before Phase 4.

---

## Phase 4 — Advanced & powerful capabilities (backlog, opt-in — implement
only what's requested; CI/LICENSE/SECURITY.md are already handled in Phase 2,
not repeated here)
1. **Federation beyond LAN**: rendezvous/relay servers so two nodes across
   the public internet — not just the local network — can find each other;
   today's discovery layer (mDNS/BLE/WiFi Direct) is LAN-only by design.
2. **Post-quantum-ready handshake**: hybrid X25519+Kyber in the Noise layer,
   so the security story doesn't need a rewrite later.
3. **Multi-path link aggregation**: use BLE and WiFi simultaneously for
   redundancy, instead of one-at-a-time escalation.
4. **Plugin/extension interface** so third-party services can register
   alongside the 9 built-in ones without forking the daemon.
5. **Chaos/fault-injection harness**: automate packet loss, partition, and
   churn simulation in CI, not just manually-triggered integration tests.
6. **QoS/bandwidth shaping** per peer or service — useful once Voice, VPN,
   and Files compete for the same link.
7. **Proper module publishing**: once the module-path mismatch is fixed,
   publish to pkg.go.dev with real godoc on every exported type, so the
   advanced capabilities are actually discoverable and usable by others.

---

## Phase 5 — Full graphical interface (desktop/web GUI)

Only start this once Phase 2's critical fixes are in place. This phase
replaces the "optional read-only dashboard" mentioned in Phase 3 with a
complete, first-class graphical application — not a decorative wrapper
around the CLI.

### 5.0 Architecture decision (check first, don't assume)
Check what's already in the repo (`package.json`, any existing frontend
scaffolding) before picking a stack — don't introduce a second frontend
approach if one already exists.

Recommended default, if nothing exists yet: build a web-based SPA served by
the existing L8 HTTP gateway service (extended with a JSON/WebSocket API),
opened at `localhost:8080` by default. Only wrap it as a native desktop app
afterward — Wails is the natural fit for a Go backend, since it avoids
bundling a full Node/Electron runtime — once the web UI itself is solid.
Don't build the native shell first.

The GUI talks to real backend data through a real API. If an endpoint the
UI needs doesn't exist yet (e.g. `/api/status`, `/api/peers`,
`api/services/health`, `/api/dht/table`, `/api/audit-log`,
`/api/crdt/sync-status`), add it — with its own test — rather than faking
data in the frontend. **Never mock or hardcode data in the shipped UI.**

Real-time updates (peer connect/disconnect, transfer progress, sync
status) go over a WebSocket or SSE stream from the daemon, not polling
hacks, unless polling is a deliberate, stated fallback.

### 5.1 Design direction (do this before writing any UI code)
Ground the visual identity in what this product actually is: a local-first,
infrastructure-free, encrypted mesh network someone runs on their own
laptop — not a generic SaaS admin panel. Work in two passes:

**Plan**: propose a compact token system — 4–6 named colors, one or two
typefaces with clear roles, a layout concept (ASCII-wireframe a couple of
core screens), and 2–3 principles specific to this product (e.g. "peer
connections are the hero visual, not a stat card").

**Critique against generic tells before building**: no warm-cream-and-
terracotta or near-black-and-acid-green default palette picked by default;
no identical rounded SaaS cards with the same soft grey shadow on
everything; no tracked-out ALL-CAPS eyebrows; no "01/02/03" numbering
unless the content is truly a sequence; no reflexive arrow (→) on every
button. If the plan reads like the default anyone would produce for "a
network dashboard," revise it and say what changed and why.
Only start implementation once the plan is confirmed against that
checklist. Keep boldness in one place — the network/peer visualization is
the natural hero — and keep the chrome around it quiet.

### 5.2 Screens (cover every service, not just an overview page)
- **Onboarding**: visual version of `cli init` — identity generation, data
  dir, a plain-language explanation at each step, not a wall of settings.
- **Dashboard**: node identity, uptime, resource use, connectivity summary
  at a glance.
- **Network / Peers**: list view *and* a real topology visualization (which
  peers are connected via which link type, RTT) reflecting actual
  DHT/discovery state, not a static illustration.
- **Per-service panels** (all 9, each genuinely functional, not just a
  status pill): DNS (record browser/search), HTTP (per-site routing
  status), Email (minimal inbox + compose), Messaging (channels + compose +
  history), Files (browse/send/receive with real transfer progress), Docs
  (opens the collaborative text editor directly), Registry (browse/install
  packages), Voice (call UI — connect/mute/hang-up, real ICE/codec state),
  VPN (connect/disconnect toggle, active routes, real tunnel status).
- **Security & identity**: NodeID/public key display and export, capability
  token list, audit log viewer with a live tamper-chain indicator computed
  from a real re-verification, not a static green check.
- **Settings**: config editor for ports/paths/storage, theme (dark/light),
  and a literal "Basic / Advanced" toggle that shows or hides the low-level
  dials — this is where the beginner/advanced split from earlier phases
  becomes a UI control, not just a doc convention.
- **Notifications**: toasts for real events (peer connected, transfer
  complete, sync conflict, error), written in the interface's voice — say
  what happened and what to do next, never vague.

### 5.3 Quality bar
Responsive down to a small window; visible keyboard focus on every
interactive element; respects reduced-motion; motion reserved for real
state changes (a peer connecting, a transfer completing), never decorative
entrance animations on every panel; dark and light themes both fully
supported, not one default with an unstyled alternate; copy written from the
end user's perspective in plain language (a user manages "Peers" and
"Files," not "discovery daemon" or "block store").

### 5.4 Build sequence (don't attempt all of this in one pass)
1. Foundation: API contract + real-data plumbing, no UI yet — confirm each
   endpoint returns real backend state before writing a single screen.
2. Dashboard + Network/Peers view.
3. One service panel end-to-end (pick the simplest, e.g. Files) to prove
   the full pattern — API, real-time updates, UI — before repeating it 8
   more times.
4. Remaining 8 service panels.
5. Security/identity + Settings + notifications.
6. Accessibility/responsiveness/theme pass, then a design self-critique
   against 5.1's checklist before calling it done.

### Phase 5 output
Report which screens are backed by real endpoints vs. still pending, and
which platform(s) the app currently runs on (browser only vs. native
wrapper). Confirm before treating any screen as finished.

---

## Definition of done
- **Correctness/security bar** (primary gate, unchanged): test-proven, not
  claimed, per Phase 1 criteria.
- **Beginner bar**: someone who has never touched this repo can go from
  `git clone` to two machines finding each other with one documented
  command, without reading the architecture section first.
- **Advanced bar**: every Phase 4 capability ships behind an explicit
  opt-in flag/config, with its own tests, and never changes the default
  behavior for someone who just wants the simple path.
- **GUI bar**: every Phase 5 screen is backed by a real API endpoint (no
  mocked or hardcoded data), meets the Phase 5.3 quality bar, and the visual
  design was reviewed against the genericness checklist in 5.1 before being
  built.

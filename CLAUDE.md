# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`stone-access` is a NATS-native physical access control (PACS) system built on the
[Stone-Age.io](https://stone-age.io) primitives (NATS core, KV, JetStream, PocketBase).
Two Go binaries plus a Vue 3 management UI:

- **`accessd`** (`cmd/accessd`) — central system of record. Embeds PocketBase (control plane + schema),
  mirrors the policy graph to NATS KV (one key per record), runs the JetStream audit consumer, and runs the
  controller-health monitor (heartbeats → `controllers.last_seen`/`status`). Serves the embedded UI at `/`.
- **`access-controller`** (`cmd/access-controller`) — edge runtime. Watches the KV keyspace into in-memory
  maps, decides credential presentations **locally** with the pure `policy.Decide`, drives reader/lock/door-input
  hardware, runs a per-door forced/held-open state machine, emits access events to JetStream, and publishes a
  liveness heartbeat.

v1 status: the **reader is simulated** — taps arrive by publishing to `acc.{location}.{type}.{thing}.tap` (a real
OSDP/Wiegand `ReaderDriver` slots in later). The **lock and door inputs have real drivers**: `internal/drivers`
holds the interfaces + mocks; `internal/drivers/gpio` is a no-cgo GPIO backend (KinCony Server-Mini) keyed by a
per-model profile in `internal/drivers/hardware`. A controller picks `mock` (default) or `gpio` via
`controller.driver`.

## Build & run

The UI is `//go:embed`-ed into `accessd` **at Go compile time**, so the UI must be built before the binary.
The committed `internal/webui/public` means a fresh checkout builds without npm — but **rebuild and re-commit
`internal/webui/public` whenever the frontend changes**.

```bash
cd ui && npm install            # once (needs Node 20.19+ / 22.12+ for Vite 8)
npm run build                   # → internal/webui/public  (vue-tsc typecheck + vite build; commit the output)
cd .. && go build ./cmd/accessd
./accessd serve                 # UI at http://127.0.0.1:8090/ · PocketBase admin at /_

go build ./cmd/access-controller
./access-controller -config config/controller.yaml
```

UI dev server (proxies `/api` + `/_` to `:8090`): `npm --prefix ui run dev` → http://localhost:5174

Create the admin login (collections are superuser-only): `./accessd superuser upsert <email> <pass>`

`accessd` is driven by PocketBase's CLI (`serve`, `migrate`, `superuser`). NATS/KV/audit resources come up
**only on `serve`**, not for `migrate`/`superuser`.

## Test

```bash
go test ./...                                    # all tests are pure — no NATS/network needed
go test ./internal/policy -run TestDecide        # single package / test
cd ui && npm run build                           # UI has no test suite; the typecheck in build is the gate
```

## Architecture

### The decision is a pure function

`internal/policy` is the core. `Decide(p, loc, posture, cred, portal, atUTC) Decision` is a pure function over
plain maps — no I/O, no locks, no rules engine. It runs identically on central and edge and is table-tested.
The graph mirrors the operator's mental model 1:1: **user → roles → access groups → (portals + one schedule)**.

**Evaluation order is the contract — deny-overrides come first** (see the doc comment on `Decide`):
unknown portal → posture gate (disabled/lockdown deny; `unlocked`/`free_access` allow without consulting the
credential; secure continues) → credential/user status (incl. credential `validFrom`/`validUntil` bounds) → grant
walk (a group containing the portal whose schedule window is open *and the day isn't a holiday the schedule
observes*). Everything unrecognized or not-yet-synced **fails closed (deny)**. A zero `Policy{}` default-denies
everything. The **effective posture** fed to the gate is resolved by the controller (command override → scheduled
posture `autoPosture` while `autoSchedule` is open → standing `posture`), so `Decide` stays pure.

Two postures both "allow without consulting the credential" but differ physically (the controller owns this, not
`Decide`): `free_access` opens on any tap with the strike pulsing (door stays closed, every entry logged);
`unlocked` holds the strike open (free passage, no tap needed).

### Data flow (one direction, eventually consistent)

```
operator edits PocketBase ─┐
                           ├─► internal/mirror ──► NATS KV (ACC_POLICY) ──► controller PolicyStore (in-mem maps)
migrations seed fixture ───┘    (one key/record)        watch                       │
                                                                                    ▼  policy.Decide (local)
events collection (UI) ◄── internal/audit ◄── ACC_EVENTS JetStream ◄── acc.*.…evt.> ◄── Runtime (tap loop)
   (rebuildable read model)   consumer                                              pulses lock on allow
```

- **Mirror** (`internal/mirror`) is deliberately dumb: one PocketBase record → one KV key, via after-commit
  record hooks. No aggregation, no whole-policy rebuild. `SyncAll` reconciles on boot (covers migration-seeded
  data and changes made while accessd was down) and prunes stale keys.
- **Wire contract** (`internal/policykv`) is the shared JSON shape + key scheme between mirror (writer) and
  PolicyStore (reader). Key = `<prefix><natural-key>`, e.g. `cred.CARD-001`, `portal.lobby-main`, `user.<pbid>`.
  **Cross-references are stored as stable codes** (or credential value / cardholder id), never PocketBase ids,
  so KV stays human-readable and self-contained.
- **PolicyStore** (`internal/controller/policystore.go`) watches `WatchAll`, parses into maps behind an RWMutex,
  resolves each location's timezone once on apply (hot path never calls `LoadLocation`). Self-heals across NATS
  reconnects: `Resync` (wired to the reconnect handler) stops the watcher so `runWatch` re-creates it
  (`WatchAll` re-delivers every key = full re-sync). On each applied change and each sync sentinel the store
  fires `SetOnChange`, which drives the controller's **watch-driven arming**.
- **PortalManager** (`internal/controller/portalmanager.go`) keeps the controller's armed portals in step with
  policy. Binding is **central**: the set this box drives is every portal whose `controller` relation points at
  this controller's `code` (`PolicyStore.PortalsForController`), so reassigning a portal to another box, retyping
  it, or removing it takes effect without touching the box — local config shrinks to identity (`controller.code`).
  Arming reconciles on every policy change (coalesced, off the watch goroutine): for each portal it arms the
  reader subscription, the lock, and the DPS/REX inputs via a `PortalHardware` backend (`drivers.MockHardware`
  or the GPIO driver), and disarms the lot when the portal leaves. The controller boots **default-deny** (armed
  for nothing) instead of blocking or crashing when policy is slow/unreachable, and converges as policy arrives.
  It binds the policy KV **read-only** (`natsx.KVBucket`); accessd owns bucket creation.
- **Door monitoring** (`internal/controller/runtime.go`) — a per-door state machine over DPS/REX inputs emits
  `evt.alarm` events (`type`: `forced`/`held`/`held_clear`). A grant or REX opens a short authorized-open window
  (no `forced`) and arms the held-open (DOTL) timer; a location's fire input suppresses all alarm emission. The
  hardware binding (logical relay/input indices, held-open threshold) rides policy on the portal record, never
  the pure `policy.Decide`; the box maps logical indices to physical lines via its `model` profile.
- **Audit** (`internal/audit`) — JetStream is the system of record for events; the PocketBase `events`
  collection is a rebuildable projection. Durable consumer, at-least-once (a redelivery may dup a row in v1).
- **Controller health** (`internal/health`, accessd-side) — a core-NATS subscriber to the heartbeat subject
  updates `controllers.last_seen`/`status` with a **direct record update, not an events row**, plus a staleness
  sweep that marks a silent box offline. Heartbeats are deliberately kept out of the audit stream.

### NATS subjects

Every subject **leads with the app token `acc`**: the access app owns the `acc.>` subtree and a **portal** is a
Thing addressed underneath it as `acc.{location}.{type}.{thing}`, with the verb trailing. `{type}` is the portal
kind (door/turnstile/elevator/gate/logical), a single NATS token. The leading literal `acc` is load-bearing — on
a shared NATS account, a stream subject that *led with a wildcard* (e.g. `*.*.*.acc.evt.>`) overlaps any sibling
stream rooted at a literal first token (`things.>`, `cameras.>`, `kiosk.*.event.>`, …), and JetStream rejects
overlapping stream subjects (err 10065). Leading with `acc` keeps our subject space disjoint from theirs.

- `acc.{location}.{type}.{thing}.tap` — credential presentation (v1: simulated reader publishes here)
- `acc.{location}.{type}.{thing}.evt.{kind}` (`tap`/`state`/`alarm`) and `acc.{location}.evt.fire` (location-scoped) — audit events → ACC_EVENTS
- `acc.{location}.{type}.{thing}.cmd.posture` / `.cmd.unlock` — control-plane commands (core NATS, fire-and-forget)
- `acc.{location}.ctrl.{code}.heartbeat` — controller liveness. A controller is addressed under the reserved
  `ctrl` namespace (not a portal type); the heartbeat sits **outside** the `.evt` subtree (5 tokens, no `evt`) on
  purpose, so ACC_EVENTS never captures it — accessd updates the `controllers` record directly instead.

**All subject construction and parsing lives in `internal/subjects`** (one `Subjects` value carrying the `acc`
app token from `subjects.app` config, default `acc`, threaded through every constructor) — never hand-format
subject strings elsewhere. The audit stream captures two patterns of **different fixed arity** — `acc.*.evt.fire`
(4-token fire) and `acc.*.*.*.evt.>` (6+-token portal events) — both rooted at that literal app token, so they
overlap neither each other nor a sibling app's stream, and can't capture a foreign Thing's events. (The fire
pattern is fixed-arity, no trailing `>`, so it can't expand to overlap the portal pattern.) **accessd and every
controller must share the same `subjects.app`** (a mismatch silently severs policy/commands/events).
`docs/protocol.md` is the full wire reference (subjects, message shapes, KV key scheme, decision reason codes).

Commands (`internal/controller/commands.go`) install **runtime posture overrides** that are operational state,
never written back to PocketBase. `posture: "clear"` reverts to the effective posture (scheduled if open, else
standing). The controller grows **no policy ticker** — `until` (timed reversion) comes from outside (an external
scheduler publishing a follow-up command). The only timers are **three** deliberate, scoped exceptions: the
per-door held-open (DOTL) timer, the liveness heartbeat, and the **scheduled-posture hold-eval reconcile**
(`runtime.reconcileHolds`, default 10s) — a sampling loop that flips the strike hold at schedule-window boundaries
(the no-event case) and is backed up by immediate reconciles on posture commands and on portal arming. Only the
`unlocked` posture holds the strike (via `LockDriver.SetHeld`, which composes with `Pulse`); everything else is
enforced lazily at tap, so physically the strike is just not held. Fire input suppresses alarm emission (hardware
owns egress).

### Schema is code

`pbmigrations` defines collections in Go (`1750000000_collections.go`) and seeds an idempotent dev fixture
(`1750000001_fixture.go`, no-ops if `locations` is non-empty). Later additive migrations extend the schema:
`1750000002` (credential `valid_from`/`valid_until`), `1750000003` (the `holidays` collection +
`schedules.ignore_holidays`, the inverted opt-out so observe-holidays is the default), `1750000004` (portal
`auto_posture`/`auto_schedule` for scheduled posture), and `1750000005` (fixture extras that demo holidays +
auto-unlock — they must run *after* the schema that defines them, so they can't live in the base fixture).
Collections have **nil access rules = superuser-only**, the right default for a PACS control plane. `migratecmd`
Automigrate snapshots dashboard collection edits into new Go files beside the hand-authored ones — review those
before committing.

## Config

Unified `config.Config` (`config/config.go`) for both binaries, loaded via Viper. Every key is overridable by an
**`SA_`-prefixed env var** (`SA_NATS_URLS`, `SA_CONTROLLER_LOCATION`, ...). A missing config file is fine — defaults +
env apply. `accessd` reads `$SA_CONFIG` (default `config/accessd.yaml`); `access-controller` uses its `-config`
flag (default `config/controller.yaml`). Exactly one NATS auth method may be set (creds file / nkey / token /
user-pass); `*.creds` and `config/local*.yaml` are gitignored — never commit credentials.

A controller's config is just its identity and hardware selection: `controller.code` (which portals it drives,
matched against the policy graph), `controller.location` (timezone + command/fire subscription scope),
`controller.driver` (`mock`|`gpio`), `controller.model` (required for `gpio`; selects the hardware profile), and
`controller.heartbeatInterval`. accessd's `accessd.controllerOfflineAfter` sets how long a silent controller stays
"online" before the health sweep marks it offline.

## Conventions

- Module path `github.com/stone-age-io/access-control`, Go 1.26. Structured logging via `zap` wrapper
  (`internal/logger`); Prometheus metrics (`internal/metrics`) on a side port (accessd `:2113`, controller `:2114`).
- **Fail-safe everywhere**: dangling references, malformed values, not-yet-synced records, parse errors — all
  resolve to deny or "keep previous value," never to a grant or a crash.
- Decision **reason codes** (`policy.go`) are stable strings that flow verbatim into events and the UI — treat
  them as a public contract.

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`stone-access` is a NATS-native physical access control (PACS) system built on the
[Stone-Age.io](https://stone-age.io) primitives (NATS core, KV, JetStream, PocketBase).
Two Go binaries plus a Vue 3 management UI:

- **`accessd`** (`cmd/accessd`) — central system of record. Embeds PocketBase (control plane + schema),
  mirrors the policy graph to NATS KV (one key per record), and runs the JetStream audit consumer.
  Serves the embedded UI at `/`.
- **`access-controller`** (`cmd/access-controller`) — edge runtime. Watches the KV keyspace into in-memory
  maps, decides credential presentations **locally** with the pure `policy.Decide`, drives reader/lock
  hardware (mocked in v1), and emits access events to JetStream.

v1 status: **software substrate, no hardware.** Reader/lock/FAI are interfaces (`internal/drivers`) with
mock implementations. Taps are simulated by publishing to `acc.{location}.{type}.{thing}.tap`.

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
unknown portal → posture gate (disabled/lockdown deny; unlocked allows without consulting credential; secure
continues) → credential/user status → grant walk (a group containing the portal whose schedule window is open).
Everything unrecognized or not-yet-synced **fails closed (deny)**. A zero `Policy{}` default-denies everything.

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
- **PortalManager** (`internal/controller/portalmanager.go`) keeps the controller's armed readers/locks in step
  with policy. The set of portal *codes* a controller drives is local config (which doors this box is wired to),
  but each portal's type/existence comes from the policy graph and can change after boot, so arming is reconciled
  on every policy change (coalesced, off the watch goroutine) rather than resolved once at startup: a portal that
  appears, is retyped, or is removed is armed/re-armed/disarmed without a restart. The controller boots
  **default-deny** (armed for nothing) instead of blocking or crashing when policy is slow/unreachable, and
  converges as policy arrives. The controller binds the policy KV **read-only** (`natsx.KVBucket`); accessd owns
  bucket creation.
- **Audit** (`internal/audit`) — JetStream is the system of record for events; the PocketBase `events`
  collection is a rebuildable projection. Durable consumer, at-least-once (a redelivery may dup a row in v1).

### NATS subjects

Every subject **leads with the app token `acc`**: the access app owns the `acc.>` subtree and a **portal** is a
Thing addressed underneath it as `acc.{location}.{type}.{thing}`, with the verb trailing. `{type}` is the portal
kind (door/turnstile/elevator/gate/logical), a single NATS token. The leading literal `acc` is load-bearing — on
a shared NATS account, a stream subject that *led with a wildcard* (e.g. `*.*.*.acc.evt.>`) overlaps any sibling
stream rooted at a literal first token (`things.>`, `cameras.>`, `kiosk.*.event.>`, …), and JetStream rejects
overlapping stream subjects (err 10065). Leading with `acc` keeps our subject space disjoint from theirs.

- `acc.{location}.{type}.{thing}.tap` — credential presentation (v1: mock reader publishes here)
- `acc.{location}.{type}.{thing}.evt.{kind}` (`tap`/`state`/`alarm`) and `acc.{location}.evt.fire` (location-scoped) — audit events → ACC_EVENTS
- `acc.{location}.{type}.{thing}.cmd.posture` / `.cmd.unlock` — control-plane commands (core NATS, fire-and-forget)

**All subject construction and parsing lives in `internal/subjects`** (one `Subjects` value carrying the `acc`
app token from `subjects.app` config, default `acc`, threaded through every constructor) — never hand-format
subject strings elsewhere. The audit stream captures two patterns of **different fixed arity** — `acc.*.evt.fire`
(4-token fire) and `acc.*.*.*.evt.>` (6+-token portal events) — both rooted at that literal app token, so they
overlap neither each other nor a sibling app's stream, and can't capture a foreign Thing's events. (The fire
pattern is fixed-arity, no trailing `>`, so it can't expand to overlap the portal pattern.) **accessd and every
controller must share the same `subjects.app`** (a mismatch silently severs policy/commands/events).
`docs/protocol.md` is the full wire reference (subjects, message shapes, KV key scheme, decision reason codes).

Commands (`internal/controller/commands.go`) install **runtime posture overrides** that are operational state,
never written back to PocketBase. `posture: "clear"` reverts to the standing value. The controller grows **no
ticker** — `until` (timed reversion) and the standing posture both come from outside; reversion is an external
scheduler publishing a follow-up command. Fire input suppresses alarm emission (hardware owns egress).

### Schema is code

`pbmigrations` defines collections in Go (`1750000000_collections.go`) and seeds an idempotent dev fixture
(`1750000001_fixture.go`, no-ops if `locations` is non-empty). Collections have **nil access rules = superuser-only**,
the right default for a PACS control plane. `migratecmd` Automigrate snapshots dashboard collection edits into
new Go files beside the hand-authored ones — review those before committing.

## Config

Unified `config.Config` (`config/config.go`) for both binaries, loaded via Viper. Every key is overridable by an
**`SA_`-prefixed env var** (`SA_NATS_URLS`, `SA_CONTROLLER_LOCATION`, ...). A missing config file is fine — defaults +
env apply. `accessd` reads `$SA_CONFIG` (default `config/accessd.yaml`); `access-controller` uses its `-config`
flag (default `config/controller.yaml`). Exactly one NATS auth method may be set (creds file / nkey / token /
user-pass); `*.creds` and `config/local*.yaml` are gitignored — never commit credentials.

## Conventions

- Module path `github.com/stone-age-io/access-control`, Go 1.26. Structured logging via `zap` wrapper
  (`internal/logger`); Prometheus metrics (`internal/metrics`) on a side port (accessd `:2115`, controller `:2114`).
- **Fail-safe everywhere**: dangling references, malformed values, not-yet-synced records, parse errors — all
  resolve to deny or "keep previous value," never to a grant or a crash.
- Decision **reason codes** (`policy.go`) are stable strings that flow verbatim into events and the UI — treat
  them as a public contract.

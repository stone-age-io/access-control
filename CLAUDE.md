# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`stone-access` is a NATS-native physical access control (PACS) system built on the
[Stone-Age.io](https://stone-age.io) primitives (NATS core, KV, JetStream, PocketBase).
Two Go binaries plus a Vue 3 management UI:

- **`accessd`** (`cmd/accessd`) — central system of record. Embeds PocketBase (control plane + schema),
  mirrors the policy graph to NATS KV (one key per record), runs the JetStream audit consumer, runs the
  controller-health monitor (heartbeats → `controllers.last_seen`/`status`), runs the **notification
  sink** (`internal/notify`) — a *second*, independent durable on ACC_EVENTS that emails on alarm/fire
  (`DeliverNew`, always on but config-free and inert unless opted in: an alarm source sets
  `portals`/`areas.notify_on_alarm` or `locations.notify_fire`, **and** an operator sets `users.notify` to
  receive it — recipients optionally scoped to locations via `users.notify_locations`, empty = all; SMTP
  transport is PocketBase's own mail settings) — and runs the **entry-disarm sink**
  (`internal/disarm`) — a *third* such durable that, on a valid credential grant at a `disarm_on_grant` portal,
  durably disarms that portal's area (`DeliverNew`, always on but inert unless a portal opts in). Serves the
  embedded UI at `/`.
- **`access-controller`** (`cmd/access-controller`) — edge runtime. Watches the KV keyspace into in-memory
  maps, decides credential presentations **locally** with the pure `policy.Decide`, drives reader/lock/door-input
  hardware, runs a per-door forced/held-open state machine, evaluates **area arm-state** (intrusion-lite: while an
  area is armed, an `intrusion` aux input — or any `tamper_24h` input — or a **forced open on a member portal** —
  raises an `intrusion` alarm), emits access events to JetStream, and publishes a liveness heartbeat.

v1 status: the reader is selectable via `controller.reader` (orthogonal to the lock/door driver) — `nats` (default;
simulated taps published to `acc.{location}.{type}.{thing}.tap`, for dev), `osdp` (a real OSDP reader on the
model's RS485 bus), or `both` (NATS for every portal **plus** OSDP for the portals that have a physical reader).
Under `both` a portal opts into OSDP per-portal via its `reader_address` (`>= 0` = OSDP reader at that PD address,
`-1` = NATS-only); the `internal/controller` `multiReader` composes the two readers behind one `Reader` interface,
dispatching `Arm` by address and fanning both tap streams into one. Each `evt.tap` carries a `source` (`nats`/`osdp`)
so a physical read is distinguishable from a NATS-published tap in the audit trail. The OSDP reader is the controller acting as ACU/CP: `internal/drivers/osdp` is a no-cgo CP
engine (per-PD `INIT→CAPDET→ONLINE→OFFLINE` state machine round-robined over one bus, mirroring libosdp's design)
built on the clear-text packet codec in `internal/drivers/osdp/wire`; OSDP Secure Channel is a deliberate v1
omission (fast-follow). The reader plugs in behind `drivers.ReaderDriver` (a card read becomes a `Tap`), so
`policy.Decide`, the lock-pulse path, and the door state machine are untouched. The **lock and door inputs have
real drivers**: `internal/drivers` holds the interfaces + mocks; the two real backends sit behind a per-model
profile in `internal/drivers/hardware` and are chosen by that profile's `Transport()` — `internal/drivers/gpio`
(no-cgo Linux GPIO char device) for the KinCony Server-Mini (CM4), `internal/drivers/i2c` (no-cgo MCP23017 over
I2C, via periph.io, inputs read by polling) for the Pi5R8 (CM5). A controller picks `mock` (default) or `gpio` via
`controller.driver`; under `gpio` the `model`'s profile decides GPIO-vs-I2C transport, so neither the binary nor
config changes per board. The same `model` profile also carries the RS485 serial port (e.g. `/dev/ttyAMA2`) the
OSDP reader uses, and each portal's `reader_address` is its OSDP PD address on that bus.

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
  reader subscription, the lock, and the DPS/REX inputs via a `PortalHardware` backend (`drivers.MockHardware`,
  the GPIO driver, or the I2C/MCP23017 driver), and disarms the lot when the portal leaves. The controller boots **default-deny** (armed
  for nothing) instead of blocking or crashing when policy is slow/unreachable, and converges as policy arrives.
  It binds the policy KV **read-only** (`natsx.KVBucket`); accessd owns bucket creation.
- **Door monitoring** (`internal/controller/runtime.go`) — a per-door state machine over DPS/REX inputs emits
  `evt.alarm` events (`type`: `forced`/`held`/`held_clear`). A grant or REX opens a short authorized-open window
  (no `forced`) and arms the held-open (DOTL) timer; a location's fire input suppresses all alarm emission. The
  hardware binding (logical relay/input indices, held-open threshold) rides policy on the portal record, never
  the pure `policy.Decide`; the box maps logical indices to physical lines via its `model` profile.
- **AreaManager** (`internal/controller/areamanager.go`) — the arming sibling of AuxManager. The desired set is
  every area with a member `aux_input` **or portal** on this box (`PolicyStore.AreasForController`/`AreaControllers`
  union both kinds); for each it resolves the
  effective arm-state (`ResolveArmState`: `armOverride` → scheduled `autoArm` → standing `arm`, fail-safe to
  disarmed) and writes a **per-controller arm shadow** to ACC_STATUS (`area.{controller}.{areacode}`), stamping
  the full participant set (`peers`) so the console can tell "all armed" from "a box never reported." It reconciles
  on policy change *and* on the runtime's hold-eval tick (so scheduled-arm boundaries refresh — **no new timer**),
  and drops a shadow when an area leaves the box. The arm decision is **not** in `policy.Decide` (it's
  time/schedule-dependent operational state, like posture). Two trip paths share the fire-suppression gate and the
  edge-triggered/no-latch shape of `forced`: an **aux input** trip (`runtime.setAuxInput` → `maybeIntrusionAlarm`,
  by `point_type`), and a **portal** trip (`runtime.maybeForcedIntrusion` at the `forced` site — a member portal's
  unauthorized open while armed; a grant/REX open is normal passage and never trips). Entry-disarm is the inverse,
  and lives **centrally** (durable arm-state, area spans boxes): accessd's `internal/disarm` sink, not the
  controller, writes `armOverride: disarmed` on a credential grant at a `disarm_on_grant` portal.
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

- `acc.{location}.{type}.{thing}.tap` — credential presentation (the `nats` reader subscribes here; the `osdp` reader reads RS485 instead; under `both`, NATS for every portal and RS485 for the reader portals)
- `acc.{location}.{type}.{thing}.evt.{kind}` (`tap`/`state`/`alarm`) and `acc.{location}.evt.fire` (location-scoped) — audit events → ACC_EVENTS. An **area intrusion alarm** reuses this as `acc.{location}.area.{areacode}.evt.alarm` (type token `area`, body `{type:"intrusion",point,ts}`) — captured by the existing 6-token wildcard, no new stream subject.
- `acc.{location}.{type}.{thing}.cmd.posture` / `.cmd.unlock` — control-plane commands (core NATS, fire-and-forget). **There is no `cmd.arm`**: arm/disarm is a *durable record write* (`areas.arm_override` → mirror → KV → controllers converge), so a reboot can't silently disarm.
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
Later migrations add the upward shadow + UI/operator surface: `1750000006` (`point_status`), `1750000007`
(`aux_input`/`aux_output`), `1750000008` (portal `reader_address`), `1750000009` (the operator auth tier +
role-based rules + `audit_logs`), `1750000011` (location-map/floor-plan UI fields), `1750000012`/`1750000013`
(posture `source` + event `source`), `1750000015` (credential `type` rename `nkey`→`generic`, widened to
`generic`/`wiegand`/`pin`/`mobile` — a control-plane label only, never on the policy wire), `1750000016`
(replace the operator `role` rank with the orthogonal `permissions` capabilities), and `1750000017`
(per-install wiring sense: portal `dps_contact`/`rex_contact` NO/NC + `lock_type` strike/maglock + `rex_unlock`,
and `aux_input.contact` — controller-only hints folded onto the board profile's electrical polarity, never on
the `policy.Decide` wire), `1750000018` (shareable holiday calendars), `1750000019` (the **intrusion-lite**
`areas` collection + `aux_input.area`/`point_type` + a `point_status.kind` of `area`), `1750000020`
(`events` ack fields `acknowledged`/`ack_by`/`ack_at`), `1750000021` (a guarded areas demo fixture), and
`1750000022` (portal `area`/`disarm_on_grant` — portals as area members + entry-disarm), and `1750000023`
(notification opt-in: `users.notify` recipient flag + per-source `portals`/`areas.notify_on_alarm` and
`locations.notify_fire` — moves the alarm-email "who"/"which" out of config into UI-managed data), and
`1750000024` (notification recipient scoping: `users.notify_locations` — an operator is paged only for
alarms at locations in its scope; empty = all locations).
The base `1750000000` stays frozen; everything is additive. `migratecmd`
Automigrate snapshots dashboard collection edits into new Go files beside the hand-authored ones — review those
before committing.

### Control-plane access (operators & audit)

Two collection rules govern the *control plane* (who may edit policy), entirely separate from the *data-plane*
decision (`policy.Decide`, which never sees operators). Operators sign in against PocketBase's built-in **`users`**
auth collection (not `_superusers`); a superuser stays the break-glass account that bypasses everything. Ability is
the multi-select **`users.permissions`** — orthogonal capabilities (`enroll`/`policy`/`topology`/`command`/
`operators`), **not** a rank. Read is a universal floor for any authenticated operator; only writes and commands are
gated. Two enforcement points share `permissions`: **collection rules** (migration `1750000016`, the real boundary
— rule form is `@request.auth.permissions ~ "x"`, JSON-LIKE not `?=`, exact only because capability names are
pairwise non-substring) and **`authz.RequireCapability`** on accessd's custom routes (`internal/commandapi`'s
grant/posture/output need `command`; `internal/modelsapi`'s `/api/models` needs any auth). `internal/changelog`
records every API-driven policy edit (+ operator logins) to the `audit_logs` collection via PocketBase `*Request`
hooks — so accessd's own programmatic `app.Save` writes (heartbeats, the events/point_status projections, the KV
mirror) are excluded by construction; rows strip secrets and a daily cron prunes past `accessd.auditRetentionDays`.
The full reference is `docs/operators.md`.

## Config

Unified `config.Config` (`config/config.go`) for both binaries, loaded via Viper. Every key is overridable by an
**`SA_`-prefixed env var** (`SA_NATS_URLS`, `SA_CONTROLLER_LOCATION`, ...). A missing config file is fine — defaults +
env apply. `accessd` reads `$SA_CONFIG` (default `config/accessd.yaml`); `access-controller` uses its `-config`
flag (default `config/controller.yaml`). Exactly one NATS auth method may be set (creds file / nkey / token /
user-pass); `*.creds` and `config/local*.yaml` are gitignored — never commit credentials.

A controller's config is just its identity and hardware selection: `controller.code` (which portals it drives,
matched against the policy graph), `controller.location` (timezone + command/fire subscription scope),
`controller.driver` (`mock`|`gpio`), `controller.model` (required for `gpio`, `reader: osdp`, or `reader: both`;
selects the hardware profile + RS485 serial port), `controller.reader` (`nats`|`osdp`|`both`), and
`controller.heartbeatInterval`. accessd's
`accessd.controllerOfflineAfter` sets how long a silent controller stays "online" before the health sweep marks it
offline. An optional, **off-by-default** read-only diagnostics endpoint (`diagnostics.enabled`/`diagnostics.address`,
controller-only, localhost by default) serves a self-contained local `/status` page (+ `/status.json`) of this box's
live state for field install/troubleshooting — `internal/diag`, strictly read-only (control stays on the command
plane); see `docs/configuration.md`.

## Conventions

- Module path `github.com/stone-age-io/access-control`, Go 1.26. Structured logging via `zap` wrapper
  (`internal/logger`); Prometheus metrics (`internal/metrics`) on a side port (accessd `:2113`, controller `:2114`).
- **Fail-safe everywhere**: dangling references, malformed values, not-yet-synced records, parse errors — all
  resolve to deny or "keep previous value," never to a grant or a crash.
- Decision **reason codes** (`policy.go`) are stable strings that flow verbatim into events and the UI — treat
  them as a public contract.

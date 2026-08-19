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
  durably disarms that portal's area (`DeliverNew`, always on but inert unless a portal opts in) — and runs the
  **one-shot disarm release** (`internal/armrelease`) — a periodic sweep that clears a disarm `arm_override` on a
  scheduled area once its base arm-state (scheduled/standing, override excluded) is disarmed, so scheduled-arm +
  entry-disarm loops without an operator clearing the override. Serves the embedded UI at `/`.
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
The graph mirrors the operator's mental model 1:1: **user → roles → access groups → (targets + one schedule)**,
where a target is a **portal**, an **area** (arm/disarm), or an **aux output** — three independent relations on one
group, because the schedule is the whole reason groups exist and "warehouse staff, Mon–Fri 06:00–18:00" is one
window whichever kind of thing it authorizes (migration `1750000037`).

Each target kind gets its own **pure sibling** of `Decide`, not a widened `Decide` — its evaluation order is a
documented contract on the per-tap hot path, and an area has no posture gate and no strike to pulse:
`DecideArea(p, loc, cred, area, action, atUTC)` and `DecideOutput(p, loc, cred, output, atUTC)`
(`decide_targets.go`). All three share the credential/user ladder (`subjectFor`), so "is this pass usable" has one
implementation rather than three that can drift toward allow. Arm and disarm are **separate rights**
(`AccessGroup.CanArm`/`CanDisarm`, flattened from the wire's `areaRights` by `policy.ArmRights`): disarming turns
intrusion detection off, and "may lock up but not silence the building" is a real role. An empty rights list grants
neither and reports `deny_no_area_right`, distinct from `deny_no_access`, so a group with areas and no rights is
diagnosable rather than silent.

Authorizing an arm is pure; **resolving** an area's arm-state still is not (it is time/schedule/override
state, like posture) — so nothing in `policy` reads an arm-state, and a grant to disarm is a grant whether the area
is currently armed or not. Today only accessd evaluates the two siblings (the badge routes); the controller maps the
fields anyway so an OSDP keypad arming a partition at the reader needs no wire change.

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
  fires `SetOnChange`, which drives the controller's **watch-driven arming**. The KV bucket is bound **lazily**
  inside the watch loop (`NewPolicyStore` takes a `KVBinder`, not a live handle) and the bind is retried there,
  so a controller boots even when NATS is unreachable — it never fatals on a cold, serverless start. An optional
  **offline config cache** (`internal/controller/policycache.go`, opt-in via `policy.cache`) makes that boot
  useful: a write-through local snapshot of the KV keyspace that `LoadCache` replays through the same `apply`
  path on boot, so a reboot with NATS down decides on **last-known config** instead of default-deny. It's
  fail-secure — a missing/corrupt/too-old snapshot (staleness bound `policy.cache.maxAge`) loads nothing — and
  live KV always wins once a sync lands. `StatusWriter` binds its bucket lazily the same way (upward status is
  simply not published while offline). `PolicyStore.SyncStatus` reports `synced`/`cached`/`loading` for the
  `/status` page. See `docs/configuration.md`.
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
  `evt.alarm` events (`type`: `forced`/`held`/`held_clear`/`no_entry`). A grant or REX opens a short
  authorized-open window (no `forced`) and arms the held-open (DOTL) timer. `no_entry` is the inverse of
  `forced` — the grant window closed with no door-open, i.e. "access granted, nobody came through", which
  separates a real entry from a badge test, a stuck strike, or someone who changed their mind. It is
  **exception-only** (a used grant emits nothing, so the happy path adds no volume) and rides the **existing
  hold-eval tick** rather than a per-grant timer: the controller keeps exactly three timers by design, and since
  the grace window and the tick are both 10s it lands 10–20s late — the same sampling trade scheduled-posture
  holds already accept. A portal with no `dpsInput` can never observe an open, so it is gated out entirely or it
  would fire on every grant. The hardware binding (logical relay/input indices, held-open threshold) rides policy
  on the portal record, never the pure `policy.Decide`; the box maps logical indices to physical lines via its
  `model` profile.
- **Fire is an aux input, not a driver interface.** A location's fire-alarm interface is an `aux_input` whose
  `point_type` is `fire`; the controller that owns the contact publishes `acc.{loc}.evt.fire` on **both edges**,
  and every controller at that location (including the publisher, idempotently) applies it. It used to be
  `drivers.FAIInput` with zero implementations in any backend — an FAI is electrically just another dry contact,
  so everything it needed already existed as an aux input (controller binding, logical index, contact sense,
  floor-plan position, a UI, and a driver path that already delivers `InputAux` on both GPIO and I2C). Making it
  data **deleted an interface instead of adding an implementation**. Two traps: the `fire` case hangs off
  `setAuxInput` *above* the rising-edge guard (the clear matters as much as the assert), and suppression is now
  gated on `locations.fai_suppress` — that field was mirrored to KV and read by nobody, so the UI toggle did
  nothing. `held_clear` is exempt from suppression: a clear can't be a false alarm, and dropping it stranded an
  active held-open on the console with no later event to resolve it. **Hardware owns egress** — the fire panel's
  relay drops maglock power directly; software never unlocks for fire.
- **AreaManager** (`internal/controller/areamanager.go`) — the arming sibling of AuxManager. The desired set is
  every area with a member `aux_input` **or portal** on this box (`PolicyStore.AreasForController`/`AreaControllers`
  union both kinds); for each it resolves the
  effective arm-state (`ResolveArmState`: `armOverride` → scheduled `autoArm` → standing `arm`, fail-safe to
  disarmed) and writes a **per-controller arm shadow** to ACC_STATUS (`area.{controller}.{areacode}`), stamping
  the full participant set (`peers`) so the console can tell "all armed" from "a box never reported." It reconciles
  on policy change *and* on the runtime's hold-eval tick (so scheduled-arm boundaries refresh — **no new timer**),
  and drops a shadow when an area leaves the box. That shadow is also where an arm/disarm becomes an **event**:
  all four paths that change arm-state (the operator route, entry-disarm, the one-shot release, and a scheduled
  auto-arm evaluated at the edge) converge on it, so accessd's status projector emits
  `acc.{loc}.area.{code}.evt.state` by comparing old-vs-new on the record it has *already loaded* — one
  comparison covering four features, with no new query, no new timer, and no migration. Scheduled auto-arm
  otherwise leaves no trace anywhere in the system. The tension is real and was taken deliberately: this makes
  **accessd an event emitter**, where the architecture otherwise has the edge emit and accessd project. The
  judgment is that projecting edge-reported state into a central record and emitting an event about it are the
  same act, not a claim of ownership. The rejected alternative — each controller emits on its own resolved-state
  change — keeps the direction pure but puts emission logic on the edge and yields N events for an N-controller
  area with nowhere central to dedupe. Events are emitted **per controller**, matching the shadow's own
  granularity, each naming its own box. The arm decision itself is **not** in `policy.Decide` (it's
  time/schedule-dependent operational state, like posture). Two trip paths share the fire-suppression gate and the
  edge-triggered/no-latch shape of `forced`: an **aux input** trip (`runtime.setAuxInput` → `maybeIntrusionAlarm`,
  by `point_type`), and a **portal** trip (`runtime.maybeForcedIntrusion` at the `forced` site — a member portal's
  unauthorized open while armed; a grant/REX open is normal passage and never trips). Entry-disarm is the inverse,
  and lives **centrally** (durable arm-state, area spans boxes): accessd's `internal/disarm` sink, not the
  controller, writes `armOverride: disarmed` on a credential grant at a `disarm_on_grant` portal. A disarm
  override (manual or entry-disarm) on a *scheduled* area is **one-shot**: accessd's `internal/armrelease` sweep
  clears it once the area's base arm-state (scheduled `autoArm`/standing, override excluded) is disarmed, so
  scheduled auto-arm + entry-disarm loops without an operator clearing it daily (an area with no `autoSchedule`
  stays disarmed until cleared).
- **Audit** (`internal/audit`) — JetStream is the system of record for events; the PocketBase `events`
  collection is a rebuildable projection. Durable consumer, at-least-once made idempotent: each row carries
  its message's JetStream stream sequence (`stream_seq`, unique-indexed), so a redelivery whose row already
  landed is acked and skipped, never duplicated.
  Because the projection is rebuildable, an optional daily 03:00 prune (`RegisterPrune`, gated on
  `accessd.eventRetentionDays`, **off by default = keep forever**) trims `events` older than the retention
  window — draining in bounded batches (it's high-volume, unlike the changelog's single-batch audit-log prune).
  **No new `kind` value has ever been added**: `kind` is a SelectField (a migration + UI filter change) while
  `type` is plain text, so the *pair* `(type, kind)` carries every newer shape — `(area, state)` is an arm
  transition, `(ctrl, state)` a liveness flip, `(door, alarm)` with `type: no_entry` a grant nobody used.
  **`events.source` is a SelectField too, and that is a trap with teeth.** An out-of-range select value fails the
  whole record, so one event body carrying a foreign word means that message never projects — and a durable
  consumer retried permanent failures as eagerly as transient ones, so it was redelivered *immediately and
  forever*: a log file filling from a single event. It happened twice from opposite directions. An emitter added
  values the schema did not have (`command`/`badge` on `cmd.grant`, shipped without a migration; widened by
  `1750000044`), and an unrelated feature reused the key for a different question (the arm-transition event put the
  shadow's `standing|scheduled|override` provenance under `source`; it is `armSource` now). Three defences, because
  the two causes were independent: the column's vocabulary is one thing — how an event *arrived*
  (`nats`/`osdp`/`command`/`badge`) — and arm provenance is not it; the consumer asks the *collection* whether a
  value is accepted before writing it, keeping an unknown one in `payload` rather than losing the row (the schema
  is the authority, so there is no second list to drift); and `retry` backs off (~5 min total) then `Term`s, since
  giving up costs one row until a rebuild while looping costs the log. Adding a value to
  `internal/subjects.Source*` **is a migration plus the UI's `EventSource` union and `SOURCES` filter list**.
- **Notification opt-in has two axes, not one.** Beyond the source×recipient AND, `users.notify_types` selects
  *which kinds* page an operator, because opting in used to mean forced + held + intrusion together — and `held`
  (a propped door in July) is the highest-volume and least urgent of the three, which is how a notification
  system ends up switched off. **Empty means the DEFAULT set, not literally all**: every urgent type, now and in
  future, but not `no_entry`. That direction is deliberate — a new urgent type should reach everyone who never
  narrowed. `held_clear` is never emailed (a clear, not a raise) and a box coming back *online* is not paged.
- **Repage** (`internal/repage`, accessd-side) — re-sends an urgent alarm still unacknowledged after 15 minutes,
  at most twice. It joins two things that already existed and did not talk: the `acknowledged`/`ack_at` fields
  (migration `1750000020`) and the notify sink, which emailed exactly once. It reuses the **same `SendFunc`**, so
  a reminder can never reach anyone the original page could not, and the count lives on the row
  (`events.repage_count`) so the cap survives a restart — an in-memory counter would reset and page forever. A
  projection reader, not a fifth durable: "still unacknowledged N minutes later" is a question about accumulated
  state, not a message arriving.
- **Webhook** (`internal/webhook`, accessd-side) — a **fourth** durable that POSTs each pageable event as
  structured JSON to `accessd.webhookURL`. This is the answer to "let admins edit the email templates": the real
  request is almost never wording, it is getting the event into something that already routes, escalates, and
  acknowledges. A template engine would buy a template language, a preview UI, an escaping story, and a
  "my template broke and nobody got paged" failure mode, and still render worse than PagerDuty. It shares
  `notify`'s classification (so the two never disagree about what is worth forwarding) but deliberately ignores
  the email opt-ins — those decide who gets *mail*; a webhook has one destination whose purpose is the feed.
  JetStream's `Nak` is the retry, which is the whole argument for a durable over a hook. The URL is **config, not
  a record**: accessd POSTs from inside the deployment's network, so deploy-time means there is no API to abuse;
  redirects are never followed and the timeout is hard.
- **Controller health** (`internal/health`, accessd-side) — a core-NATS subscriber to the heartbeat subject
  updates `controllers.last_seen`/`status` with a **direct record update, not an events row**, plus a staleness
  sweep that marks a silent box offline. Heartbeats are deliberately kept out of the audit stream — but a
  **liveness transition is not a heartbeat**: an online↔offline flip is one event per outage, and it is the most
  operationally urgent thing the system notices (that site is now on cached policy, or default-deny), so each
  transition emits `acc.{loc}.ctrl.{code}.evt.state`. Six tokens, so the existing `acc.*.*.*.evt.>` stream
  subject captures it with **no stream change**; the heartbeat stays outside `.evt` at five. Zero new timers —
  the sweep loop already ticks and `sweep()` already *is* the transition branch (it queries only `status =
  'online'`); the online direction needed one comparison, since `markOnline` sets status unconditionally.
- **Three record planes.** The system records in three places and the split is load-bearing: the **event stream**
  (what happened at the building), **`audit_logs`** (who changed the config, via `*Request` hooks), and the
  **status shadow** (what is true right now, last-value). Control-plane edits stay in `audit_logs` — the planes
  are not merged. What was fixed was building-history filed in the wrong plane: arm/disarm and controller
  liveness lived only in the shadow (or, for scheduled auto-arm, nowhere at all), and a badge *denial* wrote only
  an `audit_logs` row while a physical denial wrote an events row — so "who has been probing doors" needed a
  union across two collections. Each now emits an event; the `audit_logs` rows stay, because "an API call
  happened" is a different question from "a door was denied".
- **Badge tier** (`internal/badgeapi`, accessd-side) — the routes a *cardholder or visitor* calls, authenticated
  against the **`cardholders`** collection itself, never `users`. That collection is an **auth collection**: one
  person is one record whether or not they ever sign in, and a person who may sign in is one with `badge_login` set
  (which backs the collection's `AuthRule`). So the two tiers read **one substrate through two lenses** — an operator
  sees the policy graph, a holder sees one badge — rather than one shadowing the other.
  It began as a separate `badge_users` collection with a 1:1 relation back to `cardholders`, and everything
  complicated about that version was the relation, not the feature: a unique index (one login per person), a cascade
  delete (no orphan login that authenticates and resolves nobody), a field guard (no repointing the relation to
  inherit someone's doors), and a delete hook (a visitor's two halves must die together). Collapsing the records
  deleted all four. Three PocketBase facts make it work: collection type is **immutable**, so `cardholders` must be
  *born* as auth (hence the base migration is not frozen any more — a fresh `pb_data` is required);
  `initEmailField` leaves `Required` alone on a supplied field and the unique index it generates is
  `WHERE email != ''`, so **email stays optional** and blanks do not collide (it is unique, so requiring it would
  force a synthetic address onto every contractor and non-person card, and hard-fail a sparse LDAP/CSV import); but
  `initPasswordField` **force-re-enforces Required**, so `bindPasswordFill` supplies a random password on create —
  otherwise the ordinary cardholder form, which has no password box, could not save anyone.
  `GET /api/badge/me` returns their own badge (photo, QR payload, and everything the badge grants — **names only**,
  never codes or hardware fields); `POST /api/badge/unlock/{portalId}` is a **remote unlock authorized by
  `policy.Decide`**, run over a live `policysnapshot` of ACC_POLICY (a short TTL cache, since `SnapshotKV` drains the
  whole keyspace and this is a button a visitor can tap). So remote unlock can never exceed what that badge opens in
  person, and it emits the **existing `cmd.grant`** with `actor: badge:<cardholderId>` — no new subject, no edge
  change. A per-door `portals.allow_remote_unlock` (default false, not mirrored) gates it, because "may walk through"
  and "may open from anywhere with no presence proof" are different permissions. **A badge action never publishes
  `.tap`** — `Tap.Source` exists so a physical read stays distinguishable. Every attempt writes an `audit_logs` row,
  including denials: a deny never reaches the controller, so without it a holder could probe doors leaving no trace.
  The **non-door actions** are the same shape (`areas.go`): `POST /api/badge/areas/{id}/arm` and `/disarm` authorize
  with `policy.DecideArea` and then write `areas.arm_override` — a *durable record write*, exactly as
  `internal/commandapi` does and for the same reason (a reboot must not silently disarm; there is no `cmd.arm`
  subject) — while `POST /api/badge/outputs/{id}/pulse` authorizes with `policy.DecideOutput` and publishes the
  existing `cmd.output`. Each has its own default-false opt-in (`areas.allow_remote_arm`,
  `aux_output.allow_remote`). Two things a holder deliberately **cannot** do: clear an arm override (that is an
  operator's "revert to the schedule"; `internal/armrelease` already releases a one-shot disarm on a scheduled area,
  so a holder's disarm strands nothing), and latch an output on (the route drives `pulse` only — a momentary act is
  self-limiting, and this is a choice about the *surface*, not a claim that on/off/pulse are separately authorized).
  `GET /api/badge/live` places the holder's own portals and outputs on a site's floor plan, gated by
  `locations.badge_floorplan` (default false): the pins are badge-scoped but the plan is the whole building, so a
  contractor with one door would otherwise learn the layout. It is a **server projection, not widened rules** —
  PocketBase rules are row-level, so any rule letting a holder read their own portal row would also hand them
  `lock_relay`/`reader_address`/the policy code, and "portal in a group in a role I hold" is a deep back-relation
  filter. Areas never appear on the plan (only portals and aux I/O carry a `floorplan_position`; an area is a set with
  no single place to pin), and there is no live state or realtime — the badge polls `/me` for the one piece of state a
  holder can act on (and that state is policy *intent* from `armStateFor`, not a hardware report; nothing in the badge
  tier reads `point_status`).
  The badge's screens are an **adaptive** set — badge face / plan / portals / areas / controls / on-site — reached from
  **one flat bottom navigation bar** (`BadgeView`), over one derivation (`badgeNav.ts`): a screen exists only if the
  holder has something in it, so the common badge is Badge + Plan, or Badge + Portals. A site has hundreds of points
  and an operator is hunting; a holder typically has a handful of doors and no areas or controls, so fixed segments
  would be mostly-empty chrome over a three-item list. "On site" sits beside the three kinds because the split a holder
  cares about is press-a-button vs walk-to, and past a few doors that list wants grouping by building
  (`BadgeOnSiteList`) which the action lists do not. Showing one view at a time is also why the action lists no longer
  bound their own height: the nested scroll existed because four cards were stacked.
  The bar is **one level where there used to be two**, and that is the fix rather than a restyle. It was Badge/Access
  tabs pinned under the header *plus* a row of segment pills inside the Access tab — three rows of chrome above the
  content on a phone, and the pills **wrapped to a second line** at four segments, so the badge with the most on it
  got the least room to show it. Equal `flex-1` columns cannot wrap by construction: six items at 375px are ~62px
  each, which is what a native tab bar does with five, and no label needs a second line (`min-h-14` + an 11px label
  is the smallest that holds that while clearing the 44px touch floor). It also moves navigation into the thumb zone
  and hands the ~90px the two rows cost back to the content, which is what the floor plan wanted. The flattening is
  honest about the hierarchy too: from the holder's side these were never two levels — "my photo and QR" and "my
  doors" are peers, different screens of one badge. A count rides the **icon**, not the label, because "Portals 3"
  wraps at 62px.
  The derivation moving to `badgeNav.ts` is what turned `BadgeAccessPanel` into a **pure renderer** told which view to
  draw. Two hosts render the same views over the same payload with different chrome — the holder's bottom bar and the
  operator preview's dialog tabs (`BadgePreviewModal`) — and they must never disagree about *content*, since a segment
  the holder sees and the operator does not is exactly the discrepancy the preview exists to rule out. Same reason
  `remoteGrants`/`onSiteGrants` live there: a segment's count and the rows under it come from one filter.
  Selection is remembered two ways because they answer different questions — the `?tab=` query param so a screen can
  be bookmarked and linked, `localStorage` so opening the badge lands where the holder left it. `?tab=access` from the
  two-level version still resolves (to the first access segment); a stored key that no longer exists resolves to the
  face, so a vanished segment needs no migration.
  The **plan segment shows one site**, picked from a `<select>` when `/api/badge/live` returns several (that route
  always returned every opted-in site; the panel used to stack them, which is not wrong so much as invisible — each
  plan is a full-width image, so on a phone the second card's header starts below the fold with nothing to suggest
  it exists, and the tab said "Plan" whether there was one or four). The same adaptive rule applies: no picker at
  one site, and the Plan item carries a count of **sites** only past one. A native select rather than a row of pills,
  which read as a second switcher and cost the picture a line of chrome per site. It mirrors the
  operator console's `/monitor` overview → `/monitor/:locationId` drill-in, and keying `BadgeFloorplan` by site id
  settles the stacked version's other bug for free — each instance owns its selection, so two plans on screen could
  each caption a selected door, and the one scrolled off the top kept stale result text.
  **The plan fills the frame instead of being sized by its image**, which is why the shell hands the Access panel a
  *definite* height (`BadgeView`'s one scroll region → `h-full` wrapper → panel as a flex item) and the plan card is a
  flex column whose plan area takes what is left. A list longer than the frame still overflows into that same scroll
  region; the change is only that a view can know how much room it has.
  Two things had to be true for that to actually happen, and only one of them was at first. `max-h-full`/`max-w-full`
  on the image is **half a fit**: a cap shrinks an oversized plan and does nothing at all for one *smaller* than the
  space, which then sits at natural size with the screen empty around it — and a plan exported at 800px on a tall phone
  is the ordinary case, not a corner one. So the image is `h-full w-full object-contain`, which scales both ways. (An
  earlier note here said `object-contain` was the wrong tool because it hides the fitted result; that was reasoning
  about *measurement*, and it is wrong about *fitting* — the fit is the part CSS must do, and the result is arithmetic,
  see below.)
  The other was a **DaisyUI rule with real teeth**: `.card-body :where(p){flex-grow:1}`, which makes a bare `<p>` that
  is a direct child of a card-body flex column a *growing* flex item. The plan's "tap a marker" caption was one, so it
  split the card's free space evenly with the plan area — the plan filled about half the card and a blank band sat
  under the caption, which read as "flex-1 isn't working" and is why this took a measured harness to find rather than a
  reading. `shrink-0` does not help (the item was growing, not shrinking) and a `grow-0` utility only wins on source
  order, so the caption is wrapped in a `<div>`: the paragraph is out of the flex line, no specificity argument, and a
  sentence stays a `<p>`. **Any direct-child `<p>` of a `card-body` that is a flex column has this bug.**
  Pins are placed against the **drawn rectangle, computed** — not the element box, and not percentages. Under
  `object-contain` the element fills the area and the picture is letterboxed inside it, so `BadgeFloorplan` repeats the
  browser's own fit (`scale = min(boxW/naturalW, boxH/naturalH)`, centred) from `offsetLeft`/`Top`/`offsetWidth`/
  `Height` relative to the `relative` plan area. Two earlier versions were wrong in opposite directions — percentages
  of the wrapper (right only while the wrapper was sized BY the image) and then measuring the element box (right only
  while the element WAS the image) — and both landed pins in the letterboxing. The `ResizeObserver` watches the **plan
  area, not the image**: selecting a pin adds the action bar, which shortens the area, and on a width-limited plan the
  drawn width does not change at all — only where it is centred, which observing the image would miss while leaving
  every pin shifted.
  The **badge face** (`BadgePassPanel`) is a centred column, not a list row: the photo sits above the name at 112px
  (a badge is held up to be compared against a face, so it is the largest thing after the code itself — the old 64px
  thumbnail beside the name was sized like a list avatar), the name below it at `text-lg`, and the QR at 208px. The
  card is centred in the leftover height with `my-auto` rather than `justify-center` on the column, because auto
  margins collapse to nothing when the content is taller than the frame — a short phone then scrolls, where flex
  centring would have clipped the top of the photo unreachably.
  The portal segment is labelled **Portals**, not "Doors" — what the rest of the system calls them, and a portal is
  not always a door (a holder whose badge opens the vehicle gate should not read it as one). It has a filter past
  six entries, on the same terms as `BadgeOnSiteList`'s, sharing one `BadgeFilterInput` so two search boxes a
  segment apart cannot drift. It stayed a **filter rather than a location picker**: a picker left set to one
  building keeps the other buildings' doors off the screen for as long as it stays set, and this is the surface
  where a hidden door is a holder standing outside one. It is also deliberately **not persisted**, unlike the
  segment and the plan's site — coming back to a badge showing three of your twelve doors, because of something
  typed last week, is how a holder concludes their access was revoked.
  `GET /api/badge/preview/{id}` (`enroll`, operator-only, `preview.go`) is the **operator's** read of a holder's badge,
  for "my pass doesn't work" — it returns the holder's own `/me` and `/live` payloads (the same `buildMe`/`buildLive`
  builders serve both, so a preview that differs from their screen is a bug) plus the three facts a badge cannot show
  about itself (`badgeLogin`/`passwordSet`/`status`), and the console renders it with the badge's own Vue components.
  It **mints nothing**: `NewStaticAuthToken` would have let an operator press the holder's buttons, but a badge action
  stamps the *cardholder* as `actor_id`, so a borrowed badge session would write rows indistinguishable from the
  holder's own — "did this visitor open the loading bay, or someone checking on them?" must stay answerable from the
  log. The trade is that it cannot prove an unlock works end-to-end, only what the server would decide; an operator
  who needs the door opened uses the command routes under their own `command`. Every preview writes an `audit_logs`
  row, because reading a badge is reading someone's photo, QR payload, and every door they hold.
  One human can hold an account in both tiers, so `cardholders.operator` (migration `1750000040`) points at the
  `users` record of the same person and `GET /api/badge/me` — **alone among the holder routes** — accepts an operator
  token, resolving through it (`subjectCardholder`). Everything that *actuates* names `cardholders` only: an operator opening
  a door uses the command routes with their `command` capability, where it is audited as an operator action, rather
  than through a second path that would make the audit trail ambiguous about which authority they used. The pointer
  lives on `cardholders` because `users.UpdateRule` is self — a `users.cardholder` field would be self-writable, so
  any operator could repoint it and inherit that badge's doors, which is exactly the escalation the badge tier's write
  rules removed.
  `POST /api/badge/password` lets a holder set/change their own password; `password_set` decides whether the current
  one must be proved — an OTP-signed-in holder is setting a *first* password and has nothing to prove, while
  demanding no proof from one who has a password would let a stolen session lock them out. It cannot go through
  PocketBase's own record update, which always demands `oldPassword` from a non-manager. There is deliberately **no**
  route for "give this cardholder a login": `badge_login` is a field, so it is one checkbox on the cardholder form
  and an ordinary PATCH the collection rules already govern. The old `/api/badge/holders` existed only to centralise
  the `kind` discriminator, the throwaway password, and `password_set` — all three moved into the schema and
  `RegisterGuards`. What remains is `POST /api/badge/invite/{id}` (`enroll`), because emailing someone is not a
  field write, and enabling a login is genuinely separate from telling them about it (a 500-person rollout flips the
  flag by import and mails later; an operator handing a password over at a desk wants no mail at all). An optional
  operator-set initial password is what makes the tier usable **with no SMTP at all** (OTP and password-reset are
  both emails); it is handed over in person and never mailed.
  `POST /api/badge/visitors` (`enroll`) mints a visitor in one transaction: the cardholder (`kind: visitor`,
  `badge_login`) plus a time-bound credential whose value comes from `crypto/rand` as uppercase base32 (QR
  alphanumeric mode, and inside the KV key charset). A repeat visitor is **reused**, not duplicated — email is
  uniquely indexed, and the same person visiting twice is the same person; that reuse path is also how a visit is
  **extended** (the UI's "Reissue"), because a visitor's QR carries the credential *value*, so pushing an existing
  one's `valid_until` out would silently re-arm every screenshot of it. An optional `password` on the request is the
  same never-mailed operator-set initial password the enrollment path has (`setInitialPassword`, which is a **no-op**
  when blank — that is what lets `bindPasswordFill` supply the random value on create and leaves a returning
  visitor's existing password intact); without it a mint on an SMTP-less install produced a pass its holder could
  never see, since OTP and password-reset are both emails.
  `POST /api/badge/visitors/{id}/revoke` ends a visit: it revokes the credentials and **keeps** the person, so the
  visit stays on the record and a returning visitor is still recognised. Delete is the retention decision, and it
  now works — `credentials.user` cascades (`1750000036`), so removing a person removes their cards, through the
  ordinary delete path so the mirror prunes the `cred.{value}` keys.
  On the **console side** only the mint is a separate page (`/visitors/new`): a visitor is a `cardholders` row, so
  the visitor list and detail views were folded into the cardholder ones and the old routes redirect. The list
  filters `kind` server-side (Permanent / Visitors / All, default Permanent, remembered in `localStorage`), the
  detail page grows the pass state + Reissue/Revoke when `kind = "visitor"`, and `utils/visitorPass.ts` is the one
  derivation both read so they cannot describe a visit two ways. Two things stay asymmetric on purpose: **search
  always spans every kind** whatever the filter shows, reporting how many matches it is hiding (two pages meant a
  roster search for a guest returned nothing, with no hint the other page existed — that bug is the reason for the
  merge), and the **pass-state chips exist only under the Visitors filter**, because a state comes from the newest
  of a person's credentials in another collection and so narrows the *loaded page*, not the query — honest over 50
  `-created` visits, a lie on a name-sorted roster of thousands. The mint cannot merge into the cardholder form:
  the credential value must come from the server's CSPRNG, and person + credential are one transaction.
  `GET /api/badge/me` reports a single `passState` (`valid`/`expired`/`not_yet_valid`/`none`/`suspended`) rather
  than an `expired` boolean, because that boolean was computed as "no in-window credential" and so told a cardholder
  who had never been issued one that their pass was "not currently valid"; a suspended cardholder gets no QR at all,
  since their screen must not look like a working badge.
  The QR fork is the security-relevant decision: a *visitor* pass carries the credential value (it must work at a
  scanner, and lives hours), while every other badge carries the cardholder id — an identifier that opens nothing,
  because a staff badge hangs on a lanyard for years and gets photographed incidentally.
  `RegisterGuards` (registered at startup, not in `OnServe`, since it guards the *collection* API) binds the three
  invariants that keep an auth `cardholders` coherent: `bindPasswordFill` (a random password + `emailVisibility`,
  the latter because PocketBase strips `email` from an auth record's response for any requester who is not the
  owner, a superuser, or a `ManageRule` match — so operators would see a blank email column);
  `bindLoginRequiresEmail` (email is the sole identity field and the only route an OTP can arrive by, so a login
  without one is unusable by every method); and `bindOTPPasswordPreservation`, which stops PocketBase's own
  pre-hijacking defence — on a first OTP it marks the record verified and, with MFA off, **randomises the
  password** — from destroying the operator-set password the SMTP-less install depends on. That defence does not
  apply to an `enroll`-gated collection: nobody but an operator can create a record, so there is no
  attacker-authored one to disarm. There is **no field-level guard** any more: `cardholders` writes are
  `enroll`-gated with **no self clause**, so a holder cannot PATCH their own row at all — the escalation surface is
  gone rather than guarded, which matters more here than it did, since that row is also the policy graph's entry
  point (`roles`) and its `status`.
- **Visitor sweep** (`internal/badgesweep`, accessd-side) — marks an expired *visitor* credential `revoked`. This is
  **hygiene, not enforcement**: `policy.Decide` already enforces `valid_from`/`valid_until` at the edge, offline
  included. It buys truth in the control plane (an `active` pass really is active) and stops a stale value being
  resurrected by a date edit. It deliberately does **not** delete anyone — retention is the install's policy to set,
  not a background job's to invent — and it leaves expired *staff* credentials alone, since an operator may be about
  to extend one. It finds its scope with one query now (`cardholders where kind = 'visitor'`) rather than
  enumerating logins and dereferencing each one's `cardholder`.

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
alarms at locations in its scope; empty = all locations), `1750000025` (events `stream_seq` unique index for
idempotent audit projection), `1750000026` (`aux_input`/`aux_output` `floorplan_position` — UI-only, so aux
I/O can be placed and monitored on the floor plan like portals, never mirrored to KV), and then the **badge
tier**: `1750000027` (scope the control-plane READ floor from `@request.auth.id != ""` to
`@request.auth.collectionName = "users"`, plus `id = @request.auth.id` on `cardholders` so a holder reads their own
row — the clause a PROTECTED photo download is checked against — the old rule was auth-collection-agnostic, so a second auth tier
would have inherited operator read on the whole graph including `credentials.value` in plaintext),
`1750000028` (`credentials.value` charset `Pattern` from `policykv.CredentialValuePattern` — a value outside the
NATS KV key charset used to save fine and then silently never mirror), `1750000029` (`cardholders.photo`, a
**protected** file so its URL is not public to anyone holding the link), `1750000031`
(`portals.allow_remote_unlock`, default false, control-plane only), `1750000032` (default rate limits for the badge
routes and the `cardholders` auth endpoints), `1750000033` (`roles.visitor_preset` — the curated presets the visitor
mint flow offers; on *roles* because the graph is cardholder → roles → groups, so a role is the assignable unit), and
`1750000036` (`credentials.user` **cascades** — before it, PocketBase refused to delete the target of a required
relation, so deleting a cardholder failed outright for anyone ever issued a card; and a credential outliving its
holder is a key that opens doors and resolves to nobody. The cascade calls `app.Delete` per record, so the mirror
prunes the `cred.{value}` keys and it cannot leave a working key in KV). Then the **widened access group**:
`1750000037` (`access_groups.areas`/`aux_outputs`/`area_rights` — a group grants areas and aux outputs alongside
portals, under the one schedule that is the whole reason groups exist; `area_rights` is a two-value multi-select
because arming and disarming are different rights, and an EMPTY list grants neither), `1750000038` (a guarded
fixture granting the demo area + a new `lobby-gate` output through `lobby-group`), `1750000039`
(`areas.allow_remote_arm` + `aux_output.allow_remote`, both default false, control-plane only — the same
"may act here" ≠ "may act from anywhere" split as `allow_remote_unlock`, plus rate limits for the new routes),
`1750000040` (`cardholders.operator`, an optional uniquely-indexed pointer to the `users` record of the same
human, so an operator can view their OWN badge — on *cardholders* because `users` is self-writable, so the
mirror-image field would let any operator repoint it and inherit someone else's badge), and `1750000041`
(`locations.badge_floorplan`, default false — a badge may show a site's floor plan with the holder's own doors
pinned on it; the pins are scoped to one badge but the plan is the whole building). Then the **notification and
event-plane** pass: `1750000042` (`users.notify_types` — which kinds page an operator, where EMPTY means the
*default set* rather than literally all, so a future urgent type reaches everyone who never narrowed while
`no_entry` stays opt-in; plus `controllers.notify_offline`, the per-source opt-in completing the two-sided AND
for the liveness transitions accessd now emits; plus `events.repage_count`, which lives on the row so the
reminder cap survives a restart — an in-memory counter would reset and page forever), and `1750000043`
(`aux_input.point_type` gains `fire`, which is what finally gives the fire input a *source*: it had a consumer
and a transport but nothing produced the signal, since `drivers.FAIInput` had zero implementations), and
`1750000044` (`events.source` gains `command`/`badge` — the two values `cmd.grant` already carried and the select
rejected, which was not a missing column value but an unbounded audit-redelivery loop; see the Audit section).

**The base `1750000000` is NOT frozen any more, and there are gaps at 30/34/35.** The badge tier used to be a second
auth collection (`badge_users`, migrations `1750000030`/`1750000034`/`1750000035`); it was collapsed into
`cardholders`, which PocketBase can only express by making that collection auth **at creation** — collection type is
immutable (`core/collection_validate.go`, "Collection type cannot be changed"). So the base migration changed and
the three `badge_users` migrations were deleted rather than superseded. Pre-production, and the trade was
deliberate: the alternative was a delete-and-recopy migration leaving the base migration lying about what
`cardholders` is. **A fresh `pb_data` is required** — an existing dev database will not migrate onto this schema.
Everything after the base stays additive.
`migratecmd` Automigrate snapshots dashboard collection edits into new Go files beside the hand-authored ones —
review those before committing.

### Control-plane access (operators & audit)

Two collection rules govern the *control plane* (who may edit policy), entirely separate from the *data-plane*
decision (`policy.Decide`, which never sees operators). Operators sign in against PocketBase's built-in **`users`**
auth collection (not `_superusers`); a superuser stays the break-glass account that bypasses everything. Ability is
the multi-select **`users.permissions`** — orthogonal capabilities (`enroll`/`policy`/`topology`/`command`/
`operators`), **not** a rank. Read is a universal floor for any authenticated operator — where "operator" means a
record in `users` *specifically* (`@request.auth.collectionName = "users"`, migration `1750000027`), never "any
authenticated request", so the badge tier below cannot inherit it; only writes and commands are
gated. Two enforcement points share `permissions`: **collection rules** (migration `1750000016`, the real boundary
— rule form is `@request.auth.permissions ~ "x"`, JSON-LIKE not `?=`, exact only because capability names are
pairwise non-substring) and **`authz.RequireCapability`** on accessd's custom routes (`internal/commandapi`'s
grant/posture/output need `command`; `internal/modelsapi`'s `/api/models` and `internal/simulateapi`'s
`/api/simulate` need only operator auth). Every operator route binds **`authz.RequireOperatorAuth()`**, never bare
`apis.RequireAuth()`: bare RequireAuth admits *any* auth collection (`/api/simulate` is a decision oracle over the
whole graph, so that would be worse than exposing the collections), while `apis.RequireAuth("users")` alone would
lock out the break-glass superuser — PocketBase's check is plain collection-name membership with no superuser
exemption. `RequireOperatorAuth` names both. `internal/changelog`
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

An optional operator **branding overlay** (`branding.dir`, accessd-only, env `SA_BRANDING_DIR`, empty by default)
lets an install override the UI's app name, logo, and DaisyUI theme **without rebuilding**: accessd serves that host
directory's `theme.css`/`logo.svg`/`branding.json` under `/branding/*` (the route is always registered and returns a
silent empty `theme.css`/`{}` `branding.json` when unconfigured, so a stock install never 404s). The UI's `index.html`
`<link>`s `/branding/theme.css` and `stores/branding.ts` fetches `branding.json` pre-mount; `BrandLogo.vue` prefers the
overlay logo, else the built-in inline mark. `branding.example/` is the committed template. Mirrors the sibling
`platform`/`helpdesk` apps' branding system.

The UI is an installable **PWA**, copied from the sibling `platform` app so the three consoles install the same way:
`ui/public/` holds `manifest.json` (standalone, `start_url: /`, the shared Stone Age mark at 192/512) and `sw.js`,
Vite copies them to the embed root, and `main.ts` registers the worker **in PROD only** (in dev a registration
outlives a Vite restart and serves a stale bundle). The service worker **caches nothing** — and here that is a
safety property, not just platform's simplicity one: this app's job is to say what a badge opens *right now*, so a
cached `/api/badge/me` would show a revoked pass as live. Offline resilience belongs at the edge, where
`internal/controller`'s policy cache is designed to be stale safely. It exists solely to satisfy Chrome's install
criteria. Two deliberate deviations from platform: the viewport keeps pinch-zoom (platform sets
`user-scalable=no`; WCAG 1.4.4, and `touch-action: manipulation` in `main.css` already removes the tap delay that
motivates disabling it), and `rel="icon"` points at `icon-192.png` rather than platform's zero-byte `favicon.ico`.
`start_url: /` is right for both tiers rather than a compromise: the sign-in page remembers the last tier per
browser, so a holder's installed app lands on the badge form and an operator's on theirs.

**Touch targets are 44px minimum on anything a badge holder taps.** `main.css` lifts `btn-xs`/`btn-sm` to a
`min-height` of 2.75rem, but **only below 1023px and only min-height** — so `btn-square btn-sm` is a trap: DaisyUI
emits `.btn-sm{height:2rem}` *after* `.btn-square{height:3rem}`, making that combination 48 wide by 32 tall on a
laptop and 48×44 on a phone. Badge chrome therefore uses unmodified `.btn .btn-square` (48px at every breakpoint),
adds `min-h-11` to dropdown menu rows (DaisyUI's `menu-sm` pads them to ~28px, and one of them signs you out), and
`min-h-14` to the bottom navigation bar's columns (DaisyUI's `.tab` is 2rem, fine in a dense console and short for a
phone's primary navigation — which is one reason that bar is plain buttons in a flex row rather than `tabs`).
`BadgeFloorplan`'s pins are the case where this trap actually bit: as `btn btn-circle btn-xs` they came out 48×44 on
a phone and 48×24 on a laptop — an *ellipse* far larger than the visible dot, centred on the pin, so neighbouring
pins' boxes overlapped and a tap on one door could resolve to the next one's button. They are a plain 44px square
button wrapping a `pointer-events-none` 28px dot now, not a DaisyUI `btn` whose size classes fight each other. The
fix that mattered more was making a marker **select only**: acting moved to a full-width labelled button under the
plan (the control the Doors view already uses), so an overlapping hit box costs a wrong *name* on screen — visible
and correctable — instead of a wrong door. The old two-taps-on-the-marker protocol also made the tap count depend on
which pin was last selected, so a tap could appear to do nothing.

## Conventions

- Module path `github.com/stone-age-io/access-control`, Go 1.26. Structured logging via `zap` wrapper
  (`internal/logger`); Prometheus metrics (`internal/metrics`) on a side port (accessd `:2113`, controller `:2114`).
- **Fail-safe everywhere**: dangling references, malformed values, not-yet-synced records, parse errors — all
  resolve to deny or "keep previous value," never to a grant or a crash.
- Decision **reason codes** (`policy.go`) are stable strings that flow verbatim into events and the UI — treat
  them as a public contract.

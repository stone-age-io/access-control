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
- **Controller health** (`internal/health`, accessd-side) — a core-NATS subscriber to the heartbeat subject
  updates `controllers.last_seen`/`status` with a **direct record update, not an events row**, plus a staleness
  sweep that marks a silent box offline. Heartbeats are deliberately kept out of the audit stream.
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
  holder can act on.
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
pinned on it; the pins are scoped to one badge but the plan is the whole building).

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
`h-11` to the badge tabs (`.tab` is 2rem — fine in a dense console, short for a phone's primary navigation).

## Conventions

- Module path `github.com/stone-age-io/access-control`, Go 1.26. Structured logging via `zap` wrapper
  (`internal/logger`); Prometheus metrics (`internal/metrics`) on a side port (accessd `:2113`, controller `:2114`).
- **Fail-safe everywhere**: dangling references, malformed values, not-yet-synced records, parse errors — all
  resolve to deny or "keep previous value," never to a grant or a crash.
- Decision **reason codes** (`policy.go`) are stable strings that flow verbatim into events and the UI — treat
  them as a public contract.

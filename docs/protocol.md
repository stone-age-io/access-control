# stone-access protocol reference

The wire contract between the central app (`accessd`) and the edge controllers
(`access-controller`), carried entirely over NATS. There are two planes:

- **Policy plane** — `accessd` mirrors the PocketBase policy graph into a NATS KV
  bucket (`ACC_POLICY`), one key per record; controllers watch it into memory.
- **Event/command plane** — controllers decide locally and publish access events
  to JetStream (`ACC_EVENTS`); operators issue commands over core NATS.

Subjects, KV keys, and message shapes below are the source of truth shared by
both binaries. Subject construction and parsing live in one place,
[`internal/subjects`](../internal/subjects/subjects.go); the KV value shapes live
in [`internal/policykv`](../internal/policykv/wire.go).

## Subject namespace

Every subject **leads with the app token `acc`**: the access app owns the
`acc.>` subtree, and a **portal** (a controllable opening or logical access
target) is a Thing addressed underneath it as `acc.{location}.{type}.{thing}`,
with the verb trailing (`.tap`, `.cmd.*`, `.evt.*`). Leading with a literal app
token is what keeps `ACC_EVENTS`'s subjects disjoint from every sibling app's
stream on a shared NATS account — JetStream forbids overlapping stream subjects,
and a subject that *led with a wildcard* (e.g. `*.*.*.acc.evt.>`) would intersect
any stream rooted at a literal first token (`things.>`, `cameras.>`,
`kiosk.*.event.>`, …). The `acc` token is set by `subjects.app` in config (or
`SA_SUBJECTS_APP`), default `acc`. `accessd` and all controllers **must use the
same app token** — they publish and subscribe to each other's traffic, so a
mismatch silently severs policy/commands/events. Change it only to isolate a
deployment on a shared NATS account; it must be a single NATS token (no `.`, `*`,
`>`, or whitespace).

`{location}`, `{type}`, and `{thing}` are each a single NATS token and the record
**codes** (e.g. `hq`, `door`, `lobby-main`), never PocketBase ids. The mirror
rejects a location/portal code or portal type that is not a single token or that
collides with a reserved keyword (`acc`/`evt`/`cmd`/`tap`/`fire`).

## Subjects

| Subject | Dir* | Transport | Body |
|---|---|---|---|
| `acc.{location}.{type}.{thing}.tap` | → ctrl | core NATS | `{"cred":"..."}` or a bare credential string |
| `acc.{location}.{type}.{thing}.cmd.posture` | → ctrl | core NATS | `{"posture":"…","actor":"…","reason":"…","until":"…"}` |
| `acc.{location}.{type}.{thing}.cmd.unlock` | → ctrl | core NATS | `{"seconds":N,"actor":"…","reason":"…"}` |
| `acc.{location}.evt.fire` | → ctrl | core NATS | `{"active":bool}` |
| `acc.{location}.{type}.{thing}.evt.tap` | ctrl → | core NATS → JetStream | `{"cred","user","allow","reason","ts"}` |
| `acc.{location}.{type}.{thing}.evt.state` | ctrl → | core NATS → JetStream | `{"posture","actor?","reason?","ts"}` |
| `acc.{location}.{type}.{thing}.evt.alarm` | ctrl → | core NATS → JetStream | `{"type","ts"}` |
| `acc.{location}.ctrl.{code}.heartbeat` | ctrl → accessd | core NATS (**not** JetStream) | `{"code","location","ts"}` |

\* → ctrl = controller subscribes; ctrl → = controller publishes.

Controllers subscribe per location with wildcards: taps via
`acc.{location}.*.*.tap` and commands via `acc.{location}.*.*.cmd.posture` /
`acc.{location}.*.*.cmd.unlock`. The audit surface is the `acc.*.…evt` subtree,
captured by `ACC_EVENTS` and projected into the `events` collection via **two
stream subjects of different fixed arity** (JetStream forbids overlapping subjects,
so the short one must not use a trailing `>`):

- `acc.*.evt.fire` — the 4-token location-scoped fire (`acc.{location}.evt.fire`)
- `acc.*.*.*.evt.>` — the 6+-token portal events (`acc.{location}.{type}.{thing}.evt.{kind}`)

A 4-token subject can never match the ≥6-token portal pattern, so the two are
disjoint with each other; both lead with the literal `acc`, so the set overlaps
no sibling stream rooted at another literal (`things.>`, `cameras.>`,
`kiosk.*.event.>`, …) on a shared account. All bodies are JSON; `ts` is RFC 3339
UTC.

A **controller** is addressed under the reserved `acc.{location}.ctrl.{code}`
namespace (`ctrl` is not a portal type). Its **heartbeat** sits deliberately
*outside* the `.evt` subtree — a 5-token subject with no `evt`, so it matches
neither audit pattern and is **never captured by `ACC_EVENTS`**. accessd
subscribes to `acc.*.ctrl.*.heartbeat` over core NATS and writes the controller's
`last_seen`/`status` directly on the `controllers` record (not an `events` row,
which would flood the audit log). A controller publishes one heartbeat on start
and then every `controller.heartbeatInterval` (default 15s); accessd marks a
controller `offline` once it has been silent longer than
`accessd.controllerOfflineAfter` (default 45s). The publish ticker is one of the
controller's three deliberate exceptions to its no-ticker rule (the others are the
held-open DOTL timer and the scheduled-posture hold-eval ticker, below).

### Command details

- **posture** — installs a runtime posture override for the portal. Valid values:
  `secure`, `free_access`, `unlocked`, `lockdown`, `disabled`, or `clear` (reverts
  to the *effective* posture from policy — scheduled posture if its window is open,
  else the standing posture). Overrides are operational state on the controller and
  are **never written back to PocketBase**. `until` is parsed but **deliberately
  ignored** — timed reversion must come from an external scheduler publishing a
  follow-up command. `free_access` opens on any tap without consulting the
  credential (strike pulses, door stays closed); `unlocked` holds the strike open.
- **unlock** — a momentary strike pulse, distinct from a standing posture change.
  `seconds <= 0` (or omitted) falls back to the portal's configured `pulseSeconds`.
- **fire** — toggles a location's fire-alarm-input state. While active, the
  controller **suppresses alarm emission** for that location (forced/held-open
  events would be false alarms during evacuation). It never changes posture and
  never unlocks — hardware owns egress. It is location-scoped (not per-portal) and
  lives on the `evt` namespace, not `cmd`: it is both a control input the
  controller subscribes to *and* an audited event the stream captures
  (`kind="fire"`).

### Door monitoring & alarms

The controller runs a per-portal door-state machine fed by two digital inputs —
a **door-position switch** (DPS, `dpsInput`) and an optional **request-to-exit**
(REX, `rexInput`). It emits `evt.alarm` events whose `type` is a stable string
(like a reason code), carried as `{"type","ts"}`:

| `type` | Meaning |
|---|---|
| `forced` | door opened with no recent grant or REX — a break-in |
| `held` | an authorized-open door stayed open past `heldOpenSeconds` (DOTL) |
| `held_clear` | a previously-held door closed |

A grant (an `allow` tap or an `unlock` command) and a REX press each open a short
window during which a door-open reads as *authorized* (no `forced`), arming the
held-open timer instead; the DOTL timer is a deliberate no-ticker exception. While
a location's **fire** input is active, all alarm emission is suppressed
(forced/held during evacuation would be false alarms). The DOTL timer and the
held-open threshold are hardware-local timing, not policy.

### Scheduled posture & the strike hold

Of the postures, only `unlocked` (B) has a standing physical effect: the strike is
held energized so the door stands open. Every other posture is enforced lazily at
the next tap, so physically the strike is just *not held*. The controller keeps each
driven portal's hold in step with its effective posture three ways: immediately on
a posture command (so a lockdown re-locks at once), immediately when a portal is
armed, and on a periodic **hold-eval reconcile** (default 10s) that re-evaluates
each portal and is the no-event fallback that flips scheduled posture at window
boundaries — the controller's third deliberate timer exception. The reconcile is a
*sampling* loop (it reads "is the window open now", never computes boundaries), so
the interval is a pure latency knob with no correctness coupling; if an
`autoSchedule` is set but not yet loaded (mid re-sync) the reconcile keeps the
previous hold rather than flapping the door. A momentary `Pulse` composes with the
hold: the strike is energized while either is active, so a habitual tap during an
auto-unlock window pulses harmlessly. On controller shutdown/crash the strike
de-energizes (fail-secure: the door re-locks; egress stays hardware-owned).

> **v1 status:** the **reader is still simulated** — taps arrive by publishing to
> `acc.{location}.{type}.{thing}.tap` (the legacy/integration path; a real OSDP/
> Wiegand `ReaderDriver` slots in later). The **lock and door inputs have real
> drivers**: `controller.driver: mock` (default — no physical I/O, no door
> monitoring) or `gpio` (relays + DPS/REX over the Linux GPIO character device,
> via the `controller.model` hardware profile; Linux/arm64 edge hardware).

## Policy KV (bucket `ACC_POLICY`)

One key per record, `<prefix><natural-key>`. Cross-references are stored as
stable **codes** (or credential value / cardholder id), never PocketBase ids, so
keys and values are human-readable and self-contained. `accessd`'s mirror is the
sole writer; controllers are read-only watchers.

| Key | Value shape |
|---|---|
| `location.{code}` | `{"code","name","timezone","faiSuppress"}` |
| `sched.{code}` | `{"code","windows":[{"days":[1..7],"start":"HH:MM","end":"HH:MM"}],"observeHolidays"}` |
| `portal.{code}` | `{"code","type","location","posture","pulseSeconds",`<br>`"autoPosture"?,"autoSchedule"?,`<br>`"controller"?,"lockRelay"?,"dpsInput"?,"rexInput"?,"heldOpenSeconds"?}` |
| `controller.{code}` | `{"code","name","location","model"}` |
| `holiday.{pbid}` | `{"location":"<location code>","date":"YYYY-MM-DD","recurring"}` |
| `group.{code}` | `{"code","portals":["<portal code>"],"schedule":"<sched code>"}` |
| `role.{code}` | `{"code","groups":["<group code>"]}` |
| `user.{pbid}` | `{"id","status","roles":["<role code>"]}` |
| `cred.{value}` | `{"value","user":"<cardholder pbid>","status","validFrom"?,"validUntil"?}` |

`type` is the portal kind (`door`/`turnstile`/`elevator`/`gate`/`logical`) and the
`{type}` subject segment. `timezone` is an IANA name resolved once per location on
the controller. `days` are ISO weekdays (1=Mon … 7=Sun); `start`/`end` are local
wall-clock `HH:MM` (`24:00` allowed as end-of-day); `end <= start` means the
window crosses midnight. `user.{pbid}` and `cred.{value}.user` are the only places
a PocketBase id appears — the cardholder id is the credential→user join key.

`observeHolidays` (default true; stored inverted as `schedules.ignore_holidays` so
the safe default holds for any record) closes every window of that schedule on a
holiday of the evaluated portal's location. A `holiday` is a local calendar
`date`; `recurring` matches that month/day every year. `validFrom`/`validUntil` are
optional RFC 3339 credential bounds (the controller parses them once on apply; a
present-but-unparseable bound drops the credential — fail closed). `autoPosture` +
`autoSchedule` are **scheduled posture**: while the schedule's window is open the
controller adopts `autoPosture` (any posture, e.g. `unlocked` for auto-unlock or
`lockdown` for an overnight lock) instead of the standing `posture`; a runtime
command override still beats both. The two are written together or not at all
(the mirror drops a half-configured pair). Like the hardware fields, `autoPosture`/
`autoSchedule` are resolved by the controller, never by the pure `policy.Decide`.

A portal's hardware binding (the `?`-marked fields, omitted when unset) is **central
state**, carried in policy so a box is stateless and swappable: `controller` is the
code of the edge box that drives the portal; `lockRelay`/`dpsInput`/`rexInput` are
*logical* relay/input indices on that box; `heldOpenSeconds` is the held-open (DOTL)
threshold. The box maps the logical indices to physical lines via its `model`'s
hardware profile (`internal/drivers/hardware`); the indices and `controller`/`model`
are consumed only by the controller's PortalManager/runtime, never by the pure
`policy.Decide`. `controllers.last_seen`/`status` are **not** mirrored — accessd
writes them from heartbeats (see above), so they are absent from the KV value.

Eventual consistency is fail-safe: an unknown credential, a reference to a
not-yet-synced role/group/schedule, a malformed value, or no policy at all all
result in **deny**. A `WatchAll` re-delivers every key on (re)subscribe, so a
reconnect performs a full re-sync.

## Decision

`policy.Decide` is a pure function evaluated locally per tap. Order is the
contract — **deny-overrides come first**:

1. Unknown portal → `deny_unknown_point`
2. Posture gate: `disabled` → `deny_point_disabled`; `lockdown` → `deny_lockdown`
   (beats a valid credential); `unlocked` → `allow_posture_unlocked` (strike held,
   credential not consulted); `free_access` → `allow_posture_free_access` (any tap
   opens, credential not consulted); `secure` → continue
3. Credential/user: unknown credential → `deny_unknown_credential`; non-active
   credential → `deny_revoked`; before `validFrom` → `deny_not_yet_valid`; after
   `validUntil` → `deny_expired`; unknown/non-active user → `deny_revoked`
4. Grant: walk the user's roles → access groups; a group that contains this portal
   **and** whose schedule window is open now (and the day is not a holiday the
   schedule observes) → `allow_grant`. If a group contained the portal but no
   window was open → `deny_schedule_closed`; if none contained it → `deny_no_access`.

The effective posture fed to step 2 is resolved by the controller, not `Decide`:
a runtime command override, else scheduled posture (`autoPosture` while
`autoSchedule` is open), else the standing `posture`.

| Concept | Values |
|---|---|
| Posture | `secure` · `free_access` · `unlocked` · `lockdown` · `disabled` |
| Status (user/cred) | `active` (anything else denies: `suspended`, `revoked`) |
| Reason codes | `allow_grant` · `allow_posture_unlocked` · `allow_posture_free_access` · `deny_unknown_credential` · `deny_revoked` · `deny_not_yet_valid` · `deny_expired` · `deny_no_access` · `deny_schedule_closed` · `deny_lockdown` · `deny_point_disabled` · `deny_unknown_point` |

Reason codes are **stable strings** — they flow verbatim into `tap` events and
the `events` collection, so downstream consumers and dashboards depend on them.
(The `*_point` reason codes keep their historical spelling even though the entity
is now called a portal.)

## Audit projection (`events` collection)

`ACC_EVENTS` is the system of record for events; the PocketBase `events`
collection is a rebuildable projection behind the UI timeline. The durable
consumer (`acc-audit`) delivers from the start of the stream and is
**at-least-once** — a redelivery after a failed write may produce a duplicate row
(acceptable for v1). Each event subject maps to a row:

| Column | Source |
|---|---|
| `location`, `type`, `portal`, `kind` | parsed from the subject (`kind` ∈ `tap`/`state`/`alarm`/`fire`) |
| `credential`, `user`, `allow`, `reason`, `ts` | corresponding body fields |
| `payload` | the full event body (JSON) |

For `acc.{location}.evt.fire`, `portal` and `type` are empty and `kind` is `fire`.

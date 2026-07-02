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
in [`internal/policykv`](../internal/policykv/wire.go) (policy, downward) and
[`internal/statuskv`](../internal/statuskv/wire.go) (status, upward). Config keys
(bucket/stream names, the app token) have their own reference in
[`configuration.md`](configuration.md).

## Contents

- [Subject namespace](#subject-namespace) — the `acc.>` token layout and why it leads
- [Subjects](#subjects) — every subject, command bodies, door alarms, scheduled posture
- [Policy KV (`ACC_POLICY`)](#policy-kv-bucket-acc_policy) — downward policy mirror, one key per record
- [Status KV (`ACC_STATUS`)](#status-kv-bucket-acc_status) — upward device shadow
- [Decision](#decision) — the pure `policy.Decide` order and reason codes
- [Audit projection](#audit-projection-events-collection) — JetStream → PocketBase `events` rows

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
| `acc.{location}.{type}.{thing}.cmd.grant` | → ctrl | core NATS | `{"seconds":N,"actor":"…","reason":"…"}` |
| `acc.{location}.auxout.{thing}.cmd.output` | → ctrl | core NATS | `{"action":"on"\|"off"\|"pulse","seconds":N,"actor":"…","reason":"…"}` |
| `acc.{location}.evt.fire` | → ctrl | core NATS | `{"active":bool}` |
| `acc.{location}.{type}.{thing}.evt.tap` | ctrl → | core NATS → JetStream | `{"cred","user","allow","reason","ts","source"}` |
| `acc.{location}.{type}.{thing}.evt.state` | ctrl → | core NATS → JetStream | `{"posture","actor?","reason?","ts"}` |
| `acc.{location}.{type}.{thing}.evt.alarm` | ctrl → | core NATS → JetStream | `{"type","ts"}` |
| `acc.{location}.ctrl.{code}.heartbeat` | ctrl → accessd | core NATS (**not** JetStream) | `{"code","location","ts"}` |

\* → ctrl = controller subscribes; ctrl → = controller publishes.

Controllers subscribe per location with wildcards: taps via
`acc.{location}.*.*.tap` and commands via `acc.{location}.*.*.cmd.posture`,
`acc.{location}.*.*.cmd.grant`, and `acc.{location}.*.*.cmd.output` (aux outputs).
The audit surface is the `acc.*.…evt` subtree,
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
`accessd.controllerOfflineAfter` (default 45s).

### Command details

- **posture** — installs a runtime posture override for the portal. Valid values:
  `secure`, `free_access`, `unlocked`, `lockdown`, `disabled`, or `clear` (reverts
  to the *effective* posture from policy — scheduled posture if its window is open,
  else the standing posture). Overrides are operational state on the controller and
  are **never written back to PocketBase**. `until` is parsed but **deliberately
  ignored** — timed reversion must come from an external scheduler publishing a
  follow-up command. `free_access` opens on any tap without consulting the
  credential (strike pulses, door stays closed); `unlocked` holds the strike open.
- **grant** — a momentary strike pulse (the same physical effect as a credential
  grant, operator-initiated), distinct from a standing posture change.
  `seconds <= 0` (or omitted) falls back to the portal's configured `pulseSeconds`.
  Emits an `evt.tap` with `allow=true`, `reason=allow_command_grant`, and `user`
  set to the issuing actor, so the open is attributable in the audit trail.
- **output** — drives a named auxiliary output relay (`auxout` type). `on`/`off`
  set the standing held state; `pulse` energizes momentarily (`seconds<=0` falls
  back to the aux output's configured `pulseSeconds`). Aux outputs are first-class
  Things bound to a controller, addressed like portals; their live state flows up
  the status channel (`auxout.{code}`).
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
| `intrusion` | an armed area's `intrusion` aux point — or any `tamper_24h` point — went active, **or** a member portal was forced while its area is armed |

A grant (an `allow` tap or a `grant` command) and a REX press each open a short
window during which a door-open reads as *authorized* (no `forced`), arming the
held-open timer instead. While
a location's **fire** input is active, all alarm emission is suppressed
(forced/held/intrusion during evacuation would be false alarms). The DOTL timer and the
held-open threshold are hardware-local timing, not policy.

### Areas & intrusion-lite arming

An **area** is a logical, single-location arm-state grouping that may span several
controllers. A **point** of an area is either an **aux input** (a bare contact /
DPS-only door) or a **portal** (a full reader/DPS/REX door) — membership lives on
each (`aux_input.area` + `point_type`, or `portal.area`). The participant set
(`peers`) and "which areas this box drives" union both kinds.

For an **aux input**, `point_type` decides the trip: a `monitor` point is
observe-only (the default), an `intrusion` point raises an `intrusion` alarm
**while its area is armed**, and a `tamper_24h` point raises one **regardless** of
arm-state — any active edge trips, because a bare contact has no notion of an
"authorized" open.

For a **portal**, the reader changes the rule: an authorized open (a grant/REX) is
normal passage and never an intrusion, so a member portal trips intrusion only on
its **forced** condition (a DPS open with no grant/REX window — exactly what the
door state machine already detects) **while the area is armed**. That forced open
still emits its door-level `forced` event; the area `intrusion` is an additional
armed-zone roll-up. A portal with no area, or whose area is disarmed, escalates
nothing.

Intrusion alarms are addressed as a Thing of type `area` and reuse the generic
alarm subject — `acc.{location}.area.{areacode}.evt.alarm`, body
`{"type":"intrusion","point":"<aux-or-portal-code>","ts"}` — captured by the
existing 6-token portal-event wildcard with no new stream subject. They go through
the same fire-suppression gate as door alarms. Like `forced`, an intrusion alarm is
**edge-triggered** (one per active edge; a continuously-asserted point fires once) —
there is no controller-side latch and no new timer; the events row stays
unacknowledged until an operator acks it.

**Arm-state is durable, not a RAM override.** Unlike posture, a reboot must not
silently disarm, so arming rides the policy KV: an operator arm/disarm writes a
durable `armOverride` field on the area record (via accessd), the mirror propagates
it, and every participating controller converges. The controller resolves the
effective arm-state exactly as it resolves scheduled posture —
`armOverride` → `autoArm` (while `autoSchedule`'s window is open, holidays honored)
→ standing `arm` — and the fail-safe direction is the **inverse of access**: an
unresolved or unknown area falls back to standing (default disarmed) and never
spuriously arms. Arm-state boundaries themselves fire no alarm; they only change
whether a future trip alarms. There is **no `cmd.arm` subject** — arming is a record
write, not a fire-and-forget command.

**Entry-disarm (a valid grant disarms the area).** A portal flagged
`disarmOnGrant` is an *entry* door: a valid credential grant there durably disarms
its area. Because arm-state is durable and central (and an area spans controllers),
this can't be a local edge action — so the edge just emits the `evt.tap` it already
emits, and accessd's **disarm sink** (`internal/disarm`, a third independent durable
on ACC_EVENTS alongside the audit and notify consumers, `DeliverNew`, filter
`acc.*.*.*.evt.tap`) observes the grant and writes the same durable `armOverride:
disarmed` the manual disarm route writes; the mirror then converges every peer
controller. Only a **credential** grant disarms (`allow` with a `cred`): a deny is
ignored, and an operator remote `cmd.grant` carries no `cred`, so a remote door-pop
can't silently disarm a building. The write is idempotent and skips an area that is
already disarmed / can never be armed, so a redelivery is a harmless no-op (no dedup
needed). Each disarm writes its own `audit_logs` row attributed to the credential +
portal. **Re-arm is explicit (a v1 limitation):** `armOverride: disarmed` is sticky
and outranks `autoArm`, so entry-disarm does **not** compose with scheduled
auto-re-arm — re-arm via an operator action (or don't put `autoArm` on an
entry-disarm area). Automatic re-arm is a deliberate fast-follow.

Each input's **contact sense** is configurable per install (`dpsContact`/
`rexContact`/`aux_input.contact`, see Policy KV below): a normally-open vs
normally-closed contact is folded onto the board's electrical polarity so "active"
always means the monitored condition is asserted, regardless of wiring. By default
a REX press only shunts the forced alarm; with `rexUnlock` the controller also
pulses the strike (electric egress). The strike's fail-safe behavior follows
`lockType`: a fail-secure **strike** de-energizes (re-locks) on shutdown/crash,
while a fail-safe **maglock** idles energized and releases on power loss by design.

### Scheduled posture & the strike hold

Of the postures, only `unlocked` (B) has a standing physical effect: the strike is
held energized so the door stands open. Every other posture is enforced lazily at
the next tap, so physically the strike is just *not held*. The controller keeps each
driven portal's hold in step with its effective posture three ways: immediately on
a posture command (so a lockdown re-locks at once), immediately when a portal is
armed, and on a periodic **hold-eval reconcile** (default 10s) that re-evaluates
each portal and is the no-event fallback that flips scheduled posture at window
boundaries. The reconcile is a
*sampling* loop (it reads "is the window open now", never computes boundaries), so
the interval is a pure latency knob with no correctness coupling; if an
`autoSchedule` is set but not yet loaded (mid re-sync) the reconcile keeps the
previous hold rather than flapping the door. A momentary `Pulse` composes with the
hold: the strike is energized while either is active, so a habitual tap during an
auto-unlock window pulses harmlessly. On controller shutdown/crash the strike
de-energizes (fail-secure: the door re-locks; egress stays hardware-owned).

> **Reader options** (`controller.reader`): `nats` (default) — taps arrive by
> publishing to `acc.{location}.{type}.{thing}.tap`, the simulated/integration path,
> driven with `nats pub`; `osdp` — a real OSDP reader polled on the model's RS485
> bus (clear-text in v1; OSDP Secure Channel is a planned fast-follow); or `both` —
> the NATS reader for **every** portal plus the OSDP reader for the portals that
> have a physical reader. In `both` a portal opts into OSDP via its `readerAddress`:
> `>= 0` means a reader at that PD address, `-1` (or absent → 0 is treated as PD 0)
> means NATS-only. `osdp` and `both` require `controller.model` (its RS485 serial
> port). Each emitted `evt.tap` carries a `source` (`nats`/`osdp`) so a physical read
> is distinguishable from a NATS-published tap. The reader is
> independent of the lock/door driver: an `osdp` reader pairs with any
> `controller.driver` (the strike and DPS/REX stay on GPIO/I2C). The **lock and door
> inputs have real drivers**: `controller.driver: mock` (default — no physical I/O,
> no door monitoring) or `gpio` (relays + DPS/REX on real hardware, the
> `controller.model` profile selecting the transport — native GPIO char device or an
> MCP23017 I2C expander; Linux edge hardware).
>
> An OSDP card read becomes a credential string via the **lowercase hex of the raw
> card bytes** (`internal/drivers/osdp/wire`): lossless and format-agnostic, so
> enrollment mirrors what the bench observes. Decimal/Wiegand decoding depends on the
> reader's bit order and is deferred until confirmed against physical hardware.

## Policy KV (bucket `ACC_POLICY`)

One key per record, `<prefix><natural-key>`. Cross-references are stored as
stable **codes** (or credential value / cardholder id), never PocketBase ids, so
keys and values are human-readable and self-contained. `accessd`'s mirror is the
sole writer; controllers are read-only watchers.

| Key | Value shape |
|---|---|
| `location.{code}` | `{"code","name","timezone","faiSuppress","holidayCalendars":["<calendar code>"]?}` |
| `sched.{code}` | `{"code","windows":[{"days":[1..7],"start":"HH:MM","end":"HH:MM"}],"observeHolidays"}` |
| `portal.{code}` | `{"code","type","location","posture","pulseSeconds",`<br>`"autoPosture"?,"autoSchedule"?,`<br>`"controller"?,"lockRelay"?,"dpsInput"?,"rexInput"?,"heldOpenSeconds"?,"readerAddress"?,`<br>`"dpsContact"?,"rexContact"?,"lockType"?,"rexUnlock"?,`<br>`"area"?,"disarmOnGrant"?}` |
| `controller.{code}` | `{"code","name","location","model"}` |
| `holiday.{pbid}` | `{"calendar":"<calendar code>","date":"YYYY-MM-DD","recurring"}` |
| `group.{code}` | `{"code","portals":["<portal code>"],"schedule":"<sched code>"}` |
| `role.{code}` | `{"code","groups":["<group code>"]}` |
| `user.{pbid}` | `{"id","status","roles":["<role code>"]}` |
| `cred.{value}` | `{"value","user":"<cardholder pbid>","status","validFrom"?,"validUntil"?}` |
| `auxin.{code}` | `{"code","location","controller"?,"inputIndex"?,"contact"?,"area"?,"pointType"?}` |
| `auxout.{code}` | `{"code","location","controller"?,"relayIndex"?,"pulseSeconds"?}` |
| `area.{code}` | `{"code","name"?,"location","arm"?,"armOverride"?,"autoArm"?,"autoSchedule"?}` |

**UI-only fields are deliberately excluded.** The management UI adds
`locations.description`/`coordinates`/`floorplan` and `portals.floorplan_position`
for the location map and floor-plan views. These are visualization metadata only —
the mirror's wire shape above omits them, so they never reach `ACC_POLICY` or the
edge. `policy.Decide`, arming, and the door state machine are unaffected, and no
floor-plan image data ever leaves accessd.

`type` is the portal kind (`door`/`turnstile`/`elevator`/`gate`/`logical`) and the
`{type}` subject segment. `timezone` is an IANA name resolved once per location on
the controller. `days` are ISO weekdays (1=Mon … 7=Sun); `start`/`end` are local
wall-clock `HH:MM` (`24:00` allowed as end-of-day); `end <= start` means the
window crosses midnight. `user.{pbid}` and `cred.{value}.user` are the only places
a PocketBase id appears — the cardholder id is the credential→user join key.

`observeHolidays` (default true; stored inverted as `schedules.ignore_holidays` so
the safe default holds for any record) closes every window of that schedule on a
holiday observed by the evaluated portal's location. Holidays are grouped into
**calendars**: a `holiday` belongs to one calendar, and a location observes a set
of them (`location.holidayCalendars`), so one shared "Christmas" serves many sites
instead of being duplicated per location. The controller unions a location's
observed calendars into its holiday set, so the same date can close schedules at
every site that observes the calendar. The `holiday_calendars` collection is a pure
grouping label and is **not** mirrored to KV — both holidays and locations carry
the calendar `code`, so the edge never needs the calendar record itself. A
`holiday` is a local calendar `date`; `recurring` matches that month/day every
year. `validFrom`/`validUntil` are
optional RFC 3339 credential bounds (the controller parses them once on apply; a
present-but-unparseable bound drops the credential — fail closed). The credentials
collection's `type` (`generic`/`wiegand`/`pin`/`mobile`) is a **control-plane label
only** — it is deliberately absent from the `cred` value above, never crosses the
wire, and `policy.Decide` ignores it. `autoPosture` +
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
threshold; `readerAddress` is the reader's OSDP PD address on the box's RS485 bus
(used when `controller.reader` is `osdp` or `both`). It doubles as the per-portal
OSDP enable: `>= 0` is a reader at that PD address (0 = the single-reader case),
`-1` is NATS-only (no OSDP). The UI writes `-1` when a portal's OSDP reader is off. The
box maps the logical indices to physical lines via its `model`'s
hardware profile (`internal/drivers/hardware`); the indices and `controller`/`model`
are consumed only by the controller's PortalManager/runtime, never by the pure
`policy.Decide`.

The remaining hardware fields are **per-install wiring sense**, distinct from the
board's electrical polarity (which lives in the model profile) — the controller
folds them onto that polarity when it arms each line. `dpsContact`/`rexContact` are
the contact type, `"nc"` or `"no"` (empty = the common default: a DPS is normally
**closed** when the door is shut, a REX is normally **open**, closed when pressed);
the non-default value inverts how a contact edge is read. `lockType` is `"strike"`
(empty/default, fail-secure — energize to unlock) or `"maglock"` (fail-safe —
energize to lock, so the relay idles energized and releases on power loss); a
maglock inverts the lock relay's drive sense. `rexUnlock` (default false) makes a
REX press also pulse the strike for electric egress, not just shunt the forced
alarm. Like the indices, these are controller-only and never seen by `policy.Decide`.
`aux_input.contact` is the same `"nc"`/`"no"` sense (default normally-open). `controllers.last_seen`/`status` are **not** mirrored — accessd
writes them from heartbeats (see above), so they are absent from the KV value.

Eventual consistency is fail-safe: an unknown credential, a reference to a
not-yet-synced role/group/schedule, a malformed value, or no policy at all all
result in **deny**. A `WatchAll` re-delivers every key on (re)subscribe, so a
reconnect performs a full re-sync.

## Status KV (bucket `ACC_STATUS`)

The upward "device shadow" — the live state of each point the edge drives, the
mirror image of `ACC_POLICY`: **controllers write** (one key per point, value
shapes in [`internal/statuskv`](../internal/statuskv/wire.go)); **accessd watches**
and projects into the rebuildable `point_status` collection (the UI subscribes for
realtime). Latest-wins per key (KV history 1): this is "what is true now," not
history — the history of record is `ACC_EVENTS`. accessd owns bucket creation;
controllers bind it read-write. A controller deletes its keys on disarm; a
reconnect re-publishes the whole shadow.

accessd's projector ([`internal/status`](../internal/status)) is the upward twin
of a controller's PolicyStore: it `WatchAll`s the bucket (a reconnect re-delivers
every key = full re-sync), and on the sync sentinel it **prunes** `point_status`
rows whose KV key is gone — so a deleted shadow key removes the projection row.

| Key | Value shape |
|---|---|
| `portal.{code}` | `{"code","location","controller","door":"open"\|"closed"\|"unknown","posture","source":"standing"\|"scheduled"\|"override","held","updatedAt"}` |
| `auxin.{code}` | `{"code","location","controller","active","updatedAt"}` |
| `auxout.{code}` | `{"code","location","controller","energized","updatedAt"}` |
| `area.{controller}.{code}` | `{"code","location","controller","arm":"armed"\|"disarmed","source","peers":["<controller code>"],"updatedAt"}` |

The area key is **compound** (one shadow per participating controller for the same
area) — `code`/`controller` come from the value, not the key. `peers` is the full
participant set (every controller with a member input in the area), so the console
has a **denominator**: the area is "armed" only when *every* peer reports armed,
"partial/arming" if a peer disagrees or hasn't reported (e.g. it was offline at arm
time and converges on reconnect), and "disarmed" when all peers report disarmed.

`door` is `unknown` on a controller without a DPS input wired (e.g. the mock
driver) or before the first edge. `posture` is the current **effective** posture
(command override / scheduled / standing) and `source` is which of those three
produced it — so the UI can flag a manual `override` (or an active `scheduled`
posture) distinctly from the `standing` state. An empty `source` (a shadow from an
older controller) reads as `standing`. `held` is the **door-held-open (DOTL)
alarm flag** — true while a held-open alarm is active — **not** the strike's
physical state; the strike hold follows `posture` (only `unlocked` holds it).
`energized` is an aux output's standing held state (a `pulse` is momentary and
not reflected).

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
| Reason codes | `allow_grant` · `allow_posture_unlocked` · `allow_posture_free_access` · `allow_command_grant` · `deny_unknown_credential` · `deny_revoked` · `deny_not_yet_valid` · `deny_expired` · `deny_no_access` · `deny_schedule_closed` · `deny_lockdown` · `deny_point_disabled` · `deny_unknown_point` |

Reason codes are **stable strings** — they flow verbatim into `tap` events and
the `events` collection, so downstream consumers and dashboards depend on them.
(The `*_point` reason codes keep their historical spelling even though the entity
is now called a portal.)

## Audit projection (`events` collection)

`ACC_EVENTS` is the system of record for events; the PocketBase `events`
collection is a rebuildable projection behind the UI timeline. The durable
consumer (`acc-audit`) delivers from the start of the stream and is
**at-least-once, made idempotent**: each row carries the message's JetStream
stream sequence (`stream_seq`, unique-indexed), and a redelivery whose row
already landed is acked and skipped. Each event subject maps to a row:

| Column | Source |
|---|---|
| `location`, `type`, `portal`, `kind` | parsed from the subject (`kind` ∈ `tap`/`state`/`alarm`/`fire`) |
| `credential`, `user`, `allow`, `reason`, `ts` | corresponding body fields |
| `source` | tap body field (`nats`/`osdp`) — which reader produced a tap; empty for non-tap and legacy rows |
| `acknowledged`, `ack_by`, `ack_at` | operator acknowledgement (set via `POST /api/events/{id}/ack`, the `command` capability) |
| `stream_seq` | the message's JetStream stream sequence (idempotency key; 0 on rows projected before it existed) |
| `payload` | the full event body (JSON) |

For `acc.{location}.evt.fire`, `portal` and `type` are empty and `kind` is `fire`.
For an area intrusion alarm, `type` is `area`, `portal` is the area code, `kind` is
`alarm`, and `payload.type` is `intrusion` (with `payload.point` naming the tripped
input). The ack fields live on the projection row; the `stream_seq` dedupe means
a redelivery or stream replay no longer resurrects an already-acknowledged row
(rows from before the field existed read `stream_seq` 0 and stay exempt from the
unique index).

**Notification sink.** A *second, independent* durable consumer (`acc-notify`,
[`internal/notify`](../internal/notify)) on `ACC_EVENTS` emails on `alarm`/`fire`.
It is parallel to `acc-audit`, not coupled to it (the audit consumer is an
at-least-once projection; coupling alerting there would double-send on redelivery).
It diverges in one way: **`DeliverNew`, not `DeliverAll`** — alerting is not a
backfillable projection, so the sink starts from "now" instead of emailing every
historical alarm. Bounded redelivery (`MaxDeliver`) keeps a dead SMTP server from
looping forever; the SMTP transport is PocketBase's own mail settings, configured
in `/_`. It is **config-free and always started** (like the disarm sink) and stays
inert unless two opt-ins line up: the alarm's source opts in
(`portals`/`areas.notify_on_alarm`, `locations.notify_fire`) **and** at least one
operator opts in (`users.notify`). Recipients are then **scoped by location**:
`users.notify_locations` is the set of locations an operator is paged for, so an
alarm at location *L* mails only the notify operators whose scope is empty (= all
locations, the default) or contains *L* — routing site-local alarms to site-local
people without a per-source→per-operator rules engine. The sink itself stays
PocketBase-free — it parses the event and hands it to accessd, which resolves the
source opt-in and the location-scoped recipients; `held_clear` (a held-open
auto-clear) is never emailed.

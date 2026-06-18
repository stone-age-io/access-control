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

Subjects follow the Stone-Age.io platform convention: a **portal** (a controllable
opening or logical access target) is a Thing addressed
`{location}.{type}.{thing}`, and **`acc`** is an app-discriminator segment placed
*before the verb* (`acc.tap`, `acc.cmd.*`, `acc.evt.*`). The `acc` token is set by
`subjects.app` in config (or `SA_SUBJECTS_APP`), default `acc`. `accessd` and all
controllers **must use the same app token** — they publish and subscribe to each
other's traffic, so a mismatch silently severs policy/commands/events. Change it
only to isolate a deployment on a shared NATS account; it must be a single NATS
token (no `.`, `*`, `>`, or whitespace).

`{location}`, `{type}`, and `{thing}` are each a single NATS token and the record
**codes** (e.g. `hq`, `door`, `lobby-main`), never PocketBase ids. The mirror
rejects a location/portal code or portal type that is not a single token or that
collides with a reserved keyword (`acc`/`evt`/`cmd`/`tap`/`fire`).

## Subjects

| Subject | Dir* | Transport | Body |
|---|---|---|---|
| `{location}.{type}.{thing}.acc.tap` | → ctrl | core NATS | `{"cred":"..."}` or a bare credential string |
| `{location}.{type}.{thing}.acc.cmd.posture` | → ctrl | core NATS | `{"posture":"…","actor":"…","reason":"…","until":"…"}` |
| `{location}.{type}.{thing}.acc.cmd.unlock` | → ctrl | core NATS | `{"seconds":N,"actor":"…","reason":"…"}` |
| `{location}.acc.evt.fire` | → ctrl | core NATS | `{"active":bool}` |
| `{location}.{type}.{thing}.acc.evt.tap` | ctrl → | core NATS → JetStream | `{"cred","user","allow","reason","ts"}` |
| `{location}.{type}.{thing}.acc.evt.state` | ctrl → | core NATS → JetStream | `{"posture","actor?","reason?","ts"}` |
| `{location}.{type}.{thing}.acc.evt.alarm` | ctrl → | core NATS → JetStream | `{"type","ts"}` |

\* → ctrl = controller subscribes; ctrl → = controller publishes.

Controllers subscribe per location with wildcards: taps via
`{location}.*.*.acc.tap` and commands via `{location}.*.*.acc.cmd.posture` /
`{location}.*.*.acc.cmd.unlock`. The audit surface is the `acc.evt` subtree,
captured by `ACC_EVENTS` and projected into the `events` collection via **two
token-count-disjoint** stream subjects:

- `*.acc.evt.>` — the 4-token location-scoped fire (`{location}.acc.evt.fire`)
- `*.*.*.acc.evt.>` — the 6-token portal events (`{location}.{type}.{thing}.acc.evt.{kind}`)

Both pin a literal `acc.evt`, so neither captures a foreign Thing's events (e.g.
`warehouse-a.camera.cam-042.evt.motion` has no `acc` segment). All bodies are
JSON; `ts` is RFC 3339 UTC.

### Command details

- **posture** — installs a runtime posture override for the portal. Valid values:
  `secure`, `unlocked`, `lockdown`, `disabled`, or `clear` (reverts to the
  standing posture from policy). Overrides are operational state on the
  controller and are **never written back to PocketBase**. `until` is parsed but
  **deliberately ignored** — the controller grows no timer; timed reversion must
  come from an external scheduler publishing a follow-up command.
- **unlock** — a momentary strike pulse, distinct from a standing posture change.
  `seconds <= 0` (or omitted) falls back to the portal's configured `pulseSeconds`.
- **fire** — toggles a location's fire-alarm-input state. While active, the
  controller **suppresses alarm emission** for that location (forced/held-open
  events would be false alarms during evacuation). It never changes posture and
  never unlocks — hardware owns egress. It is location-scoped (not per-portal) and
  lives on the `acc.evt` namespace, not `acc.cmd`: it is both a control input the
  controller subscribes to *and* an audited event the stream captures
  (`kind="fire"`).

> **v1 note:** the reader/lock/FAI are mocks. Taps are simulated by publishing to
> `{location}.{type}.{thing}.acc.tap`; the lock just logs its pulse; there is no
> real alarm source yet (the `alarm` subject is the gate real detection will flow
> through).

## Policy KV (bucket `ACC_POLICY`)

One key per record, `<prefix><natural-key>`. Cross-references are stored as
stable **codes** (or credential value / cardholder id), never PocketBase ids, so
keys and values are human-readable and self-contained. `accessd`'s mirror is the
sole writer; controllers are read-only watchers.

| Key | Value shape |
|---|---|
| `location.{code}` | `{"code","name","timezone","faiSuppress"}` |
| `sched.{code}` | `{"code","windows":[{"days":[1..7],"start":"HH:MM","end":"HH:MM"}]}` |
| `portal.{code}` | `{"code","type","location","posture","pulseSeconds"}` |
| `group.{code}` | `{"code","portals":["<portal code>"],"schedule":"<sched code>"}` |
| `role.{code}` | `{"code","groups":["<group code>"]}` |
| `user.{pbid}` | `{"id","status","roles":["<role code>"]}` |
| `cred.{value}` | `{"value","user":"<cardholder pbid>","status"}` |

`type` is the portal kind (`door`/`turnstile`/`elevator`/`gate`/`logical`) and the
`{type}` subject segment. `timezone` is an IANA name resolved once per location on
the controller. `days` are ISO weekdays (1=Mon … 7=Sun); `start`/`end` are local
wall-clock `HH:MM` (`24:00` allowed as end-of-day); `end <= start` means the
window crosses midnight. `user.{pbid}` and `cred.{value}.user` are the only places
a PocketBase id appears — the cardholder id is the credential→user join key.

Eventual consistency is fail-safe: an unknown credential, a reference to a
not-yet-synced role/group/schedule, a malformed value, or no policy at all all
result in **deny**. A `WatchAll` re-delivers every key on (re)subscribe, so a
reconnect performs a full re-sync.

## Decision

`policy.Decide` is a pure function evaluated locally per tap. Order is the
contract — **deny-overrides come first**:

1. Unknown portal → `deny_unknown_point`
2. Posture gate: `disabled` → `deny_point_disabled`; `lockdown` → `deny_lockdown`
   (beats a valid credential); `unlocked` → `allow_posture_unlocked` (credential
   not consulted); `secure` → continue
3. Credential/user: unknown credential → `deny_unknown_credential`; non-active
   credential, or unknown/non-active user → `deny_revoked`
4. Grant: walk the user's roles → access groups; a group that contains this portal
   **and** whose schedule window is open now → `allow_grant`. If a group
   contained the portal but no window was open → `deny_schedule_closed`; if none
   contained it → `deny_no_access`.

| Concept | Values |
|---|---|
| Posture | `secure` · `unlocked` · `lockdown` · `disabled` |
| Status (user/cred) | `active` (anything else denies: `suspended`, `revoked`) |
| Reason codes | `allow_grant` · `allow_posture_unlocked` · `deny_unknown_credential` · `deny_revoked` · `deny_no_access` · `deny_schedule_closed` · `deny_lockdown` · `deny_point_disabled` · `deny_unknown_point` |

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

For `{location}.acc.evt.fire`, `portal` and `type` are empty and `kind` is `fire`.

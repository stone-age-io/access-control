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

Every subject hangs off a single **root namespace**, default `acc`, set by
`subjects.root` in config (or `SA_SUBJECTS_ROOT`). `accessd` and all controllers
**must use the same root** — they publish and subscribe to each other's traffic,
so a mismatch silently severs policy/commands/events. Change it only to isolate a
deployment on a shared NATS account; it must be a single NATS token (no `.`, `*`,
`>`, or whitespace). `{site}` and `{point}` are the record **codes** (e.g. `hq`,
`lobby-main`), never PocketBase ids.

## Subjects

| Subject | Dir* | Transport | Body |
|---|---|---|---|
| `acc.tap.{site}.{point}` | → ctrl | core NATS | `{"cred":"..."}` or a bare credential string |
| `acc.cmd.{site}.{point}.posture` | → ctrl | core NATS | `{"posture":"…","actor":"…","reason":"…","until":"…"}` |
| `acc.cmd.{site}.{point}.unlock` | → ctrl | core NATS | `{"seconds":N,"actor":"…","reason":"…"}` |
| `acc.evt.{site}.fire` | → ctrl | core NATS | `{"active":bool}` |
| `acc.evt.{site}.{point}.tap` | ctrl → | core NATS → JetStream | `{"cred","user","allow","reason","ts"}` |
| `acc.evt.{site}.{point}.state` | ctrl → | core NATS → JetStream | `{"posture","actor?","reason?","ts"}` |
| `acc.evt.{site}.{point}.alarm` | ctrl → | core NATS → JetStream | `{"type","ts"}` |

\* → ctrl = controller subscribes; ctrl → = controller publishes.

Controllers subscribe to commands per site with wildcards
(`acc.cmd.{site}.*.posture`, `acc.cmd.{site}.*.unlock`). The `acc.evt.>` subtree
is the **audit surface**: the `ACC_EVENTS` stream captures it and the audit
consumer projects it into the `events` collection. All bodies are JSON; `ts` is
RFC 3339 UTC.

### Command details

- **posture** — installs a runtime posture override for the point. Valid values:
  `secure`, `unlocked`, `lockdown`, `disabled`, or `clear` (reverts to the
  standing posture from policy). Overrides are operational state on the
  controller and are **never written back to PocketBase**. `until` is parsed but
  **deliberately ignored** — the controller grows no timer; timed reversion must
  come from an external scheduler publishing a follow-up command.
- **unlock** — a momentary strike pulse, distinct from a standing posture change.
  `seconds <= 0` (or omitted) falls back to the point's configured `pulseSeconds`.
- **fire** — toggles a site's fire-alarm-input state. While active, the controller
  **suppresses alarm emission** for that site (forced/held-open events would be
  false alarms during evacuation). It never changes posture and never unlocks —
  hardware owns egress. Note it lives on the `evt` namespace, not `cmd`: it is
  both a control input the controller subscribes to *and* an audited event the
  stream captures (`kind="fire"`).

> **v1 note:** the reader/lock/FAI are mocks. Taps are simulated by publishing to
> `acc.tap.{site}.{point}`; the lock just logs its pulse; there is no real alarm
> source yet (the `alarm` subject is the gate real detection will flow through).

## Policy KV (bucket `ACC_POLICY`)

One key per record, `<prefix><natural-key>`. Cross-references are stored as
stable **codes** (or credential value / cardholder id), never PocketBase ids, so
keys and values are human-readable and self-contained. `accessd`'s mirror is the
sole writer; controllers are read-only watchers.

| Key | Value shape |
|---|---|
| `site.{code}` | `{"code","name","timezone","faiSuppress"}` |
| `sched.{code}` | `{"code","windows":[{"days":[1..7],"start":"HH:MM","end":"HH:MM"}]}` |
| `point.{code}` | `{"code","site","posture","pulseSeconds"}` |
| `group.{code}` | `{"code","points":["<point code>"],"schedule":"<sched code>"}` |
| `role.{code}` | `{"code","groups":["<group code>"]}` |
| `user.{pbid}` | `{"id","status","roles":["<role code>"]}` |
| `cred.{value}` | `{"value","user":"<cardholder pbid>","status"}` |

`timezone` is an IANA name resolved once per site on the controller. `days` are
ISO weekdays (1=Mon … 7=Sun); `start`/`end` are local wall-clock `HH:MM`
(`24:00` allowed as end-of-day); `end <= start` means the window crosses
midnight. `user.{pbid}` and `cred.{value}.user` are the only places a PocketBase
id appears — the cardholder id is the credential→user join key.

Eventual consistency is fail-safe: an unknown credential, a reference to a
not-yet-synced role/group/schedule, a malformed value, or no policy at all all
result in **deny**. A `WatchAll` re-delivers every key on (re)subscribe, so a
reconnect performs a full re-sync.

## Decision

`policy.Decide` is a pure function evaluated locally per tap. Order is the
contract — **deny-overrides come first**:

1. Unknown access point → `deny_unknown_point`
2. Posture gate: `disabled` → `deny_point_disabled`; `lockdown` → `deny_lockdown`
   (beats a valid credential); `unlocked` → `allow_posture_unlocked` (credential
   not consulted); `secure` → continue
3. Credential/user: unknown credential → `deny_unknown_credential`; non-active
   credential, or unknown/non-active user → `deny_revoked`
4. Grant: walk the user's roles → access groups; a group that contains this point
   **and** whose schedule window is open now → `allow_grant`. If a group
   contained the point but no window was open → `deny_schedule_closed`; if none
   contained it → `deny_no_access`.

| Concept | Values |
|---|---|
| Posture | `secure` · `unlocked` · `lockdown` · `disabled` |
| Status (user/cred) | `active` (anything else denies: `suspended`, `revoked`) |
| Reason codes | `allow_grant` · `allow_posture_unlocked` · `deny_unknown_credential` · `deny_revoked` · `deny_no_access` · `deny_schedule_closed` · `deny_lockdown` · `deny_point_disabled` · `deny_unknown_point` |

Reason codes are **stable strings** — they flow verbatim into `tap` events and
the `events` collection, so downstream consumers and dashboards depend on them.

## Audit projection (`events` collection)

`ACC_EVENTS` is the system of record for events; the PocketBase `events`
collection is a rebuildable read model behind the UI timeline. The durable
consumer (`acc-audit`) delivers from the start of the stream and is
**at-least-once** — a redelivery after a failed write may produce a duplicate row
(acceptable for v1). Each event subject maps to a row:

| Column | Source |
|---|---|
| `site`, `access_point`, `kind` | parsed from the subject (`kind` ∈ `tap`/`state`/`alarm`/`fire`) |
| `credential`, `user`, `allow`, `reason`, `ts` | corresponding body fields |
| `payload` | the full event body (JSON) |

For `acc.evt.{site}.fire`, `access_point` is empty and `kind` is `fire`.

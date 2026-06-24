# stone-access operators & control-plane access

Who may sign into the management UI, what each operator is allowed to do, and how
every control-plane change is recorded. This is the **control plane's** access
model — distinct from the **data plane** access *decision* (`policy.Decide`, the
credential-at-a-door call documented in [`protocol.md`](protocol.md)). The two
never mix: `policy.Decide`, the KV mirror, and the controller never see operator
permissions, and a controller never sees an operator.

## Contents

- [Sign-in](#sign-in) — the `users` auth collection, superuser break-glass
- [Capabilities](#capabilities) — the five orthogonal abilities
- [What each capability gates](#what-each-capability-gates) — collection rules + custom routes
- [Presets](#presets) — the UI's named capability sets
- [Privilege-escalation guard](#privilege-escalation-guard)
- [Control-plane audit log](#control-plane-audit-log-audit_logs) — `audit_logs`, what is recorded, retention

## Sign-in

The management UI authenticates against PocketBase's built-in **`users` auth
collection**, *not* the all-powerful `_superusers` admin. Open signup is disabled —
operator accounts are created by another operator who holds the `operators`
capability (or by a superuser). Seed the first one with the guarded dev fixture
(`pbmigrations/1750000010`) or create it directly in the PocketBase admin (`/_`).

A **superuser** remains the break-glass account: it bypasses every collection rule
and every capability check, and it still signs into the PocketBase admin UI at
`/_`. Create one with `./accessd superuser upsert <email> <pass>`. Superuser logins
are deliberately **not** written to the operator audit log (they go through
`_superusers`, a separate auth collection).

## Capabilities

An operator's ability is the multi-select **`users.permissions`** field — an
*orthogonal set of capabilities*, not a rank. (It replaced an earlier
`admin`/`operator`/`viewer` ladder, which couldn't express real roles like
"enrollment only" or "door ops but not hardware" — each a non-linear subset.)
**Read is a universal floor**: any authenticated operator can read every
operational collection. Only **writes and commands** are gated, each by one
capability:

| Capability | Grants |
|---|---|
| `enroll` | write **people** — cardholders, credentials |
| `policy` | write **access logic** — roles, access_groups, schedules, holidays |
| `topology` | write **hardware** — locations, controllers, portals, aux_input, aux_output |
| `command` | issue **door commands** — grant, posture, aux-output drive |
| `operators` | manage **operator accounts**, read the **audit log**, and **hard-delete** structural records |

The five names are constants in [`internal/authz`](../internal/authz/authz.go)
(`CapEnroll`/`CapPolicy`/`CapTopology`/`CapCommand`/`CapOperators`). They are the
operator's whole authorization surface — there is no role field to drift out of
sync with the permissions.

## What each capability gates

Two enforcement points share `users.permissions`:

**1. Collection CRUD** — PocketBase collection rules (the real boundary), set by
migration [`1750000016`](../pbmigrations/1750000016_operator_permissions.go).
List/View are open to any authenticated operator (`@request.auth.id != ""`) except
where noted; the table shows the **write** rules:

| Collection(s) | Create / Update | Delete |
|---|---|---|
| `cardholders`, `credentials` | `enroll` | `operators` |
| `schedules`, `access_groups`, `roles` | `policy` | `operators` |
| `holidays` | `policy` | `policy` |
| `locations`, `controllers`, `portals`, `aux_input`, `aux_output`, `areas` | `topology` | `operators` |
| `users` | `operators` (create) · self-or-`operators` (update) | `operators` |
| `audit_logs` | — (superuser-only; hook-written) | — |
| `events`, `point_status` | — (machine-written; accessd's `app.Save` bypasses rules) | — |

`users` List/View is **self or `operators`** (an operator without `operators` sees
only their own account), and `audit_logs` List/View needs `operators`.

**Hard-delete is a trusted action.** Removing a person or a structural
topology/policy record requires `operators` — for everyday revocation, *deactivate*
via the existing status / `valid_from` / `valid_until` fields instead of deleting.
`holidays` is the one exception (its delete stays at `policy`, being low-value
access logic).

> The rule expression is `@request.auth.permissions ~ "x"` (JSON LIKE), **not**
> `?=`. A multi-select referenced through `@request.auth` is bound as its
> serialized array (not `json_each`-expanded), so the "any-equals" `?=` silently
> matches nothing; `~` (contains) matches, and is exact here only because the five
> capability names are pairwise non-substring. This is load-bearing and is the
> security boundary `TestPermissionRuleEnforcement` locks down — don't rename a
> capability to a substring of another, and don't switch the operator.

**2. Custom HTTP routes** — accessd's bespoke routes don't go through a collection,
so they call `authz.RequireCapability` per handler:

| Route | Requires | Effect |
|---|---|---|
| `POST /api/portals/{id}/grant` | `command` | momentary strike pulse → `cmd.grant` |
| `POST /api/portals/{id}/posture` | `command` | posture override / clear → `cmd.posture` |
| `POST /api/aux-outputs/{id}/output` | `command` | drive an aux output → `cmd.output` |
| `POST /api/events/{id}/ack` | `command` | acknowledge an alarm/fire (sets ack fields) |
| `POST /api/areas/{id}/arm` · `/disarm` · `/arm-clear` | `command` | set/clear an area's durable `arm_override` |
| `GET /api/models` | any authenticated | enum/options metadata for the UI |

Most of these bridge the UI to the **NATS command plane**; the wire subjects and
bodies they publish are documented in [`protocol.md`](protocol.md#command-details).
The **ack** and **arm/disarm** routes are the exception: they write a PocketBase
record (the ack fields; the area `arm_override`) rather than publishing a
fire-and-forget command, because arm-state must be *durable* (a reboot must not
silently disarm). Each therefore writes its own `audit_logs` row (a custom-route
`app.Save` doesn't trip the changelog `*Request` hooks).

> **`command` now covers arming.** Adding arm/disarm and alarm-ack under `command`
> widens that capability's meaning: an operator you trust to buzz a door open can
> now also arm/disarm the intrusion system and acknowledge alarms. That is a
> deliberate v1 choice (no separate `arm` capability yet) — keep it in mind when
> granting `command`. Area *configuration* (membership, schedules) stays at
> `topology`; only the operational arm/disarm is `command`.

> **Entry-disarm is credential-driven, not capability-gated.** A valid credential
> grant at a portal flagged `disarm_on_grant` durably disarms that portal's area —
> this is a *cardholder* action (badging in), not an operator API call, so no
> operator capability is involved. accessd's disarm sink (`internal/disarm`) writes
> the `arm_override` and an `audit_logs` row attributed to the **credential + portal**
> (`actor_email: entry-disarm`), not to an operator. An operator remote `cmd.grant`
> carries no credential and therefore never disarms.

## Presets

The operator-management UI offers **named presets** that tick capability boxes —
they are a UI convenience only; *nothing about a preset is stored*, keeping
`permissions` the single source of truth (an operator whose set matches no preset
shows as "Custom"):

| Preset | Capabilities |
|---|---|
| Read-only | *(none)* |
| Enrollment | `enroll` |
| Door Ops | `command`, `policy` |
| Facilities | `topology` |
| Admin | all five |

## Privilege-escalation guard

Changing a user's `permissions` is itself gated beyond the `users` update rule: a
hook in [`internal/changelog`](../internal/changelog/changelog.go) rejects any
update that alters `permissions` unless the actor is a superuser or holds the
`operators` capability — so an operator who can edit their own profile (self-update
is allowed) still cannot grant themselves new capabilities.

## Control-plane audit log (`audit_logs`)

Every operator edit to a policy record is recorded in the **`audit_logs`**
collection by [`internal/changelog`](../internal/changelog/changelog.go). This is
the operator-edit counterpart to [`internal/audit`](../internal/audit) (which
records *door* activity from JetStream into `events`) — the two are complementary
and disjoint.

**What's recorded.** API-driven create / update / delete on the audited collections —
`cardholders`, `credentials`, `holidays`, `locations`, `schedules`, `controllers`,
`portals`, `access_groups`, `roles`, `aux_input`, `aux_output`, `users` — plus
operator **logins** (auth events on `users`; superusers excluded).

**What's excluded by construction.** The hooks are PocketBase `*Request` hooks,
which fire only for **API-driven** operations. accessd's own programmatic
`app.Save()` writes — controller heartbeats, the `events`/`point_status`
projections, the KV mirror — never trigger them, so machine churn is excluded
without an allowlist dance. `events`, `point_status`, and `audit_logs` itself are
also excluded. (`controllers` is safely audited *because* heartbeat updates take
the programmatic path, not the API.)

Each row carries:

| Field | Source |
|---|---|
| `event_type` | `create` · `update` · `delete` · `auth` |
| `collection_name`, `record_id` | the affected record |
| `actor_id`, `actor_email`, `actor_collection` | the authenticated operator (or superuser) |
| `request_ip`, `request_method`, `request_url` | the request origin |
| `timestamp` | when the row was written |
| `before`, `after` | full field snapshots (JSON) — **`password` and `tokenKey` are stripped** |

**Fail-safe & non-blocking.** The audited operation has already committed before
the row is written, so an audit-write failure is logged and swallowed, never
propagated to the operator.

**Retention.** When `accessd.auditRetentionDays` is positive, a daily 03:00 cron
deletes rows older than that many days, in bounded batches. The default is **365**
(`0` normalizes to 365 in config); set a **negative** value to disable pruning and
keep audit history forever. See [`configuration.md`](configuration.md#accessd).

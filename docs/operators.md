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
- [Non-operator auth tiers](#non-operator-auth-tiers) — why the read floor names `users`
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
operational collection. "Operator" here means a record in the **`users`**
collection specifically — the floor is scoped to that collection, not to "any
authenticated request", so a second auth tier does not inherit it (see
[Non-operator auth tiers](#non-operator-auth-tiers)). Only **writes and commands**
are gated, each by one capability:

| Capability | Grants |
|---|---|
| `enroll` | write **people** — cardholders, credentials |
| `policy` | write **access logic** — roles, access_groups, schedules, holidays |
| `topology` | write **hardware** — locations, controllers, portals, aux_input, aux_output |
| `command` | issue **commands** — grant, posture, aux-output drive, area arm/disarm |
| `operators` | manage **operator accounts**, read the **audit log**, and **hard-delete** structural records |

The five names are constants in [`internal/authz`](../internal/authz/authz.go)
(`CapEnroll`/`CapPolicy`/`CapTopology`/`CapCommand`/`CapOperators`). They are the
operator's whole authorization surface — there is no role field to drift out of
sync with the permissions.

### Cardholder photos (PII)

`cardholders.photo` (migration
[`1750000029`](../pbmigrations/1750000029_cardholder_photo.go)) is the first real
PII in the schema, and it follows the read floor above: **every operator can see
every photo**, with no capability gate. That is deliberate — a guard verifying a
face at a desk needs it, and gating it would make the badge and alarm views
inconsistent per-operator. Two consequences worth knowing:

- The field is a **protected** file, so its URL carries no implicit authorization:
  PocketBase requires a short-lived file token (`pb.files.getToken()`, wrapped by
  the UI's `useFileUrl`). Unlike an ordinary PocketBase file URL, a pasted link does
  not work for someone without a session.
- Photos live in `pb_data/storage`, so **backups grow** with the cardholder
  population, and dropping the field does not delete the stored files.

The photo is never mirrored to NATS KV — `policykv.User` carries only status and
roles — so it never reaches a leaf node.

## What each capability gates

Two enforcement points share `users.permissions`:

**1. Collection CRUD** — PocketBase collection rules (the real boundary), set by
migration [`1750000016`](../pbmigrations/1750000016_operator_permissions.go).
List/View are open to any authenticated operator
(`@request.auth.collectionName = "users"`, set by migration
[`1750000027`](../pbmigrations/1750000027_operator_read_floor.go)) except where
noted; the table shows the **write** rules:

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
| `GET /api/models` | any operator | enum/options metadata for the UI |
| `POST /api/simulate` | any operator | access simulator — a decision oracle; operator-only |
| `POST /api/badge/visitors` | `enroll` | mint a visitor: cardholder + time-bound credential + badge login |
| `POST /api/badge/visitors/{id}/revoke` | `enroll` | end a visit: revoke the pass, keep the login |
| `POST /api/badge/holders` | `enroll` | issue (or re-issue) a **staff** badge login for an existing cardholder |

The badge tier has three routes of its own, gated by the **`badge_users`** collection
rather than by capability — see [Non-operator auth tiers](#non-operator-auth-tiers):

| Route | Who | Purpose |
|---|---|---|
| `GET /api/badge/me` | a badge holder | their own badge: photo, QR, doors |
| `POST /api/badge/unlock/{id}` | a badge holder | remote unlock, authorized by `policy.Decide` |
| `POST /api/badge/password` | a badge holder | set or change their own password |

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
| Command Ops | `command`, `policy` |
| Facilities | `topology` |
| Admin | all five |

## Non-operator auth tiers

stone-access has more than one auth collection. `users` is the **operator** tier
this document describes; `_superusers` is the break-glass admin; `badge_users`
(migration [`1750000030`](../pbmigrations/1750000030_badge_users.go)) is the
**badge tier** — cardholders and visitors who sign in to view their own badge and,
where permitted, remotely unlock a door. A badge record is *not* an operator: it
holds no `permissions` field and must never read the policy graph.

Keeping those tiers apart takes two deliberate choices, both easy to get wrong:

1. **Collection read rules name the collection, not "any auth."** The floor is
   `@request.auth.collectionName = "users"`, not `@request.auth.id != ""`. The
   latter is auth-collection-*agnostic*: every badge holder would satisfy it and
   inherit operator read on the whole graph — including `credentials`, whose
   `value` field is the credential secret in plaintext. `TestReadFloorExcludesNonOperatorAuth`
   is the regression test, and it deliberately uses a *throwaway* auth collection so
   the guarantee holds for tiers added later.
2. **Custom routes use `authz.RequireOperatorAuth()`, not bare `apis.RequireAuth()`.**
   Bare `RequireAuth()` admits any auth collection. `POST /api/simulate` is the
   sharp case: it is a **decision oracle** over the entire policy graph, so exposing
   it to the badge tier would be worse than exposing the collections. Note that
   `apis.RequireAuth("users")` *alone* is also wrong — PocketBase's check is plain
   collection-name membership with no superuser exemption, so it would lock out the
   break-glass account. `RequireOperatorAuth` names both.
3. **A self-scoped update rule still needs a field guard.** `badge_users` lets a holder
   PATCH their own record (`id = @request.auth.id || …`), which is reasonable — but a
   collection rule selects which **records** may be written and says nothing about which
   **fields**, and there is no rule syntax that expresses "this record, but not these
   columns". Without a further guard, "may edit my own record" means "may edit every
   non-system field on it", and three of them are load-bearing:

   | Field | What editing it would buy |
   |---|---|
   | `cardholder` | **whose** credentials `/api/badge/me` and `/api/badge/unlock` resolve — repoint it at anyone and inherit every door they can open |
   | `kind` | whether the QR carries the credential value (`visitor`) or an inert identifier — flipping it turns a lanyard badge into a real key |
   | `password_set` | whether a password change must prove the current one — clearing it lets a stolen session lock the real holder out |

   [`badgeapi.RegisterGuards`](../internal/badgeapi/guards.go) rejects those changes from
   anyone but a superuser or an `enroll` operator, mirroring the privilege-escalation
   guard below. It binds a `*Request` hook, so it constrains API traffic only — accessd's
   own `app.Save` writes (the issue and password routes, the visitor mint) set these
   fields deliberately. It is registered at startup rather than in `OnServe`, because the
   collection API is served whether or not the badge routes ever came up — as is the
   visitor-pass revocation hook bound alongside it, for the same reason.

Reading `badge_users` is on the ordinary operator floor: any operator may see whether a
person has a login, and `enroll` is what it takes to change one. A badge holder still
sees only their own row — the rule is `id = @request.auth.id || @request.auth.collectionName = "users"`,
and the second clause can never match a badge token. Nothing in the row is secret (email,
`kind`, `password_set`, the cardholder id; the password hash and token key are system
fields the API never serialises), and gating *read* only bought a UI that had to hide the
field from operators without `enroll` — a blank space that cannot say "not allowed to
look".

Badge-tier routes live in `internal/badgeapi` and are authorized by
`policy.Decide` (what that person's own credential opens, right now) rather than by
capability — so a remote unlock can never exceed the holder's physical access.

### Issuing a badge login

The two kinds of badge are created by different flows, because they start from
different places:

| | **Visitor** | **Staff holder** |
|---|---|---|
| Where | **Visitors → New Visitor Pass** | **Cardholder form → Badge login** (a checkbox) |
| Route | `POST /api/badge/visitors` | `POST /api/badge/holders` |
| Creates | cardholder + time-bound credential + login, in one transaction | *only* the login — the cardholder and credential already exist |
| Access from | a curated `visitor_preset` role, chosen at mint | the roles already on that cardholder |
| QR encodes | the credential value (works at a scanner) | the cardholder id (opens nothing) |
| Usual sign-in | emailed one-time code | password |
| Ending it | **Revoke** (pass dies, login kept) or **Delete** (both) | untick the checkbox — the credential is untouched |

A staff badge is deliberately *not* created by enrollment: most cardholders never need
a login, and issuing one to everybody would put a phone-openable surface on people who
only ever tap a card. It is one checkbox on the person's own form, because a login is
1:1 with a cardholder — a property of that person rather than a related entity.

**For a staff holder, a badge login is not access.** It controls who may *see* a badge
and use remote unlock. Their credentials work at every door they are entitled to whether
or not a login exists, and removing a login revokes nothing. To actually revoke access,
set `credentials.status = revoked` (or suspend the cardholder) — that is what propagates
through the mirror to the edge. Issuing and removing both need `enroll`: giving a login
and taking it back are the same act ([`1750000035`](../pbmigrations/1750000035_badge_login_lifecycle.go)).

**A visitor pass is the exception, and it is not an oversight.** A visitor's login and
credential are minted together, in one transaction, for one visit — they are two records
holding one thing. So deleting a visitor login **revokes their credential**, enforced by
a hook in [`internal/badgeapi`](../internal/badgeapi/visitors.go) rather than by the UI,
so it holds from the Visitors page, the PocketBase admin UI, and a cardholder delete
cascading into the login alike. Ending a visit early has its own verb — **Revoke** —
which kills the pass and keeps the login, so the visit stays on the record and a repeat
visitor is recognised rather than duplicated.

> Before this, deleting a visitor login left the credential `active` *and* removed it
> from [`internal/badgesweep`](../internal/badgesweep)'s view, since the sweep finds
> credentials to expire by enumerating visitor logins. The one action an operator would
> take to end a visit both failed to end it and disabled the job that eventually would.

**A login with no credential** signs in and honestly reports that no pass has been
issued. Roles and effective access are not enough on their own; the cardholder page says
so where the fix is one click away.

### Sign-in methods

All three are enabled on `badge_users`
(migrations [`1750000030`](../pbmigrations/1750000030_badge_users.go) and
[`1750000034`](../pbmigrations/1750000034_badge_password_auth.go)):

| Method | For | Needs SMTP |
|---|---|---|
| **Password** (email + password) | staff holders, who sign in for years | no |
| **One-time code** (emailed) | visitors; anyone who has not set a password | **yes** |
| **OAuth2** | staff with an existing identity; providers are configured in the PocketBase admin | no |

> **Without SMTP, a password is the only way in.** OTP and the forgot-password link are
> both emails. On an install with no mail server, set an initial password when you issue
> the login and hand it over in person — otherwise the badge tier is inert.

An initial password is **optional and never emailed**: mail is stored indefinitely,
forwarded, and synced to devices, so a door-opening password sent by mail would outlive
every other control around it. The invite mail says only *where* to sign in.

A holder sets or changes their own password from the badge page. The `password_set`
flag records whether they have one, which decides whether the current password must be
supplied to change it: a holder who signed in by one-time code is setting a *first*
password and has nothing to prove, while one who already has a password must supply it
so that a stolen session cannot silently lock them out of their own badge. Setting a
password from the cardholder form replaces the existing one — the operator's path for
someone who is locked out and has no working mail. Note that **changing a password signs out every
device**, including the one making the change (PocketBase rotates the record's token
key); the badge UI re-authenticates silently, so this is only visible on a holder's
other phones.

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

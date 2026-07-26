# Demo data

Two files that turn a fresh `accessd` into something worth showing: a believable
multi-site company, and a live trickle of door activity.

| File | What it is |
|---|---|
| [`seed.ps1`](seed.ps1) | Idempotent PowerShell seed — sites, controllers, portals, an armed area, roles, people, credentials, badge logins, visitor passes, and a backfill of recent events plus unacknowledged alarms. |
| [`access-demo.yaml`](access-demo.yaml) | [rule-router](https://github.com/stone-age-io) scheduler rules that publish synthetic events on a cron, so the Overview and Alarm Console keep moving during a demo. |

Both are **dev/demo tooling**, not part of either binary. Nothing here ships to a
customer install, and the passwords below are deliberately weak and well-known.

## Seeding

Needs the `pb` CLI authenticated as a superuser against the target instance:

```bash
pb context select <your-context> && pb auth status
```

Then, from the repo root:

```bash
pwsh demo/seed.ps1
```

It builds on top of the base fixture (`pbmigrations/1750000001_fixture.go`), so run it
against a database where migration `1750000001` seeded `location hq` — a fresh `accessd`
data directory. It reports `+` created, `=` already present, `!` failed, and a re-run
should report `created=0 failed=0`.

> **Re-running is safe; running against production is not.** Every record is looked up by
> its natural key first, so repeating a partial run is harmless. But this creates people
> with working credentials — check `pb context list` before you run it.

### What you get

| | |
|---|---|
| Sites | `hq` (from the fixture), `dc` Distribution Center, `east-office` East Coast Office |
| Intrusion | area `dc-warehouse`, armed, with a motion point and a 24h glassbreak point |
| Automation | `east-lobby` auto-unlocks during office hours; `dc-main-entrance` is `disarm_on_grant` |
| People | 13 employees + contractors across 6 roles, one suspended |
| No-inbox cards | "Loading Dock Spare Card" and "Fire Dept Lockbox" — cardholders with no email, which cannot sign in by any method |
| Visitors | three passes, one live, one expired, one revoked |
| Area rights | `ag-warehouse` grants arm **and** disarm on `dc-warehouse`; `ag-cleaning` grants **arm only** |
| Aux output on a badge | `ag-relays` grants the East gate strike (Facilities and Security hold it) |
| Remote opt-ins | unlock on `east-lobby` + `hq-east-stair`; arm/disarm on `dc-warehouse`; the gate strike; floor plan on `east-office` |
| Operator's own badge | `admin@local.dev` is linked to Sarah Chen's cardholder, so "My badge" in the profile menu works |
| History | 9 taps and 3 unacknowledged alarms, backdated minutes to hours |

The arm-rights asymmetry is the part worth looking at: the cleaning crew can arm the
warehouse when they finish and **cannot** disarm it. That is why arm and disarm are
separate rights rather than one checkbox, and it is what `deny_no_area_right` reports when
a group has areas but no rights at all.

The floor plan on badges needs an image: `east-office` is opted in, but nothing renders
until you upload a floorplan to that location and place a portal or two on it.

### Signing in

The seed prints these when it finishes:

| Tier | Identity | Secret |
|---|---|---|
| Operator console (`/login`) | `admin@local.dev` | `changeme123` (from fixture `1750000010`) |
| Badge — staff (`/login?as=badge`) | `sarah.chen@stoneage.example`, `marcus.johnson@…`, `priya.patel@…`, `emily.rodriguez@…` | `badge-demo-1234` |
| Badge — fixture cardholder | `alice@example.com` | `changeme123` |
| Badge — visitor | `dana.whitfield@acme.example` | emailed one-time code — **needs SMTP** |

The other nine employees have **no** badge login, which is the realistic default: most
people in a PACS only ever tap a card. `david.kim@stoneage.example` is the one to try if
you want to watch the auth rule refuse a correct password.

## Live event feed

`access-demo.yaml` publishes straight to the audit subtree, so **no controller needs to be
running** — `accessd`'s always-on audit consumer projects the messages into `events` by
exactly the path a real controller's events take.

```bash
rule-router --config config/rule-scheduler.yaml   # features.scheduler: true
```

Its NATS connection must point at the same server and account as `accessd`, and be allowed
to publish `acc.>`. Copy the file into rule-router's rules directory (e.g.
`rules/scheduler/access-demo.yaml`).

Every location code, portal code, and credential value in it is one `seed.ps1` creates, so
**seed first** or the events will reference records that do not exist. They still project —
the `events` collection is a denormalized snapshot of text — but the UI will link nowhere.

> Alarms project as **unacknowledged and accumulate** until someone acks them in the Alarm
> Console. That is the point during a demo, and a nuisance if you leave the scheduler
> running for a week: widen the alarm crons, or ack them.

## Keeping these in step with the schema

`seed.ps1` writes through the REST API, so it is subject to the same rules and validators
as the UI. Two things about the current schema bite hardest, and both are commented in the
script where they matter:

- **`cardholders` is an auth collection.** Every create must carry `password` +
  `passwordConfirm`; PocketBase's record-create request form requires them on any new auth
  record, before any server hook can fill them in. `New-Cardholder` handles it.
- **Being an auth record is not being an account.** The collection's auth rule requires
  `badge_login`, so the people seeded without it cannot sign in whatever is stored.

If a migration adds a required field or a validator, this script is the thing most likely
to break silently — it prints `!` and a message per failed record, so check the summary
line rather than assuming a clean run.

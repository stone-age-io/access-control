# Live demo activity (rule-router)

[`northwind-access.yaml`](northwind-access.yaml) keeps the `accessd demo-seed`
estate moving: badge taps at all ten portals across the three sites, operator
door-pops, a nightly gate lockdown, yard lighting, a trickle of alarms, and a
weekly fire drill.

It **drives real controllers**. Taps are published to the reader subject
`acc.{location}.{type}.{thing}.tap`, so a running `access-controller` decides
each one itself and emits the resulting event. The reason codes you see are the
ones `policy.Decide` produced, not strings someone typed into YAML — edit an
access group in the console and the next tap changes.

## Run it

Four processes plus the scheduler, all on one NATS server and **one account**.

```bash
./accessd migrate up
./accessd demo-seed --confirm
./accessd serve
```

One controller per seeded panel:

```bash
SA_CONTROLLER_CODE=ctrl-kc-dc1-1    SA_CONTROLLER_LOCATION=KC-DC1    ./access-controller
```

```bash
SA_CONTROLLER_CODE=ctrl-kc-dc1-2    SA_CONTROLLER_LOCATION=KC-DC1    ./access-controller
```

```bash
SA_CONTROLLER_CODE=ctrl-kc-office-1 SA_CONTROLLER_LOCATION=KC-OFFICE ./access-controller
```

```bash
SA_CONTROLLER_CODE=ctrl-sgf-xd2-1   SA_CONTROLLER_LOCATION=SGF-XD2   ./access-controller
```

The defaults are already right for a demo: `controller.driver: mock` (no I/O)
and `controller.reader: nats` (taps arrive over NATS). Then:

```bash
rule-router --config config/rule-scheduler.yaml --rules demo/rules
```

`features.scheduler: true` is the only feature this file needs.

## Credentials — where the join actually pays off

On a plain dev `nats-server` with no auth, none of this matters. On a Stone Age
platform deployment it does, and it is worth doing properly because it is the
part that shows the two apps are one system.

The platform's `demo-seed` creates a **Thing for every controller** — codes
`ctrl-kc-dc1-1`, `ctrl-kc-dc1-2`, `ctrl-kc-office-1`, `ctrl-sgf-xd2-1`, the same
four this app seeds as `controllers` rows — and mints each one a signed NATS
credential under the `gateway` role. **Give each controller its own Thing's
credential.** Download it from the platform console (**NATS → Users**, pick the
username matching the controller code) and point the box at it:

```bash
SA_NATS_CREDSFILE=/path/to/ctrl-kc-dc1-1.creds \
SA_CONTROLLER_CODE=ctrl-kc-dc1-1 SA_CONTROLLER_LOCATION=KC-DC1 ./access-controller
```

That is the inventory doing its job: the platform is where the device is
enrolled and where its identity is issued, and stone-access is what the device
then does. A revoked Thing is a controller that stops working, with no second
place to go and revoke it.

`accessd` itself has no Thing, so give it a **`console-operator`** credential for
the Northwind org (`console-dana`, `console-raj`) — it needs to create the
`ACC_POLICY`/`ACC_STATUS` buckets and the `ACC_EVENTS` stream, which no
device-shaped role grants. Worth knowing that this is a gap rather than a design:
a central app is a participant too, and it ought to be enrolled like one.

rule-router needs **publish on `acc.>`**, which the same `console-operator`
credential covers. It stands in for many readers at once, so no single device
role fits it.

## What you should see within a minute

| Where | What |
|---|---|
| Events | a steady mix of `allow_grant`, `allow_posture_unlocked`, and five different denial codes |
| Alarm Console | a forced door on Dock A, and a held-open on the DC entrance that clears itself two minutes later |
| Monitor | strikes pulsing, the Springfield dock disarming on the morning open, the yard gate under lockdown after 20:00 |
| Controllers | four boxes online, heartbeating every 15s |

## Things the file is deliberately doing

**Publish mode is set per action, not inherited.** `mode: core` for anything a
controller consumes — taps and `cmd.*` have no JetStream stream covering them, so
a jetstream publish would hang waiting for an ack that never comes. `mode:
jetstream` for the three injected alarms, where the ACC_EVENTS ack is the only
proof the event was captured.

**The alarms are the one thing not driven for real.** A forced or held-open door
is observed on a DPS input and `driver: mock` has none, so those three are
published as finished `evt.alarm` events. Run a controller with `driver: gpio`
on real hardware and you can delete that section.

## Expect a `no_entry` alarm after every allow

Under `driver: mock` each granted tap is followed 10–20 seconds later by a
`no_entry` alarm — "access granted, nobody came through" — so at these rates the
Alarm Console fills with a few hundred an hour and buries the forced, held and
intrusion alarms this file injects on purpose.

That is not the rules' doing. `sweepNoEntry` gates on whether the portal's
*policy binding* declares a `dpsInput`, and the seeded portals all do; but the
mock driver supplies no door input, so an open is genuinely unobservable and the
grace window always expires unused. The gate checks that a contact is
**configured** rather than that one is **observable**, and those two only diverge
under the mock driver — which is precisely where the demo lives. The code comment
above that function describes the failure exactly ("would report `no_entry` on
EVERY grant") while gating on the wrong thing.

Until that is fixed, either filter the Alarm Console to `forced` / `held` /
`intrusion`, or narrow the allow-tap crons. Notification is unaffected —
`no_entry` is opt-in there and off by default, so nobody gets emailed.

**Business hours live in the cron, not in conditions.** Cron is 5-field with a
one-minute floor, and every rule carries `timezone: America/Chicago`. That makes
the window exact and visible on the line you are reading.

**The fire drill both asserts and clears.** `evt.fire` is edge-triggered; a rule
that only ever asserts leaves KC-DC1 latched in fire state, which at that site
(`fai_suppress`) silently suppresses every door alarm from then on.

## Turning it down

Peak is roughly four or five taps a minute on a weekday shift. Widen the busiest
crons (`*/2` → `*/6`) to calm it. Alarms project **unacknowledged and
accumulate** until someone acks them in the Alarm Console — fine for an hour,
a nuisance over a week.

## Two things that expire

Sandra Okonkwo's visitor pass is valid from the day before the seed run to three
days after it, so after day three her taps start coming back `deny_expired`
alongside Dale's. Re-seeding does not refresh the dates (the seed is
found-or-create, and re-running tops up rather than rewriting), so extend the
pass on the **Visitors** page or accept the extra denial.

`deny_not_yet_valid` is the one reason code this file never produces. It needs a
credential with a future `valid_from`, which the seed does not create — a pass
nobody can use yet is invisible on every screen. Mint one with tomorrow's start
date and add a rule if you want the complete set.

## Relationship to `demo/access-demo.yaml`

That file is the older one, written for `demo/seed.ps1`'s company (`hq`, `dc`,
`east-office`) and for a deployment with **no controller running**: it injects
finished `evt.tap` events straight into the audit subtree. It still works and is
still the right tool when you want a moving event feed and nothing else.

This one is the opposite trade: it needs four controller processes, and in
exchange nothing about the decision is fabricated. Point `--rules` at `demo/`
and you would load both, against two different companies — point it at
`demo/rules` for this one alone.

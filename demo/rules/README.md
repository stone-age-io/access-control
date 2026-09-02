# Live demo activity (rule-router)

[`northwind-access.yaml`](northwind-access.yaml) keeps the `accessd demo-seed`
estate moving: badge taps at all ten portals across the three sites, operator
door-pops, a nightly gate lockdown, yard lighting, the dock horn and the weekly
siren test, intrusions in all three armed areas, a trickle of alarms, and a
weekly fire drill.

Every portal code, output code, area code, detector code and credential value in
it is one the seed creates — including the fire-lockbox card and the two aux
outputs that nothing used to drive.

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
| Events | a steady mix of `allow_grant`, `allow_posture_unlocked`, and five different denial codes — under **half a dozen different names**, not one repeated |
| Alarm Console | a forced door on Dock A, a held-open on the DC entrance that clears itself two minutes later, and overnight intrusions in three different areas |
| Monitor | strikes pulsing, the Springfield dock disarming on the morning open, the yard gate under lockdown after 20:00, the dock horn on a pulse |
| Controllers | four boxes online, heartbeating every 15s |

A minute is now generous: taps run on a seconds-granularity cron, so the Events
screen is visibly moving within about twenty seconds of the scheduler starting.

## Things the file is deliberately doing

**Publish mode is set per action, not inherited.** `mode: core` for anything a
controller consumes — taps and `cmd.*` have no JetStream stream covering them, so
a jetstream publish would hang waiting for an ack that never comes. `mode:
jetstream` for the injected alarms, where the ACC_EVENTS ack is the only
proof the event was captured.

**The alarms are the one thing not driven for real.** A forced or held-open door
is observed on a DPS input and `driver: mock` has none, so they are
published as finished `evt.alarm` events. Run a controller with `driver: gpio`
on real hardware and you can delete that section.

**No `no_entry` alarms under mock hardware, by design.** A `no_entry` — "access
granted, nobody came through" — needs an open to be *observable*, which takes both
a `dpsInput` on the portal's binding and a door-input driver on the controller.
`driver: mock` has the first and not the second, so the controller stays silent
rather than raising one per granted tap. If it did, the Alarm Console would take
a few hundred an hour and bury the forced, held and intrusion alarms this file
stages on purpose. Run `driver: gpio` on real hardware and they become real.

**Business hours live in the cron, not in conditions.** Every rule carries
`timezone: America/Chicago`, which makes the window exact and visible on the line
you are reading. Cron now takes an **optional leading seconds field**, so read
the field count before assuming a `*/20` is minutes: `*/20 * 5-21 * * 1-6` is
every twenty seconds during warehouse shift.

**Which card taps is a `{@random.choice(...)}`; which denial fires is not.** An
allow-tap rule picks among the cards that are *all* granted at that portal on
that schedule, so one rule produces a mix of names instead of one name repeated.
The denial rules stay one card each on a minute cron, because the point of that
section is that each reason code arrives at a **known rate from a known cause** —
folding six causes into one choice would randomise the rate along with the name.
The cron sets the rate; random sets who.

The sets are read straight off `internal/demoseed/data.go` (a role holds groups,
a group holds portals and a schedule) and must not be widened casually: adding a
card that is not on the group turns a staged allow into a `deny_no_access` that
nobody staged. Glen (suspended) and Brett (revoked card) hold the warehouse role
and are deliberately absent from every allow set for exactly that reason.

**The fire drill both asserts and clears.** `evt.fire` is edge-triggered; a rule
that only ever asserts leaves KC-DC1 latched in fire state, which at that site
(`fai_suppress`) silently suppresses every door alarm from then on.

## Requirements

A rule-router build with **six-field cron** and the **`{@random.*}` template
functions**. On an older one every rule in the file fails to load, loudly, at
startup.

## Turning it down

Peak is roughly **ten taps a minute** on a weekday shift — enough that the Events
screen fills in seconds rather than in a quarter of an hour, and more than you
want running for a week. Widen the busiest crons (`*/20` → `*/90`), or drop the
seconds field entirely to go back to minute granularity.

Alarms project **unacknowledged and accumulate** until someone acks them in the
Alarm Console. They were deliberately left on minute crons and single hard-coded
values while the taps got faster, because their rate is the number a reader most
needs to be able to predict from the cron alone.

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

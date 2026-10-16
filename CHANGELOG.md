# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html), with the pre-1.0
caveat that a minor version may break something. Pin what you deploy, and run
the same version of `accessd` and `access-controller`.

History before `0.1.0` is not reconstructed here. About 140 commits of
pre-release development (June to October 2026) preceded it; `git log` is the
record for that period, and this file starts where the versioned releases do.

## [Unreleased]

## [0.2.0] - 2026-10-15

### Added

- **A controller answers health requests over NATS.** A request on
  `acc.{location}.ctrl.{code}.health` returns the box's live state: the local
  status page's report without its recent decisions, which carry raw
  credential values. That is identity, build, NATS and policy health, its
  portals and aux points, fire suppression and recent door alarms. It is
  read-only and always on, and it is the `health` endpoint of the
  `access-controller` discovery service, so `nats micro info` lists it. A
  field check no longer needs an SSH tunnel to the status page.
  **Upgrading:** give each controller's credential subscribe on its health
  subject. Without it the endpoint is refused and nothing else changes.

### Fixed

- **A maglock door is locked from boot, not from the first policy sync.** A
  maglock is locked only while its relay is energized, and a controller armed
  its portals only once policy arrived, so every maglock door was unlocked
  while a box booted, and for as long as NATS stayed down after a reboot
  unless the opt-in offline cache was on. After every live sync the controller
  now also writes a bindings snapshot (`policy.bindings.path`, default
  `./data/bindings.json`, always on): only its own portals, only the fields
  that arm hardware, no posture and no credentials. A box that boots without a
  usable cache arms those portals from it, which drives a maglock locked and a
  strike unpowered, while every tap still denies (`deny_point_disabled`).
  Until policy syncs or a full cache loads, `cmd.posture` and `cmd.grant` are
  refused (`refusing command: policy not synced`), so an unlocked override
  cannot open a door whose policy the box has never seen. The same boot drives
  a strike that a killed process left held open back to idle. When policy
  lands, the portals are not re-armed. The docs said a crash re-locked a
  strike; only a graceful stop writes the idle level, and the new
  [Physical state](docs/hardware.md#physical-state) table says which cases
  still need checking on real hardware.
  **Upgrading:** nothing to configure. The controller must be able to write
  `./data/` under its working directory (or wherever `policy.bindings.path`
  points); a failed write is logged and the box boots as it did before. The
  first snapshot is written at the first sync after the upgrade, so the boot
  after that is the first one that benefits.
- **The repository clones on Windows.** The console's aux I/O views lived in
  `ui/src/views/aux/`, and `aux` is a reserved device name on Windows, so a
  fresh `git clone` failed with `invalid path` unless `core.protectNTFS` was
  turned off. The directory is `ui/src/views/aux_io/` now. Routes and the
  built console are unchanged in behaviour.
- **A console built on Windows matches the one CI builds.** The console's
  source (`ui/`) and its committed build now check out with LF line endings
  everywhere. A component's style scope id is a hash of its raw source,
  carriage returns included, and `index.html` and `ui/public/` are copied into
  the build as they are, so a build from a CRLF checkout differed from CI's
  and failed the committed-console check. In an existing Windows checkout,
  delete those files and run `git checkout -- ui internal/webui/public` once
  to pick up the change.

## [0.1.0] - 2026-10-12

First tagged release. The system has been in development since June; this tag
marks the point at which it is packaged for other people to run, not the point
at which it started working.

### Security

Found by auditing the docs and the migrations against the code before tagging.
Each was on `main` before this release, so each matters to anyone who ran a
pre-release build.

- **A new install no longer gets a known-password operator.** The fixture
  migrations run on any database whose `locations` collection is empty, which
  is every new install, production included. They seeded the operator
  `admin@local.dev` / `changeme123` with every capability, and a badge login
  `alice@example.com` / `changeme123`. They now seed nothing that can sign in.
  `accessd operator upsert` creates the first operator, and
  `accessd demo-seed --confirm` creates `admin@local.dev` for the demo. **A
  database seeded before this release still has both accounts;** see
  Upgrading below.
- **A commanded lockdown did not stop a commanded grant.** An operator
  door-pop or a badge remote unlock pulsed the strike under a runtime lockdown
  that denied every card at the reader. A `cmd.grant` now passes the same
  posture gate as a tap, and a refused grant is recorded as a deny.
- **Entry-disarm accepted any card at a `free_access` or `unlocked` door.**
  Those postures allow without reading the credential, and the disarm sink
  checked only that something was presented. It now requires `allow_grant`,
  the one allow reached through the credential.
- **A credential revoked during a NATS outage stayed live at the edge** until
  `accessd` restarted, because the failed KV write was never retried. The
  mirror now re-syncs on every reconnect.

### Added

Summarising the state at first tag rather than the path to it:

- **Two binaries that share nothing but NATS.** `accessd` is the system of
  record: PocketBase, the embedded Vue console, a mirror of the policy graph to
  NATS KV (one key per record), and a JetStream audit projection.
  `access-controller` runs on each edge box: it watches that KV into memory,
  decides each tap locally, drives the locks and door inputs, and emits events.
  `docs/protocol.md` is the whole contract between them.
- **Decisions at the edge, failing closed.** The decision is a pure function
  (`policy.Decide`) over user → roles → access groups → portals, areas and aux
  outputs under one schedule, with holiday calendars and credential validity
  windows. Deny overrides come first, and anything unknown or not yet synced
  denies. Postures (secure, unlocked, free access, lockdown, disabled) are
  standing, scheduled, or overridden at runtime by command. An opt-in offline
  cache lets a controller rebooted with NATS down decide on last-known policy.
- **Real hardware.** Lock and door-input drivers for the KinCony Server-Mini
  (CM4, native GPIO) and Pi5R8 (CM5, MCP23017 over I2C), chosen by board model,
  with per-install wiring sense (NO/NC contacts, strike or maglock,
  REX-unlock). Readers are simulated over NATS, real OSDP over RS485 (a pure-Go
  control panel), or both per portal. Nothing needs cgo.
- **Door monitoring.** Forced, held-open and granted-but-no-entry alarms from a
  per-door state machine, plus controller heartbeats and online/offline events.
- **Intrusion-lite areas.** Arm and disarm are separate rights. Areas support
  scheduled auto-arm, entry-disarm on a valid badge (one-shot, so the next
  scheduled arm still happens), and intrusion alarms from motion or tamper
  inputs or a forced member door. A fire-alarm contact is an aux input that can
  suppress alarm noise at its location; egress stays in listed hardware.
- **One event stream.** Every tap, alarm, arm transition and controller
  liveness flip is a JetStream event, projected idempotently into the console's
  Events timeline and Alarm Console (acknowledge, bulk acknowledge, filters).
  Retention pruning is optional and off by default.
- **Notifications.** Opt-in email for alarms, fire and controller offline, by
  source and by operator (which types, which locations); a bounded re-page for
  alarms nobody acknowledges; and a webhook that POSTs each pageable event as
  JSON to whatever already routes your pages.
- **The console.** Locations with a map and floor plans, a live operational
  monitor, portals with printable placards, controllers with an I/O map,
  areas, aux I/O, schedules and holiday calendars, access groups, roles,
  cardholders, credentials, CSV import, and reports: denied access, who has
  access, as-built wiring, and an access simulator that runs the real
  `policy.Decide`. It installs as a PWA, and a runtime overlay rebrands it
  without a rebuild.
- **An auditable control plane.** Operators sign in against `users` and hold
  orthogonal capabilities (`enroll`, `policy`, `topology`, `command`,
  `operators`), enforced by collection rules and on every custom route. Every
  API-driven policy edit and operator login is written to `audit_logs`.
- **The badge.** Cardholders and visitors sign in to see their own pass (photo,
  QR, validity). Where an operator opts a door, area or relay in, they can
  unlock, arm or disarm, or pulse it from their phone, authorized by the same
  decision functions as a physical tap. Visitor passes are minted time-bound,
  reissued and revoked from the console, and an operator can preview any
  holder's badge read-only.
- **Observability.** Prometheus metrics on a side port (off by default), both
  binaries registered for NATS service discovery (`nats micro ls`), and an
  optional read-only `/status` page on each controller for field installs.
- **A demo estate.** `accessd demo-seed --confirm` builds Northwind Traders:
  three sites, four controllers, ten portals, four areas, fifteen cardholders
  and a backdated event history. `demo/rules/` holds rule-router rules that
  keep it busy, and `demo/README.md` describes both.

Added while preparing this tag, and therefore part of it:

- **`accessd operator upsert <email> <password>`.** Creates a console operator
  with every capability, or resets an existing one's password and grants it
  every capability, the way `superuser upsert` does for the PocketBase admin.
  It is how a new install gets its first operator and how a locked-out one gets
  back in.
- **`accessd --version` and `access-controller -version`.** Neither binary
  could report its build: `accessd` failed on the example config before
  PocketBase saw the flag, and the controller had no flag at all. Both now
  answer before loading config, since an unconfigured box is when someone asks.
  `accessd` also names the PocketBase it was built against. The version is the
  one `go build` stamps from the checkout's tag, the same value service
  discovery reports, so a release and a source build of one tag agree.
- **A CI workflow.** It rebuilds the console and fails when the committed
  `internal/webui/public` differs from the build, which is the first guard on
  the "rebuild and commit it" rule. It also checks gofmt and `go mod tidy`,
  vets on linux, windows and darwin, runs the tests, and builds the controller
  for linux/arm64.
- **A goreleaser config and release workflow.** Both binaries for linux, darwin
  and windows on amd64 and arm64 (no windows/arm64), each archive carrying its
  example config and the docs, with SHA-256 checksums. The release body is
  built from this file.
- **An MIT license.** The repository was public without one.

### Fixed

Also found in that audit:

- **The end of a fire alarm was paged as a fire.** The clear was emailed as
  "FIRE input active", forwarded by the webhook, and could be re-paged.
- **`policy.cache.enabled: true` alone did nothing**, because its path and
  maximum age defaulted to empty. Six keys also ignored their `SA_` variables
  unless they appeared in the config file.
- **Every controller rewrote every area's status every 10 seconds**, each one
  a database write in `accessd`.

### Known limitations

- **OSDP Secure Channel is not implemented.** Readers run in clear text, and a
  reader configured to require Secure Channel will not come online.
- **Under a runtime lockdown, a badge remote unlock says "Unlocked"** and the
  door stays shut: `accessd` answers before the controller refuses. The refusal
  is in the event log.
- **The access simulator and the Who Has Access report cover portals only**,
  not area or aux-output decisions.
- **There is no container image.** `accessd` needs a NATS server with JetStream
  that it does not run itself.

### Upgrading from a pre-release checkout

- **Delete the operator `admin@local.dev` and alice's badge login**, or at
  least change both passwords. A database seeded before this release keeps
  both accounts with the password `changeme123`; the fix above only stops new
  databases from getting them. Create your own operator first, with
  `accessd operator upsert`, so you are not locked out.
- **A database created before 2026-07-26**, when `cardholders` became an auth
  collection, cannot migrate onto this schema. Start from a fresh `pb_data`.

[Unreleased]: https://github.com/stone-age-io/access-control/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/stone-age-io/access-control/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/stone-age-io/access-control/releases/tag/v0.1.0

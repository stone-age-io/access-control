# stone-access

A standalone, NATS-native physical access control (PACS) app that dogfoods the
[Stone-Age.io](https://stone-age.io) platform — RBAC door control with schedules,
deny-override, and edge autonomy, composed from the platform's primitives
(NATS core, KV, JetStream, PocketBase control plane).

The authorization decision is a small **pure function** over an in-memory policy
graph (`internal/policy`), not a rules engine. The central app (`accessd`) is the
system of record (PocketBase) and mirrors policy to NATS KV one key per record;
edge controllers (`access-controller`) watch that keyspace and decide locally.

> v1 status: the reader is selectable per controller (`controller.reader`) — a
> **simulated NATS reader** (default; taps arrive over NATS, for dev) or a real
> **OSDP reader** on the model's RS485 bus (pure-Go, no cgo, clear-text in v1;
> Secure Channel is a fast-follow). The **lock and door inputs have real drivers**
> alongside the mocks: native GPIO (`internal/drivers/gpio`, KinCony Server-Mini /
> CM4) and MCP23017 over I2C (`internal/drivers/i2c`, KinCony Pi5R8 / CM5). Door
> monitoring (forced / held-open) and controller heartbeat/health are implemented.

## Docs

- [`docs/protocol.md`](docs/protocol.md) — the NATS wire contract: subjects, KV
  shapes (`ACC_POLICY` + `ACC_STATUS`), decision reason codes, audit projection.
- [`docs/configuration.md`](docs/configuration.md) — every config key, default,
  and `SA_` env override for both binaries.
- [`docs/operators.md`](docs/operators.md) — the control-plane access model: operator
  sign-in, capabilities, collection-rule matrix, and the `audit_logs` change log.
- [`docs/hardware.md`](docs/hardware.md) — physical I/O: supported boards, pin
  maps, relay/input polarity, transports, and how to add a board.
- [`demo/README.md`](demo/README.md) — dev/demo tooling: an idempotent seed script for a
  believable multi-site company, and rule-router rules that keep the event feed live.

## Layout

```
cmd/accessd/            central: PocketBase + KV mirror publisher + audit consumer + controller-health monitor
cmd/access-controller/  edge: policy watcher + pure decision + drivers + door monitoring + heartbeat + optional /status page
internal/policy/        the pure core: Policy types, Decide(), windowOpen()
internal/controller/    PolicyStore (KV watch → maps), tap loop, door state machine, portal/lock arming, commands, heartbeat
internal/drivers/       ReaderDriver / LockDriver / DoorInput / FAIInput interfaces + mocks (MockHardware)
internal/drivers/hardware/  per-model hardware Profile: logical relay/input index → physical line + transport
internal/drivers/gpio/  native GPIO lock + door-input backend (go-gpiocdev, no cgo; Linux only)
internal/drivers/i2c/   MCP23017 lock + door-input backend over I2C (periph.io, no cgo; polled inputs)
internal/drivers/osdp/  OSDP reader: RS485 CP engine (pure-Go, no cgo) + wire codec (osdp/wire); controller.reader: osdp
internal/diag/          opt-in, read-only local /status page of an access-controller's live state (field troubleshooting)
internal/health/        accessd-side heartbeat subscriber → controllers.last_seen/status
internal/authz/         operator capability checks for accessd's custom HTTP routes (commandapi, modelsapi)
internal/commandapi/    UI→NATS command bridge (grant/posture/aux output), gated by the `command` capability
internal/modelsapi/     GET /api/models — enum/options metadata for the UI
internal/simulateapi/   POST /api/simulate — the access simulator; a decision oracle, so operator-only
internal/badgeapi/      the badge tier: a holder's own badge + remote unlock/arm/pulse, and the operator
                        routes that mint a visit and read a holder's badge for troubleshooting
internal/badgesweep/    marks expired visitor credentials revoked — hygiene, not enforcement
internal/policysnapshot/ point-in-time snapshot of ACC_POLICY, shared by the simulator and the badge tier
internal/mirror/        PocketBase record hooks → one ACC_POLICY KV key per record (+ boot reconcile/prune)
internal/policykv/      the wire contract: KV key scheme + JSON shapes shared by mirror and PolicyStore
internal/subjects/      every NATS subject is built and parsed here — never hand-formatted elsewhere
internal/notify/        alarm/fire email sink (a second ACC_EVENTS durable); inert until opted into
internal/disarm/        entry-disarm sink: a valid grant at a `disarm_on_grant` portal disarms its area
internal/armrelease/    releases a one-shot disarm override once a scheduled area's base state is disarmed
internal/status/        upward device shadow: ACC_STATUS → point_status projection
internal/changelog/     control-plane audit log: API-driven policy edits + logins → audit_logs collection
internal/audit/         JetStream consumer → PocketBase events collection
internal/natsx/         NATS connection + KV helpers
internal/webui/         the compiled management UI, //go:embed-ed into accessd
pbmigrations/           PocketBase collections (schema-in-code)
ui/                     Vue 3 + Vite management UI source (PocketBase-backed CRUD)
demo/                   dev-only: seed.ps1 (demo data) + access-demo.yaml (rule-router event simulator)
```

## Web UI

`accessd` serves a Vue 3 management console (locations + a location map, schedules,
portals, controllers, areas, aux I/O, access groups, roles, cardholders, credentials,
visitor passes, an events timeline, an alarm console, reports, a live operational monitor,
operator management, and the control-plane audit log) at `/`. It is
compiled into `internal/webui/public` and **`//go:embed`-ed into the accessd
binary** — there is no `pb_public` directory to ship; the binary is
self-contained.

Operators sign in against the built-in `users` auth collection; their abilities are
an orthogonal set of capabilities (`enroll`/`policy`/`topology`/`command`/`operators`)
that gate writes and commands while reads stay open to any authenticated operator —
see [`docs/operators.md`](docs/operators.md). A PocketBase **superuser**
(`accessd superuser upsert <email> <pass>`) is the break-glass account and also
signs into the admin UI at `/_`.

There is a **second, much smaller surface for the people the system is about**: a
cardholder or visitor signs in at `/login?as=badge` and sees one page — their badge
(photo, QR, validity) and what it grants. Where an operator has opted the door, area, or
relay in, they can also open it, arm/disarm it, or pulse it from their phone; every such
action is authorized by the same pure decision function the edge runs, so a badge can
never do remotely what it could not do in person. `cardholders` is itself the auth
collection for this tier — one person is one record whether or not they ever sign in — and
`docs/operators.md` covers the boundary between the two tiers.

That page is built as a **phone screen, not a document**: a fixed-height shell that never
scrolls as a whole, its long lists grouped by building and bounded, a light/dark toggle and
an account menu in the header, and 44px-minimum tap targets throughout. The whole UI is an
**installable PWA** (`ui/public/manifest.json`), which matters most here — a badge you tap
an icon for beats one you find a bookmark for. Its service worker caches **nothing** on
purpose: this app's job is to say what a badge opens *right now*, and offline resilience
belongs at the edge, where the controller decides locally.

For the operator side of the same tier, **Visitors** issues and ends time-bound passes, and
`GET /api/badge/preview/{id}` renders *what a holder's own badge says* — the fastest answer
to "my pass doesn't work", since it reuses the holder's exact payload and the badge's own
components. It is read-only and mints no session: a badge action is recorded as the
**holder's**, so acting through a borrowed badge session would be indistinguishable from
them in the audit trail.

The console is **rebrandable at runtime without a rebuild**: point `branding.dir`
(env `SA_BRANDING_DIR`) at a host directory of `theme.css` / `logo.svg` /
`branding.json` to override the app name, logo, and DaisyUI theme. See
[`docs/configuration.md`](docs/configuration.md#branding-accessd-only) and the
[`branding.example/`](branding.example) template.

### Build order (the embed happens at Go compile time)

```
cd ui && npm install        # once
npm run build               # → internal/webui/public  (commit this)
cd .. && go build ./cmd/accessd
./accessd serve             # UI at http://127.0.0.1:8090/  · admin at /_
```

Always build the UI **before** the binary; the committed `internal/webui/public`
means a fresh checkout embeds a working UI without needing npm.

### UI development

```
npm --prefix ui run dev     # http://localhost:5174, proxies /api + /_ to :8090
```

Requires Node 20.19+ / 22.12+ (Vite 8).

## Test

```
go test ./...
```

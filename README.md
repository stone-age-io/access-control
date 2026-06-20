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
- [`docs/hardware.md`](docs/hardware.md) — physical I/O: supported boards, pin
  maps, relay/input polarity, transports, and how to add a board.

## Layout

```
cmd/accessd/            central: PocketBase + KV mirror publisher + audit consumer + controller-health monitor
cmd/access-controller/  edge: policy watcher + pure decision + drivers + door monitoring + heartbeat
internal/policy/        the pure core: Policy types, Decide(), windowOpen()
internal/controller/    PolicyStore (KV watch → maps), tap loop, door state machine, portal/lock arming, commands, heartbeat
internal/drivers/       ReaderDriver / LockDriver / DoorInput / FAIInput interfaces + mocks (MockHardware)
internal/drivers/hardware/  per-model hardware Profile: logical relay/input index → physical line + transport
internal/drivers/gpio/  native GPIO lock + door-input backend (go-gpiocdev, no cgo; Linux only)
internal/drivers/i2c/   MCP23017 lock + door-input backend over I2C (periph.io, no cgo; polled inputs)
internal/drivers/osdp/  OSDP reader: RS485 CP engine (pure-Go, no cgo) + wire codec (osdp/wire); controller.reader: osdp
internal/health/        accessd-side heartbeat subscriber → controllers.last_seen/status
internal/audit/         JetStream consumer → PocketBase events collection
internal/natsx/         NATS connection + KV helpers
internal/webui/         the compiled management UI, //go:embed-ed into accessd
pbmigrations/           PocketBase collections (schema-in-code)
ui/                     Vue 3 + Vite management UI source (PocketBase-backed CRUD)
```

## Web UI

`accessd` serves a Vue 3 management console (locations, schedules, portals,
controllers, access groups, roles, cardholders, credentials, an events timeline)
at `/`. It is
compiled into `internal/webui/public` and **`//go:embed`-ed into the accessd
binary** — there is no `pb_public` directory to ship; the binary is
self-contained.

Auth is PocketBase superuser only (`accessd superuser upsert <email> <pass>`),
since the access-control collections have superuser-only API rules.

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

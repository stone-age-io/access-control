# stone-access

A standalone, NATS-native physical access control (PACS) app that dogfoods the
[Stone-Age.io](https://stone-age.io) platform — RBAC door control with schedules,
deny-override, and edge autonomy, composed from the platform's primitives
(NATS core, KV, JetStream, PocketBase control plane).

The authorization decision is a small **pure function** over an in-memory policy
graph (`internal/policy`), not a rules engine. The central app (`accessd`) is the
system of record (PocketBase) and mirrors policy to NATS KV one key per record;
edge controllers (`access-controller`) watch that keyspace and decide locally.

> v1 status: the **reader is simulated** (taps arrive over NATS; a real OSDP/
> Wiegand driver slots in later), but the **lock and door inputs have real GPIO
> drivers** (`internal/drivers/gpio`, KinCony Server-Mini) alongside the mocks,
> selectable per controller. Door monitoring (forced / held-open) and controller
> heartbeat/health are implemented.

## Layout

```
cmd/accessd/            central: PocketBase + KV mirror publisher + audit consumer + controller-health monitor
cmd/access-controller/  edge: policy watcher + pure decision + drivers + door monitoring + heartbeat
internal/policy/        the pure core: Policy types, Decide(), windowOpen()
internal/controller/    PolicyStore (KV watch → maps), tap loop, door state machine, portal/lock arming, commands, heartbeat
internal/drivers/       ReaderDriver / LockDriver / DoorInput / FAIInput interfaces + mocks (MockHardware)
internal/drivers/hardware/  per-model hardware Profile: logical relay/input index → physical line
internal/drivers/gpio/  GPIO lock + door-input backend (go-gpiocdev, no cgo; Linux only)
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

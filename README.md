# stone-access

A standalone, NATS-native physical access control (PACS) app that dogfoods the
[Stone-Age.io](https://stone-age.io) platform — RBAC door control with schedules,
deny-override, and edge autonomy, composed from the platform's primitives
(NATS core, KV, JetStream, PocketBase control plane).

The authorization decision is a small **pure function** over an in-memory policy
graph (`internal/policy`), not a rules engine. The central app (`accessd`) is the
system of record (PocketBase) and mirrors policy to NATS KV one key per record;
edge controllers (`access-controller`) watch that keyspace and decide locally.

> v1 status: **software substrate, no hardware.** Reader/lock/FAI are interfaces
> with mock implementations. See the build plan for scope and deferred work.

## Layout

```
cmd/accessd/            central: PocketBase + KV mirror publisher + audit consumer + command issuer
cmd/access-controller/  edge: policy watcher + pure decision + drivers + command handler
internal/policy/        the pure core: Policy types, Decide(), windowOpen()
internal/controller/    PolicyStore (KV watch → maps), tap loop, command handler
internal/drivers/       ReaderDriver / LockDriver / FAIInput interfaces + mocks
internal/audit/         JetStream consumer → PocketBase events collection
internal/natsx/         NATS connection + KV helpers
internal/webui/         the compiled management UI, //go:embed-ed into accessd
pbmigrations/           PocketBase collections (schema-in-code)
ui/                     Vue 3 + Vite management UI source (PocketBase-backed CRUD)
```

## Web UI

`accessd` serves a Vue 3 management console (sites, schedules, access points,
groups, roles, cardholders, credentials, an events timeline) at `/`. It is
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

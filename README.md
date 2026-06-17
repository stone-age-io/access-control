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
pbmigrations/           PocketBase collections (schema-in-code)
```

## Test

```
go test ./...
```

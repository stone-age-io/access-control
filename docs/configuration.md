# stone-access configuration reference

Both binaries — `accessd` and `access-controller` — share one config schema
([`config/config.go`](../config/config.go)), loaded by Viper. This page is the
full key reference. For the *meaning* of the wire-level keys (`subjects.app`,
the bucket/stream names), see [`protocol.md`](protocol.md).

The annotated, copy-pasteable starting points are the example configs — read
those first, then use the tables here to look up a default or env var:

- [`config/accessd.yaml`](../config/accessd.yaml) — the central app
- [`config/controller.yaml`](../config/controller.yaml) — an edge controller

## Contents

- [How config loads](#how-config-loads) — file path, env overrides, parsing
- [NATS connection](#nats-connection) · [auth](#nats-auth-set-at-most-one) · [TLS](#nats-tls)
- [Logging](#logging) · [Metrics](#metrics) · [Diagnostics](#diagnostics-controller-only)
- [Resource names](#resource-names-shared) — buckets, stream, app token (must match across the fleet)
- [accessd](#accessd) · [controller](#controller)
- [What gets rejected](#what-gets-rejected) — every way `Load` fails
- [Which binary reads what](#which-binary-reads-what)

## How config loads

- **File path.** `accessd` reads `$SA_CONFIG` (default `config/accessd.yaml`).
  `access-controller` uses its `-config` flag (default `config/controller.yaml`).
- **A missing file is fine** — defaults plus env vars still apply. Nothing is
  required to exist on disk.
- **Every key is overridable by an `SA_`-prefixed env var.** Take the dotted key,
  uppercase it, replace dots with underscores: `nats.urls` → `SA_NATS_URLS`,
  `controller.heartbeatInterval` → `SA_CONTROLLER_HEARTBEATINTERVAL`,
  `nats.tls.enable` → `SA_NATS_TLS_ENABLE`. Env wins over the file.
- **Parsing.** Durations are Go duration strings (`250ms`, `15s`, `45s`).
  `nats.urls` accepts a comma-separated list via the env var.

The PocketBase HTTP address is **not** a config key — it is owned by PocketBase's
own `serve --http` flag (default `127.0.0.1:8090`).

## NATS connection

| Key | Default | Env var | Notes |
|---|---|---|---|
| `nats.urls` | `nats://localhost:4222` | `SA_NATS_URLS` | one or more; comma-separated via env. Use the `tls://` scheme for TLS. |
| `nats.maxReconnects` | `-1` (forever) | `SA_NATS_MAXRECONNECTS` | `0` is treated as `-1`; the KV watcher re-arms on every reconnect. |
| `nats.reconnectWait` | `250ms` | `SA_NATS_RECONNECTWAIT` | backoff between reconnect attempts. |

### NATS auth (set at most one)

Setting more than one method fails validation. None is allowed (an open dev box).
A `.creds` file is the recommended path for a backend service; `*.creds` is
gitignored — never commit credentials.

| Key | Default | Env var | Notes |
|---|---|---|---|
| `nats.credsFile` | `""` | `SA_NATS_CREDSFILE` | JWT + nkey `.creds` file. **Must exist** if set. |
| `nats.nkeySeedFile` | `""` | `SA_NATS_NKEYSEEDFILE` | raw nkey seed file. |
| `nats.token` | `""` | `SA_NATS_TOKEN` | bearer token. |
| `nats.username` | `""` | `SA_NATS_USERNAME` | with `password`. |
| `nats.password` | `""` | `SA_NATS_PASSWORD` | with `username`. |

### NATS TLS

Not needed when the URL is `tls://` and the server presents a publicly-trusted
cert. Enable for mutual TLS or a custom CA.

| Key | Default | Env var | Notes |
|---|---|---|---|
| `nats.tls.enable` | `false` | `SA_NATS_TLS_ENABLE` | |
| `nats.tls.caFile` | `""` | `SA_NATS_TLS_CAFILE` | custom CA to verify the server. |
| `nats.tls.certFile` | `""` | `SA_NATS_TLS_CERTFILE` | client cert (mutual TLS) — set **with** `keyFile`. |
| `nats.tls.keyFile` | `""` | `SA_NATS_TLS_KEYFILE` | set **with** `certFile`. |
| `nats.tls.insecure` | `false` | `SA_NATS_TLS_INSECURE` | skip server verification — **never** in production. |

## Logging

| Key | Default | Env var | Notes |
|---|---|---|---|
| `logging.level` | `info` | `SA_LOGGING_LEVEL` | `debug` · `info` · `warn` · `error`. |
| `logging.encoding` | `json` | `SA_LOGGING_ENCODING` | `json` · `console`. |
| `logging.outputPath` | `stdout` | `SA_LOGGING_OUTPUTPATH` | |

## Metrics

Prometheus endpoint on a side port. The example configs enable it and put the
two binaries on **different ports** so both can run on one host: accessd `:2113`,
controller `:2114`.

| Key | Default | Env var | Notes |
|---|---|---|---|
| `metrics.enabled` | `false` | `SA_METRICS_ENABLED` | example configs set `true`. |
| `metrics.address` | `:2113` | `SA_METRICS_ADDRESS` | controller example uses `:2114`. |
| `metrics.path` | `/metrics` | `SA_METRICS_PATH` | |
| `metrics.updateInterval` | `15s` | `SA_METRICS_UPDATEINTERVAL` | duration string. |

## Diagnostics (controller-only)

An **opt-in, read-only** local status page for field install and troubleshooting,
served by `access-controller` only (accessd ignores this section). When enabled it
serves `/status` (a self-contained, auto-refreshing HTML page — no JS, no external
assets) and `/status.json` on `diagnostics.address`. It renders this box's live
in-memory state: identity (incl. `subjects.app`), NATS/policy-sync health, the
portals it bound and their door/posture state, recent decisions (with the decoded
credential), and recent alarms.

It is strictly **read-only** — all control stays on the NATS command plane. It is
**disabled by default**, and because the page reveals topology (portal codes,
locations, reader addresses) it binds **localhost by default**; reach it over SSH /
a tunnel rather than binding a public interface. Hosting it is independent of
`metrics` (its own server/port), so metrics can be scraped on a monitoring network
while diagnostics stays local.

| Key | Default | Env var | Notes |
|---|---|---|---|
| `diagnostics.enabled` | `false` | `SA_DIAGNOSTICS_ENABLED` | controller only. |
| `diagnostics.address` | `127.0.0.1:2115` | `SA_DIAGNOSTICS_ADDRESS` | keep local unless deliberately exposed. |

## Resource names (shared)

These name the NATS resources the fleet talks over. **accessd and every
controller must agree on them** — a mismatch silently severs the data plane.
See [`protocol.md`](protocol.md) for what each carries.

| Key | Default | Env var | Notes |
|---|---|---|---|
| `policy.bucket` | `ACC_POLICY` | `SA_POLICY_BUCKET` | KV bucket: the downward policy mirror. Read by both. |
| `status.bucket` | `ACC_STATUS` | `SA_STATUS_BUCKET` | KV bucket: the upward device shadow. Read by both. |
| `events.stream` | `ACC_EVENTS` | `SA_EVENTS_STREAM` | JetStream audit stream. accessd creates/consumes it; its subjects are derived from `subjects.app`, not set here. |
| `subjects.app` | `acc` | `SA_SUBJECTS_APP` | the app token every subject leads with. Must be a single NATS token (no `.`, `*`, `>`, or whitespace). |

## accessd

| Key | Default | Env var | Notes |
|---|---|---|---|
| `accessd.dataDir` | `./pb_data` | `SA_ACCESSD_DATADIR` | embedded PocketBase data dir (db + uploads). Created at runtime, gitignored. The UI is `//go:embed`-ed, so there is no `pb_public`. |
| `accessd.controllerOfflineAfter` | `45s` | `SA_ACCESSD_CONTROLLEROFFLINEAFTER` | silence before a controller shows offline. Keep it a few controller `heartbeatInterval`s so one dropped heartbeat does not flap a box offline. |
| `accessd.auditRetentionDays` | `365` | `SA_ACCESSD_AUDITRETENTIONDAYS` | how long control-plane audit rows (`audit_logs`, written by `internal/changelog`) are kept before a daily 03:00 prune deletes them. `0` normalizes to 365; a **negative** value disables pruning (keep forever). See [`operators.md`](operators.md#control-plane-audit-log-audit_logs). |

## controller

A controller's config is just its identity and hardware selection — **which
portals it drives, and their relay/input bindings, live in policy, not here**
(matched by `controller.code`).

| Key | Default | Env var | Notes |
|---|---|---|---|
| `controller.code` | `""` | `SA_CONTROLLER_CODE` | matches a `controllers` record; the box arms every portal whose `controller` relation points at this code. |
| `controller.location` | `""` | `SA_CONTROLLER_LOCATION` | location code: selects the timezone and scopes the command/fire subscriptions. |
| `controller.heartbeatInterval` | `15s` | `SA_CONTROLLER_HEARTBEATINTERVAL` | liveness cadence to `acc.{location}.ctrl.{code}.heartbeat`. |
| `controller.driver` | `mock` | `SA_CONTROLLER_DRIVER` | `mock` (simulated, no I/O, no door monitoring) or `gpio` (real relays + DPS/REX; Linux only). Under `gpio` the `model` picks the physical transport: native GPIO char device or an MCP23017 I2C expander. |
| `controller.model` | `""` | `SA_CONTROLLER_MODEL` | hardware profile — maps a portal's logical relay/input indices to physical lines, selects the lock/input transport (native GPIO char device or MCP23017 over I2C), **and** provides the OSDP RS485 serial port: `kincony-server-mini` (CM4, GPIO, `/dev/ttyAMA0`) · `kincony-pi5r8` (CM5, I2C, `/dev/ttyAMA2`). **Required when `driver: gpio`, `reader: osdp`, or `reader: both`.** Must match the `controllers` record. |
| `controller.reader` | `nats` | `SA_CONTROLLER_READER` | credential reader: `nats` (simulated taps published to `acc.{location}.{type}.{thing}.tap`, for dev), `osdp` (a real OSDP reader on the model's RS485 bus, clear-text in v1), or `both` (NATS for every portal **plus** OSDP for portals that have a reader). Independent of `driver` — the lock/door stay on GPIO/I2C. **`osdp` and `both` require `model`.** A portal opts into OSDP via its `reader_address`: `>= 0` = OSDP reader at that PD address, `-1` = NATS-only. Emitted tap events carry a `source` (`nats`/`osdp`). |

## What gets rejected

`Load` returns an error (the binary refuses to start) only in these cases —
everything else falls back to a default:

- no NATS URL configured
- more than one NATS auth method set
- `nats.credsFile` set but the file does not exist
- `nats.tls.enable` true with only one of `certFile` / `keyFile`
- `logging.level` not one of `debug`/`info`/`warn`/`error`
- `metrics.updateInterval` not a parseable duration
- `policy.bucket` or `status.bucket` empty
- `subjects.app` empty or not a single NATS token
- `controller.driver` not `mock`/`gpio`, or `gpio` with no `controller.model`
- `controller.reader` not `nats`/`osdp`/`both`, or `osdp`/`both` with no `controller.model`

## Which binary reads what

- **Both** read `nats`, `logging`, `metrics`, `policy`, `status`, `subjects`
  (the controller writes the status bucket; accessd watches it).
- **accessd** also reads `events` and `accessd`.
- **access-controller** also reads `controller` and `diagnostics`.

Unused sections are simply ignored, so the two binaries can share one file if you
prefer (env vars then specialize per host).

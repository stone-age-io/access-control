# The access-control contract

What an organization needs on the platform to run access control: the Thing
Types, their operations and the NATS roles. It is a `stone` template, so
onboarding an organization is one command, run as an owner or admin of it:

```sh
stone org switch <org>
stone apply --template contract/ --dry-run
stone apply --template contract/
```

Applying it again updates an organization to this version and changes
nothing that already matches. It never deletes, and it leaves alone anything
it does not name. See `stone apply --help` (TEMPLATES).

| Thing Type | Process | NATS role |
|---|---|---|
| `accessd` | `accessd` | `accessd` |
| `access-controller` | each edge controller | `access-controller` |
| `access-door`, `access-gate` | a portal; inventory only, it has no credential of its own | none |

Give each process's NATS user the role in its row. The role is shared by every
Thing of that kind, so its subjects use `*` for the location and the Thing;
pb-nats has no per-Thing subject. A controller's role reads ACC_POLICY and
writes only ACC_STATUS, so a stolen controller credential cannot change
policy.

Keep this folder in step with `internal/subjects` and with the stream and
bucket names in `config`. The platform's demo seed carries the same Thing
Types (`platform/internal/demoseed/contract.go`).

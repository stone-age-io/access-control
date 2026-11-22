package main

import (
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/nats-io/nats.go"
	"github.com/stone-age-io/kit/health"
	kitnatsx "github.com/stone-age-io/kit/natsx"
	"github.com/stone-age-io/kit/pbx"
)

// readinessCollections are the collections accessd cannot serve without. When
// one is missing, the binary's migrations never registered: PocketBase's own
// collections exist and the console loads, but nothing of ours is there.
var readinessCollections = []string{"locations", "portals", "controllers", "credentials", "cardholders", "events"}

// registerReadiness adds accessd's checks for GET /api/ready to reg: the
// database answers, accessd's collections exist, the database is not ahead of
// the binary, and NATS is connected. NATS fails rather than warns: accessd is
// the policy writer and the audit consumer, and without NATS no controller
// hears a policy change and no event reaches the events log. nc is read at
// probe time, because the connection is built after the routes.
func registerReadiness(reg *health.Registry, app core.App, nc func() *nats.Conn) {
	reg.Register("database", pbx.Database(app))
	reg.Register("schema", pbx.Schema(app, readinessCollections...))
	reg.Register("schema_version", pbx.SchemaVersion(app))
	reg.Register("nats", kitnatsx.Ready(nc))
}

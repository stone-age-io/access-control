// Command accessd is the central stone-access app: the system of record
// (embedded PocketBase), the KV mirror publisher (policy graph → NATS KV, one
// key per record), and the JetStream audit consumer (events → PocketBase).
//
// It is driven by PocketBase's CLI (e.g. `accessd serve`). Our SA_-prefixed
// config (NATS, policy bucket, audit stream, logging) is loaded from the file
// at $SA_CONFIG (default config/accessd.yaml) plus environment overrides.
//
// On `serve` it boots PocketBase with the schema migrations and fixture, serves
// the embedded management UI at "/", and brings up the NATS connection (ensuring
// the policy KV bucket and audit stream), the KV mirror publisher, and the
// JetStream audit consumer in the serve lifecycle.
package main

import (
	"context"
	"io/fs"
	"net/http"
	"net/mail"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/pocketbase/pocketbase/tools/mailer"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stone-age-io/access-control/config"
	"github.com/stone-age-io/access-control/internal/audit"
	"github.com/stone-age-io/access-control/internal/changelog"
	"github.com/stone-age-io/access-control/internal/commandapi"
	"github.com/stone-age-io/access-control/internal/disarm"
	"github.com/stone-age-io/access-control/internal/health"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/mirror"
	"github.com/stone-age-io/access-control/internal/modelsapi"
	"github.com/stone-age-io/access-control/internal/natsx"
	"github.com/stone-age-io/access-control/internal/notify"
	"github.com/stone-age-io/access-control/internal/status"
	"github.com/stone-age-io/access-control/internal/subjects"
	"github.com/stone-age-io/access-control/internal/webui"

	// Side-effect import: registers the schema + fixture migrations.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

func main() {
	boot := logger.NewBootstrapLogger()

	cfg, err := config.Load(configPath())
	if err != nil {
		boot.Fatal("failed to load config", "error", err)
	}

	log, err := logger.NewLogger(&cfg.Logging)
	if err != nil {
		boot.Fatal("failed to create logger", "error", err)
	}
	defer func() { _ = log.Sync() }()
	log = log.With("app", "accessd")

	m, err := metrics.NewMetrics(prometheus.NewRegistry())
	if err != nil {
		log.Fatal("failed to create metrics", "error", err)
	}

	pb := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir: cfg.Accessd.DataDir,
	})

	// migratecmd exposes `accessd migrate ...` and, with Automigrate, snapshots
	// dashboard collection edits into Go files beside our hand-authored ones.
	migratecmd.MustRegister(pb, pb.RootCmd, migratecmd.Config{
		Dir:          "pbmigrations",
		Automigrate:  true,
		TemplateLang: migratecmd.TemplateLangGo,
	})

	// Control-plane audit log: record who changes which policy record, via API.
	// Binds *Request hooks (API-only), so accessd's own app.Save() writes
	// (heartbeats, projections, mirror) never appear. Registered here (not in
	// OnServe) since it's pure PocketBase — independent of NATS — and the hooks
	// only fire once serving anyway.
	changelog.Register(pb, cfg.Accessd.AuditRetentionDays, log)

	// Events-projection retention: a daily prune of the rebuildable events read
	// model (JetStream stays the system of record). Pure PocketBase cron, like the
	// changelog prune, so it registers here rather than in OnServe. No-op unless
	// eventRetentionDays > 0 — opt-in, so an upgrade never deletes event history.
	audit.RegisterPrune(pb, cfg.Accessd.EventRetentionDays, log)

	// Resources brought up only when actually serving (not for migrate/superuser).
	var (
		nc         *natsx.Conn
		metricsSrv *http.Server
		collector  *metrics.Collector
		auditC     *audit.Consumer
		notifier   *notify.Notifier
		disarmer   *disarm.Disarmer
		healthMon  *health.Monitor
		statusProj *status.Projector
	)

	pb.OnServe().BindFunc(func(e *core.ServeEvent) error {
		// Serve the embedded management UI (SPA) at "/" with history-mode
		// fallback. PocketBase does not serve static assets in framework mode,
		// so we register the catch-all ourselves; the more specific /api and /_
		// routes PocketBase registers take precedence over /{path...}.
		uiFS, err := fs.Sub(webui.FS, "public")
		if err != nil {
			return err
		}
		e.Router.GET("/{path...}", func(re *core.RequestEvent) error {
			p := re.Request.PathValue("path")
			if p == "" || p == "/" {
				return re.FileFS(uiFS, "index.html")
			}
			if f, openErr := uiFS.Open(p); openErr == nil {
				_ = f.Close()
				return re.FileFS(uiFS, p)
			}
			// A missing asset (has an extension) is a real 404; anything else is
			// a client-side route → hand back index.html so vue-router resolves it.
			if strings.Contains(p, ".") {
				return re.NotFoundError("File not found", nil)
			}
			return re.FileFS(uiFS, "index.html")
		})

		updateInterval, _ := time.ParseDuration(cfg.Metrics.UpdateInterval)
		collector = metrics.NewCollector(m, updateInterval)
		collector.Start()

		if cfg.Metrics.Enabled {
			metricsSrv = m.NewServer(cfg.Metrics.Address, cfg.Metrics.Path)
			go func() {
				log.Info("metrics server listening", "address", cfg.Metrics.Address, "path", cfg.Metrics.Path)
				if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Error("metrics server error", "error", err)
				}
			}()
		}

		// On reconnect, re-establish the status watcher (WatchAll re-delivers every
		// key = full re-sync). statusProj is assigned below, before any reconnect
		// can fire.
		nc, err = natsx.Connect(&cfg.NATS, log, m, func() {
			if statusProj != nil {
				statusProj.Resync()
			}
		})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		kv, err := nc.EnsureKVBucket(ctx, cfg.Policy.Bucket)
		if err != nil {
			return err
		}
		// The upward device-shadow bucket: accessd owns its creation; controllers
		// bind it read-write to publish their live state.
		statusKV, err := nc.EnsureKVBucket(ctx, cfg.Status.Bucket)
		if err != nil {
			return err
		}
		// The stream captures the whole event subtree; both its subject set and
		// the audit consumer's filter come from the one subjects app token.
		subj := subjects.New(cfg.Subjects.App)
		if _, err := nc.EnsureStream(ctx, cfg.Events.Stream, subj.EventsWildcards()); err != nil {
			return err
		}

		// KV mirror publisher: PocketBase record changes → ACC_POLICY keys.
		// Register the hooks first, then reconcile existing records (migrations
		// seed data before the hooks bind) and prune stale keys.
		pub := mirror.Register(e.App, kv, log, m)
		if err := pub.SyncAll(ctx, e.App); err != nil {
			return err
		}

		// Audit consumer: ACC_EVENTS JetStream → events collection (UI timeline).
		auditC = audit.New(e.App, nc.JS, cfg.Events.Stream, subj, log, m)
		if err := auditC.Start(ctx); err != nil {
			return err
		}

		// Notification sink: a SECOND, independent durable on ACC_EVENTS that emails
		// on alarm/fire (DeliverNew — alerting is not a backfillable projection).
		// Like the disarm sink it is ALWAYS started and config-free: inert unless
		// both the alarm's source opts in (portals/areas.notify_on_alarm,
		// locations.notify_fire) AND an operator opts in (users.notify). The SendFunc
		// owns every PocketBase concern; SMTP transport is PocketBase's own mail
		// settings.
		notifier = notify.New(nc.JS, cfg.Events.Stream, subj, newNotifySend(e.App, log), log, m)
		if err := notifier.Start(ctx); err != nil {
			return err
		}

		// Entry-disarm sink: a durable on ACC_EVENTS (DeliverNew) that durably
		// disarms an area on a valid grant at an entry (disarm_on_grant) portal —
		// the central, reboot-safe half of badge-disarms-the-area. Always on; it is
		// inert unless a portal opts in via disarm_on_grant, so it needs no config.
		disarmer = disarm.New(nc.JS, cfg.Events.Stream, subj, newDisarmFunc(e.App, log), log, m)
		if err := disarmer.Start(ctx); err != nil {
			return err
		}

		// Controller health: core-NATS heartbeat subscriber → controllers
		// last_seen/status (a direct record update, not an events row). Owns its
		// own lifetime; stopped in OnTerminate.
		healthMon = health.New(e.App, nc.NC, subj, cfg.Accessd.ControllerOfflineAfter, log, m)
		if err := healthMon.Start(); err != nil {
			return err
		}

		// Command bridge: superuser-only HTTP routes → control-plane NATS commands
		// (cmd.grant / cmd.posture). The UI's only way to drive a portal.
		commandapi.Register(e, nc.NC, subj, log)

		// Hardware-model catalogue: read-only GET /api/models the UI reads to render
		// the controller I/O map and bound the relay/input index pickers.
		modelsapi.Register(e)

		// Status projector: ACC_STATUS device shadow → point_status projection (UI
		// live state). Watches on a background context so it outlives this setup
		// (the 2-minute ctx above is cancelled on return); stopped in OnTerminate.
		statusProj = status.New(e.App, statusKV, log, m)
		if err := statusProj.Start(context.Background()); err != nil {
			return err
		}

		log.Info("accessd serving",
			"policyBucket", cfg.Policy.Bucket,
			"statusBucket", cfg.Status.Bucket,
			"eventsStream", cfg.Events.Stream,
			"subjectsApp", cfg.Subjects.App,
			"dataDir", cfg.Accessd.DataDir)
		return e.Next()
	})

	pb.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		log.Info("accessd terminating")
		if statusProj != nil {
			statusProj.Stop()
		}
		if healthMon != nil {
			healthMon.Stop()
		}
		if notifier != nil {
			notifier.Stop()
		}
		if disarmer != nil {
			disarmer.Stop()
		}
		if auditC != nil {
			auditC.Stop()
		}
		if collector != nil {
			collector.Stop()
		}
		if metricsSrv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = metricsSrv.Shutdown(ctx)
			cancel()
		}
		if nc != nil {
			_ = nc.Close()
		}
		return e.Next()
	})

	if err := pb.Start(); err != nil {
		log.Fatal("pocketbase exited with error", "error", err)
	}
}

// newNotifySend builds the notification sink's PocketBase-backed SendFunc. It is
// the inverse of the config it replaces: the "who" (recipient operators) and the
// "which" (per-source opt-in) both live in data. For each alarm/fire it (1) checks
// the source record's opt-in flag and (2) resolves the opted-in operators, then
// mails them via PocketBase's configured transport. It reports sent=false (a benign
// skip, NOT an error) when the source hasn't opted in or no operator wants mail, so
// the sink stays inert until both line up. A real DB-read or SMTP failure returns an
// error → Nak → redelivery (bounded by the sink's MaxDeliver).
func newNotifySend(app core.App, log *logger.Logger) notify.SendFunc {
	return func(ev notify.Event) (bool, error) {
		// The auto-clear of a held-open door is operational noise, not an alarm to
		// page on — only the raise (forced/held/intrusion) and fire are emailed.
		if ev.Kind == "alarm" && ev.AlarmType == "held_clear" {
			return false, nil
		}
		if !sourceWantsNotify(app, ev) {
			return false, nil // the source has not opted into email
		}
		recipients, err := notifyRecipients(app, ev.Location)
		if err != nil {
			return false, err // a genuine read failure: redeliver
		}
		if len(recipients) == 0 {
			return false, nil // no operator opted in
		}

		msg := notify.Format(ev)
		to := make([]mail.Address, 0, len(recipients))
		for _, r := range recipients {
			to = append(to, mail.Address{Address: r})
		}
		if err := app.NewMailClient().Send(&mailer.Message{
			From:    mail.Address{Address: app.Settings().Meta.SenderAddress, Name: app.Settings().Meta.SenderName},
			To:      to,
			Subject: msg.Subject,
			Text:    msg.Body,
		}); err != nil {
			return false, err // SMTP failure: Nak → redeliver
		}
		log.Info("alarm notification sent", "kind", ev.Kind, "type", ev.AlarmType,
			"location", ev.Location, "thing", ev.Thing, "recipients", len(recipients))
		return true, nil
	}
}

// sourceWantsNotify reports whether the record that emitted this alarm has opted
// into email. The flag rides the source: fire is location-scoped
// (locations.notify_fire); an intrusion alarm is area-scoped (the subject's type
// token is the literal "area" → areas.notify_on_alarm); every other alarm is a
// portal forced/held (portals.notify_on_alarm). Subject tokens are stable codes, so
// each source is found by code. Fail-safe: a missing/dangling source reads as no
// opt-in (no email), never an error.
func sourceWantsNotify(app core.App, ev notify.Event) bool {
	switch {
	case ev.Kind == "fire":
		loc, err := app.FindFirstRecordByFilter("locations", "code = {:code}", dbx.Params{"code": ev.Location})
		return err == nil && loc.GetBool("notify_fire")
	case ev.Type == "area":
		area, err := app.FindFirstRecordByFilter("areas", "code = {:code}", dbx.Params{"code": ev.Thing})
		return err == nil && area.GetBool("notify_on_alarm")
	default:
		portal, err := app.FindFirstRecordByFilter("portals", "code = {:code}", dbx.Params{"code": ev.Thing})
		return err == nil && portal.GetBool("notify_on_alarm")
	}
}

// notifyRecipients returns the email addresses of operators who opted into alarm
// email (users.notify) and whose location scope covers this alarm. The operator
// set is tiny, so it filters in Go rather than risk a backend-specific query. A
// read failure is surfaced (→ redeliver).
//
// Scoping (users.notify_locations): an empty scope means all locations — so an
// operator who never narrows keeps the pre-scoping receive-everything behavior; a
// non-empty scope must contain this alarm's location. The relation stores location
// record ids while the wire carries the stable code, so the alarm's locationCode
// is resolved to its id once. A missing/dangling location resolves to "" (no scoped
// operator can match it), so it falls through to the unscoped recipients only.
func notifyRecipients(app core.App, locationCode string) ([]string, error) {
	recs, err := app.FindAllRecords("users")
	if err != nil {
		return nil, err
	}
	var locID string
	if loc, err := app.FindFirstRecordByFilter("locations", "code = {:code}", dbx.Params{"code": locationCode}); err == nil {
		locID = loc.Id
	}
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		if !r.GetBool("notify") {
			continue
		}
		if scope := r.GetStringSlice("notify_locations"); len(scope) > 0 && !slices.Contains(scope, locID) {
			continue // operator is scoped to other locations
		}
		if email := r.GetString("email"); email != "" {
			out = append(out, email)
		}
	}
	return out, nil
}

// newDisarmFunc builds the disarm sink's PocketBase-backed action: a valid grant
// at an entry portal durably disarms that portal's area (arm_override=disarmed),
// which the mirror propagates to KV so every peer controller converges. Idempotent
// and fail-safe — an unknown portal/area, a non-entry door, or an area that is
// already disarmed (or can never be armed) is a silent no-op, never an error.
func newDisarmFunc(app core.App, log *logger.Logger) disarm.DisarmFunc {
	return func(portalCode, cred string) (bool, error) {
		portal, err := app.FindFirstRecordByFilter("portals", "code = {:code}", dbx.Params{"code": portalCode})
		if err != nil {
			return false, nil // unknown portal: nothing to disarm
		}
		if !portal.GetBool("disarm_on_grant") {
			return false, nil // not an entry door
		}
		areaID := portal.GetString("area")
		if areaID == "" {
			return false, nil // entry door with no area
		}
		area, err := app.FindRecordById("areas", areaID)
		if err != nil {
			return false, nil // dangling area relation
		}
		if !shouldDisarm(area) {
			return false, nil
		}
		area.Set("arm_override", "disarmed")
		if err := app.Save(area); err != nil {
			return false, err // a real write failure: redeliver
		}
		writeDisarmAudit(app, area, portalCode, cred, log)
		log.Info("area disarmed on entry grant", "area", area.GetString("code"), "portal", portalCode, "cred", cred)
		return true, nil
	}
}

// shouldDisarm reports whether disarming the area would be meaningful. Skip an area
// already explicitly overridden-disarmed, and an area that can never be armed (no
// armed override, no standing armed, no auto_arm schedule) — that avoids writing a
// redundant override (and an audit row) every time a permanently-disarmed entry
// door is used. accessd cannot evaluate a schedule window, so a scheduled area is
// treated as possibly-armed and is disarmed.
func shouldDisarm(area *core.Record) bool {
	if area.GetString("arm_override") == "disarmed" {
		return false
	}
	return area.GetString("arm_override") == "armed" ||
		area.GetString("arm") == "armed" ||
		area.GetString("auto_arm") == "armed"
}

// writeDisarmAudit records an entry-disarm to audit_logs. There is no request
// context (the disarm originates from a controller's grant event, not an operator
// API call), so it is attributed to the credential + portal rather than an
// operator. Fail-safe: the disarm has already committed, so a write failure is
// logged, never propagated.
func writeDisarmAudit(app core.App, area *core.Record, portal, cred string, log *logger.Logger) {
	col, err := app.FindCollectionByNameOrId("audit_logs")
	if err != nil {
		log.Error("disarm audit sink unavailable", "error", err)
		return
	}
	rec := core.NewRecord(col)
	rec.Set("event_type", "update")
	rec.Set("collection_name", "areas")
	rec.Set("record_id", area.Id)
	rec.Set("actor_email", "entry-disarm") // a system actor, not an operator
	rec.Set("request_url", "/disarm/grant")
	rec.Set("timestamp", types.NowDateTime())
	rec.Set("after", map[string]any{"arm_override": "disarmed", "by_credential": cred, "at_portal": portal})
	if err := app.Save(rec); err != nil {
		log.Error("failed to write disarm audit row", "area", area.GetString("code"), "error", err)
	}
}

// configPath returns the SA_CONFIG path, defaulting to config/accessd.yaml.
// A missing file is tolerated by config.Load (defaults + env still apply).
func configPath() string {
	if p := os.Getenv("SA_CONFIG"); p != "" {
		return p
	}
	return "config/accessd.yaml"
}

// Package repage is accessd's unacknowledged-alarm reminder: a periodic sweep that
// re-sends a notification for an alarm nobody has acknowledged yet.
//
// The pieces it joins already existed and did not talk to each other. Migration
// 1750000020 gave events an acknowledged/ack_by/ack_at trio, so an alarm is a thing
// an operator can take responsibility for; internal/notify emails once, when the
// alarm is raised. Between them was the failure mode that actually matters at 3am:
// the one page is missed, and nothing ever asks again. A raised alarm with nobody
// acknowledging it is exactly the case a notification system exists for.
//
// Deliberately narrow, because a reminder loop is easy to turn into a spam loop:
//
//   - Only the URGENT types are re-paged (forced, intrusion, fire, controller
//     offline) — never held/no_entry, which are the high-volume diagnostics. See
//     urgentTypes.
//   - Re-pages are BOUNDED (maxRepages). A permanently unacknowledged alarm stops
//     nagging rather than paging forever; at some point silence is information too.
//   - There is a floor on age (repageAfter) so an operator acknowledging promptly is
//     never paged twice.
//   - It reuses the SAME notify.SendFunc the sink uses, so the two-sided opt-in
//     (source flag AND users.notify/notify_types/notify_locations) applies
//     identically. A re-page can never reach someone the original page could not.
//
// It is a projection reader, not an event consumer: it queries the events collection
// rather than adding a fifth durable, because "still unacknowledged N minutes later"
// is a question about accumulated state, not about a message arriving.
package repage

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/notify"
)

// Sweep timing and bounds. Vars (not consts) so tests can shorten them — the same
// pattern as armrelease.releaseInterval and the controller's door timings.
var (
	// sweepInterval is how often unacknowledged alarms are re-examined.
	sweepInterval = 5 * time.Minute
	// repageAfter is how long an alarm must sit unacknowledged before the first
	// reminder. Long enough that an operator who acts on the original page is never
	// paged twice.
	repageAfter = 15 * time.Minute
	// maxRepages bounds reminders per alarm. Two is a deliberate choice: the point is
	// to survive one missed notification, not to nag until acknowledged.
	maxRepages = 2
	// lookback bounds how far back the sweep looks, so a backlog of never-acknowledged
	// alarms from months ago cannot be picked up when this is first enabled.
	lookback = 24 * time.Hour
	// batchLimit bounds one sweep's query.
	batchLimit = 200
)

// urgentTypes are the notify types worth a reminder. held and no_entry are
// deliberately absent: they are the high-volume diagnostics, and re-paging them is
// how a reminder feature becomes the reason notifications get switched off.
var urgentTypes = map[string]bool{
	notify.TypeForced:            true,
	notify.TypeIntrusion:         true,
	notify.TypeFire:              true,
	notify.TypeControllerOffline: true,
}

// Sweeper periodically re-notifies about unacknowledged alarms. It owns its own
// lifetime (like armrelease.Releaser and health.Monitor).
type Sweeper struct {
	app  core.App
	send notify.SendFunc
	log  *logger.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a Sweeper. send is the same SendFunc the notify sink uses, so the
// opt-in rules are shared rather than reimplemented.
func New(app core.App, send notify.SendFunc, log *logger.Logger) *Sweeper {
	return &Sweeper{app: app, send: send, log: log.With("component", "repage")}
}

// Start launches the sweep loop on its own context (cancelled by Stop).
func (s *Sweeper) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	go s.loop(ctx)
	s.log.Info("unacknowledged-alarm re-page started",
		"every", sweepInterval, "after", repageAfter, "max", maxRepages)
}

// Stop ends the sweep loop and waits for it to exit.
func (s *Sweeper) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Sweeper) loop(ctx context.Context) {
	defer s.wg.Done()
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sweep(time.Now().UTC())
		}
	}
}

// sweep re-pages every alarm that is still unacknowledged, old enough, urgent
// enough, and under the reminder cap. The repage count is stored on the row itself
// (events.repage_count), so the bound survives an accessd restart — an in-memory
// counter would let a restart loop reset the cap and page forever.
func (s *Sweeper) sweep(now time.Time) {
	cutoff, err := types.ParseDateTime(now.Add(-repageAfter))
	if err != nil {
		s.log.Error("repage cutoff parse failed", "error", err)
		return
	}
	floor, err := types.ParseDateTime(now.Add(-lookback))
	if err != nil {
		s.log.Error("repage floor parse failed", "error", err)
		return
	}

	rows, err := s.app.FindRecordsByFilter("events",
		"acknowledged = false && ts != '' && ts < {:cutoff} && ts > {:floor} && repage_count < {:max}",
		"ts", batchLimit, 0,
		dbx.Params{"cutoff": cutoff, "floor": floor, "max": maxRepages})
	if err != nil {
		s.log.Error("repage query failed", "error", err)
		return
	}

	for _, rec := range rows {
		ev, ok := eventFrom(rec)
		if !ok || !urgentTypes[ev.NotifyType()] {
			continue
		}
		ev.Repage = rec.GetInt("repage_count") + 1 // 1-based: this is the Nth reminder
		sent, err := s.send(ev)
		if err != nil {
			// A genuine send failure: leave the count alone so the next sweep retries.
			s.log.Error("re-page send failed", "event", rec.Id, "error", err)
			continue
		}
		// Count the attempt even when nobody was opted in, so an alarm whose source or
		// recipients are switched off does not get re-examined every sweep forever.
		rec.Set("repage_count", rec.GetInt("repage_count")+1)
		if err := s.app.Save(rec); err != nil {
			s.log.Error("failed to record re-page", "event", rec.Id, "error", err)
			continue
		}
		if sent {
			s.log.Info("re-paged unacknowledged alarm",
				"event", rec.Id, "type", ev.NotifyType(), "thing", ev.Thing,
				"attempt", rec.GetInt("repage_count"))
		}
	}
}

// eventFrom rebuilds the notify.Event for an events row. The projection stores the
// subject's parts in dedicated columns and the original body in payload, so this is
// a straight read-back rather than a re-parse. ok is false for a row too incomplete
// to route (no location), which cannot resolve a source opt-in anyway.
func eventFrom(rec *core.Record) (notify.Event, bool) {
	location := rec.GetString("location")
	if location == "" {
		return notify.Event{}, false
	}
	// PocketBase hands a JSON field back as types.JSONRaw (the stored bytes), not as
	// a decoded map — so the original event body has to be unmarshalled, not asserted.
	// A malformed payload degrades to an empty body, which NotifyType then classifies
	// as non-pageable: a row we cannot understand is never re-paged.
	body := map[string]any{}
	if raw := rec.GetString("payload"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			return notify.Event{}, false
		}
	}
	ev := notify.Event{
		Location:  location,
		Type:      rec.GetString("type"),
		Thing:     rec.GetString("portal"),
		Kind:      rec.GetString("kind"),
		AlarmType: str(body["type"]),
		Body:      body,
		TS:        rec.GetDateTime("ts").String(),
		Seq:       uint64(rec.GetInt("stream_seq")),
	}
	// Controller liveness carries its state in "status", matching the sink's parse.
	if ev.Kind == "state" {
		ev.AlarmType = str(body["status"])
	}
	return ev, true
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// Package commandapi is accessd's operator command bridge: authenticated HTTP
// routes (the `command` capability) that translate a UI action into a control-plane
// NATS command. The
// controller subscribes to these commands (core NATS, fire-and-forget); accessd
// only publishes. There is no reply — the command is accepted optimistically
// (202) and the truth reconciles asynchronously via the point_status projection
// (the ACC_STATUS device shadow).
//
// Subjects are built solely via internal/subjects, never hand-formatted. The
// actor stamped on every command is the authenticated user's email, so the
// resulting audit row (the controller emits one for grant and posture) attributes
// the action.
//
// Two routes deliberately diverge from "only publishes NATS": alarm-ack
// (/api/events/{id}/ack) and area arm/disarm (/api/areas/{id}/arm…). These write a
// PocketBase record (the ack fields; the area arm_override) rather than publishing
// a fire-and-forget command — arm-state must be DURABLE (a reboot must not silently
// disarm), so it rides the policy KV via the mirror like any other config, not a
// RAM override. Because these are custom-route app.Save() writes, they do NOT trip
// the changelog `*Request` hooks, so each writes its own audit_logs row.
package commandapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stone-age-io/access-control/internal/authz"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/subjects"
)

type bridge struct {
	app  core.App
	nc   *nats.Conn
	subj subjects.Subjects
	log  *logger.Logger
}

// Register wires the command routes onto the serve event's router. Each route
// requires an authenticated user; issuing a door command is an operational write,
// so a per-handler capability check (authz.RequireCapability(CapCommand)) admits
// operators holding `command` (and superusers, the break-glass account).
func Register(se *core.ServeEvent, nc *nats.Conn, subj subjects.Subjects, log *logger.Logger) {
	b := &bridge{app: se.App, nc: nc, subj: subj, log: log.With("component", "commandapi")}
	se.Router.POST("/api/portals/{id}/grant", b.grant).Bind(apis.RequireAuth())
	se.Router.POST("/api/portals/{id}/posture", b.posture).Bind(apis.RequireAuth())
	se.Router.POST("/api/aux-outputs/{id}/output", b.output).Bind(apis.RequireAuth())
	se.Router.POST("/api/events/{id}/ack", b.ackEvent).Bind(apis.RequireAuth())
	se.Router.POST("/api/areas/{id}/arm", b.arm).Bind(apis.RequireAuth())
	se.Router.POST("/api/areas/{id}/disarm", b.disarm).Bind(apis.RequireAuth())
	se.Router.POST("/api/areas/{id}/arm-clear", b.armClear).Bind(apis.RequireAuth())
}

// grant publishes a momentary grant (cmd.grant) for a portal.
func (b *bridge) grant(e *core.RequestEvent) error {
	if err := authz.RequireCapability(e, authz.CapCommand); err != nil {
		return err
	}
	loc, ptype, code, err := b.portalAddr(e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("portal not found", err)
	}
	var body struct {
		Seconds int    `json:"seconds"`
		Reason  string `json:"reason"`
	}
	_ = e.BindBody(&body) // all fields optional; seconds<=0 falls back to the portal pulse

	subject := b.subj.Grant(loc, ptype, code)
	if err := b.publish(subject, map[string]any{
		"seconds": body.Seconds,
		"actor":   actor(e),
		"reason":  body.Reason,
	}); err != nil {
		return e.InternalServerError("failed to publish command", err)
	}
	b.log.Info("grant command published", "portal", code, "actor", actor(e), "seconds", body.Seconds)
	return e.JSON(http.StatusAccepted, map[string]any{"ok": true})
}

// posture installs or clears a runtime posture override (cmd.posture) for a portal.
func (b *bridge) posture(e *core.RequestEvent) error {
	if err := authz.RequireCapability(e, authz.CapCommand); err != nil {
		return err
	}
	loc, ptype, code, err := b.portalAddr(e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("portal not found", err)
	}
	var body struct {
		Posture string `json:"posture"`
		Reason  string `json:"reason"`
	}
	_ = e.BindBody(&body)
	if !validPosture(body.Posture) {
		return e.BadRequestError("invalid posture (want secure/free_access/unlocked/lockdown/disabled/clear)", nil)
	}

	subject := b.subj.Posture(loc, ptype, code)
	if err := b.publish(subject, map[string]any{
		"posture": body.Posture,
		"actor":   actor(e),
		"reason":  body.Reason,
	}); err != nil {
		return e.InternalServerError("failed to publish command", err)
	}
	b.log.Info("posture command published", "portal", code, "posture", body.Posture, "actor", actor(e))
	return e.JSON(http.StatusAccepted, map[string]any{"ok": true})
}

// output drives a named aux output relay (cmd.output) on/off/pulse.
func (b *bridge) output(e *core.RequestEvent) error {
	if err := authz.RequireCapability(e, authz.CapCommand); err != nil {
		return err
	}
	loc, code, err := b.auxOutputAddr(e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("aux output not found", err)
	}
	var body struct {
		Action  string `json:"action"`
		Seconds int    `json:"seconds"`
		Reason  string `json:"reason"`
	}
	_ = e.BindBody(&body)
	if !validAction(body.Action) {
		return e.BadRequestError("invalid action (want on/off/pulse)", nil)
	}

	subject := b.subj.Output(loc, code)
	if err := b.publish(subject, map[string]any{
		"action":  body.Action,
		"seconds": body.Seconds,
		"actor":   actor(e),
		"reason":  body.Reason,
	}); err != nil {
		return e.InternalServerError("failed to publish command", err)
	}
	b.log.Info("output command published", "aux", code, "action", body.Action, "actor", actor(e))
	return e.JSON(http.StatusAccepted, map[string]any{"ok": true})
}

// ackEvent marks an events row acknowledged by the operator. Unlike grant/posture
// (which publish a NATS command), this writes the events record directly — the
// ack lifecycle lives on the projection row in v1. Gated by `command`.
func (b *bridge) ackEvent(e *core.RequestEvent) error {
	if err := authz.RequireCapability(e, authz.CapCommand); err != nil {
		return err
	}
	id := e.Request.PathValue("id")
	rec, err := b.app.FindRecordById("events", id)
	if err != nil {
		return e.NotFoundError("event not found", err)
	}
	rec.Set("acknowledged", true)
	rec.Set("ack_by", actor(e))
	rec.Set("ack_at", types.NowDateTime())
	if err := b.app.Save(rec); err != nil {
		return e.InternalServerError("failed to acknowledge event", err)
	}
	b.writeAudit(e, "events", id, map[string]any{
		"acknowledged": true, "ack_by": actor(e),
	})
	b.log.Info("event acknowledged", "event", id, "actor", actor(e))
	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}

// arm sets an area's durable arm_override to "armed".
func (b *bridge) arm(e *core.RequestEvent) error { return b.setAreaOverride(e, "armed") }

// disarm sets an area's durable arm_override to "disarmed".
func (b *bridge) disarm(e *core.RequestEvent) error { return b.setAreaOverride(e, "disarmed") }

// armClear clears the override, reverting to the effective (scheduled/standing)
// arm-state.
func (b *bridge) armClear(e *core.RequestEvent) error { return b.setAreaOverride(e, "") }

// setAreaOverride writes an area's durable arm_override and lets the mirror
// propagate it to KV, where every participating controller converges. Unlike
// posture (a RAM override published over NATS), arm-state must survive a reboot —
// so this is a record write, not a fire-and-forget command (see the package doc).
// Gated by `command`.
func (b *bridge) setAreaOverride(e *core.RequestEvent, value string) error {
	if err := authz.RequireCapability(e, authz.CapCommand); err != nil {
		return err
	}
	id := e.Request.PathValue("id")
	rec, err := b.app.FindRecordById("areas", id)
	if err != nil {
		return e.NotFoundError("area not found", err)
	}
	rec.Set("arm_override", value)
	if err := b.app.Save(rec); err != nil {
		return e.InternalServerError("failed to set area arm state", err)
	}
	b.writeAudit(e, "areas", id, map[string]any{"arm_override": value})
	b.log.Info("area arm override set", "area", rec.GetString("code"), "override", value, "actor", actor(e))
	return e.JSON(http.StatusOK, map[string]any{"ok": true})
}

// writeAudit records a custom-route record write to audit_logs. Custom routes do
// app.Save (not an API CRUD request), so the changelog `*Request` hooks never
// fire for them — this is the explicit replacement. Fail-safe: the underlying
// write has already committed, so an audit failure is logged, never propagated.
// event_type is "update" (these routes mutate existing records); the route path
// (request_url) disambiguates an ack/arm from an ordinary edit.
func (b *bridge) writeAudit(e *core.RequestEvent, collectionName, recordID string, after map[string]any) {
	col, err := b.app.FindCollectionByNameOrId("audit_logs")
	if err != nil {
		b.log.Error("audit sink unavailable", "error", err)
		return
	}
	rec := core.NewRecord(col)
	rec.Set("event_type", "update")
	rec.Set("collection_name", collectionName)
	rec.Set("record_id", recordID)
	if e.Auth != nil {
		rec.Set("actor_id", e.Auth.Id)
		rec.Set("actor_email", e.Auth.Email())
		rec.Set("actor_collection", e.Auth.Collection().Name)
	}
	rec.Set("request_ip", e.RealIP())
	if e.Request != nil {
		rec.Set("request_method", e.Request.Method)
		rec.Set("request_url", e.Request.URL.Path)
	}
	rec.Set("timestamp", types.NowDateTime())
	if after != nil {
		rec.Set("after", after)
	}
	if err := b.app.Save(rec); err != nil {
		b.log.Error("failed to write audit row", "collection", collectionName, "record", recordID, "error", err)
	}
}

// auxOutputAddr resolves an aux_output record id to (location code, aux code).
func (b *bridge) auxOutputAddr(id string) (loc, code string, err error) {
	if id == "" {
		return "", "", errors.New("missing aux output id")
	}
	rec, err := b.app.FindRecordById("aux_output", id)
	if err != nil {
		return "", "", err
	}
	code = rec.GetString("code")
	locRec, err := b.app.FindRecordById("locations", rec.GetString("location"))
	if err != nil {
		return "", "", fmt.Errorf("aux output %q location unresolved: %w", code, err)
	}
	return locRec.GetString("code"), code, nil
}

// portalAddr resolves a portal record id to the subject triple (location code,
// portal type, portal code). A portal whose location does not resolve is an
// error — without the location code there is no subject to publish to.
func (b *bridge) portalAddr(id string) (loc, ptype, code string, err error) {
	if id == "" {
		return "", "", "", errors.New("missing portal id")
	}
	rec, err := b.app.FindRecordById("portals", id)
	if err != nil {
		return "", "", "", err
	}
	code = rec.GetString("code")
	ptype = rec.GetString("type")
	locRec, err := b.app.FindRecordById("locations", rec.GetString("location"))
	if err != nil {
		return "", "", "", fmt.Errorf("portal %q location unresolved: %w", code, err)
	}
	return locRec.GetString("code"), ptype, code, nil
}

func (b *bridge) publish(subject string, payload map[string]any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return b.nc.Publish(subject, data)
}

// actor returns the authenticated user's email (the route requires auth, so
// e.Auth is set).
func actor(e *core.RequestEvent) string {
	if e.Auth != nil {
		return e.Auth.Email()
	}
	return ""
}

// validPosture reports whether p is a posture the bridge will forward — the five
// settable standing postures (policy.IsSettablePosture, shared with the controller
// so the gates cannot drift) plus "clear" (revert to the effective posture). The
// controller re-validates; this is an early reject of obvious garbage.
func validPosture(p string) bool {
	return policy.IsSettablePosture(p) || p == "clear"
}

// validAction reports whether a is a settable aux-output action.
func validAction(a string) bool {
	switch a {
	case "on", "off", "pulse":
		return true
	default:
		return false
	}
}

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
package commandapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nats-io/nats.go"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
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

package controller

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// CommandHandler subscribes to the control-plane inputs for a location over core
// NATS and drives the Runtime:
//
//	{app}.{location}.{type}.{thing}.cmd.posture {posture, actor, reason, until?}
//	{app}.{location}.{type}.{thing}.cmd.grant   {seconds, actor, reason}
//	{app}.{location}.evt.fire                   {active}
//
// posture installs/clears a runtime override (posture "clear" reverts to the
// standing value); grant momentarily pulses the lock; fire toggles alarm
// suppression. Commands are core NATS (not JetStream) — fire-and-forget control.
type CommandHandler struct {
	location string
	rt       *Runtime
	subj     subjects.Subjects
	log      *logger.Logger
	subs     []*nats.Subscription
}

// NewCommandHandler creates a handler bound to a location's Runtime.
func NewCommandHandler(location string, rt *Runtime, subj subjects.Subjects, log *logger.Logger) *CommandHandler {
	return &CommandHandler{location: location, rt: rt, subj: subj, log: log.With("component", "commands")}
}

// Start subscribes to the command and fire subjects.
func (h *CommandHandler) Start(nc *nats.Conn) error {
	subscriptions := []struct {
		subject string
		handler nats.MsgHandler
	}{
		{h.subj.PostureWildcard(h.location), h.onPosture},
		{h.subj.GrantWildcard(h.location), h.onGrant},
		{h.subj.OutputWildcard(h.location), h.onOutput},
		{h.subj.Fire(h.location), h.onFire},
	}
	for _, s := range subscriptions {
		sub, err := nc.Subscribe(s.subject, s.handler)
		if err != nil {
			h.Stop()
			return fmt.Errorf("subscribe %q: %w", s.subject, err)
		}
		h.subs = append(h.subs, sub)
		h.log.Info("command subscription active", "subject", s.subject)
	}
	return nil
}

// Stop unsubscribes from all command subjects.
func (h *CommandHandler) Stop() {
	for _, s := range h.subs {
		_ = s.Unsubscribe()
	}
	h.subs = nil
}

func (h *CommandHandler) onPosture(msg *nats.Msg) {
	_, _, portal, _, ok := h.subj.ParseCommand(msg.Subject)
	if !ok || portal == "" {
		h.log.Warn("posture command with no portal", "subject", msg.Subject)
		return
	}
	var cmd struct {
		Posture string `json:"posture"`
		Actor   string `json:"actor"`
		Reason  string `json:"reason"`
		Until   string `json:"until"`
	}
	if err := json.Unmarshal(msg.Data, &cmd); err != nil {
		h.log.Error("bad posture command", "subject", msg.Subject, "error", err)
		return
	}
	if cmd.Until != "" {
		// Time-based reversion is delegated to an external scheduler publishing a
		// follow-up command — the controller deliberately grows no ticker.
		h.log.Warn("posture 'until' is not enforced by the controller; ignoring",
			"portal", portal, "until", cmd.Until)
	}

	now := time.Now().UTC()
	if cmd.Posture == "clear" {
		h.rt.ClearPosture(portal, cmd.Actor, cmd.Reason, now)
		h.log.Info("posture override cleared", "portal", portal, "actor", cmd.Actor)
		return
	}
	if !validPosture(cmd.Posture) {
		h.log.Warn("posture command with invalid posture", "portal", portal, "posture", cmd.Posture)
		return
	}
	h.rt.SetPosture(portal, cmd.Posture, cmd.Actor, cmd.Reason, now)
	h.log.Info("posture override set", "portal", portal, "posture", cmd.Posture, "actor", cmd.Actor)
}

func (h *CommandHandler) onGrant(msg *nats.Msg) {
	_, _, portal, _, ok := h.subj.ParseCommand(msg.Subject)
	if !ok || portal == "" {
		h.log.Warn("grant command with no portal", "subject", msg.Subject)
		return
	}
	var cmd struct {
		Seconds int    `json:"seconds"`
		Actor   string `json:"actor"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(msg.Data, &cmd); err != nil {
		h.log.Error("bad grant command", "subject", msg.Subject, "error", err)
		return
	}
	h.rt.Grant(portal, cmd.Seconds, cmd.Actor, cmd.Reason)
}

func (h *CommandHandler) onOutput(msg *nats.Msg) {
	_, _, code, _, ok := h.subj.ParseCommand(msg.Subject)
	if !ok || code == "" {
		h.log.Warn("output command with no aux code", "subject", msg.Subject)
		return
	}
	var cmd struct {
		Action  string `json:"action"`
		Seconds int    `json:"seconds"`
		Actor   string `json:"actor"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(msg.Data, &cmd); err != nil {
		h.log.Error("bad output command", "subject", msg.Subject, "error", err)
		return
	}
	h.rt.DriveOutput(code, cmd.Action, cmd.Seconds, cmd.Actor, cmd.Reason)
}

func (h *CommandHandler) onFire(msg *nats.Msg) {
	var sig struct {
		Active bool `json:"active"`
	}
	if err := json.Unmarshal(msg.Data, &sig); err != nil {
		h.log.Error("bad fire signal", "subject", msg.Subject, "error", err)
		return
	}
	h.rt.SetFire(h.location, sig.Active, time.Now().UTC())
}

// validPosture reports whether p is a settable standing posture (not "clear").
func validPosture(p string) bool {
	switch p {
	case policy.PostureSecure, policy.PostureUnlocked, policy.PostureLockdown, policy.PostureDisabled:
		return true
	default:
		return false
	}
}

package controller

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policy"
)

// CommandHandler subscribes to the control-plane inputs for a site over core
// NATS and drives the Runtime:
//
//	acc.cmd.{site}.{point}.posture {posture, actor, reason, until?}
//	acc.cmd.{site}.{point}.unlock  {seconds, actor, reason}
//	acc.evt.{site}.fire            {active}
//
// posture installs/clears a runtime override (posture "clear" reverts to the
// standing value); unlock momentarily pulses the lock; fire toggles alarm
// suppression. Commands are core NATS (not JetStream) — fire-and-forget control.
type CommandHandler struct {
	site string
	rt   *Runtime
	log  *logger.Logger
	subs []*nats.Subscription
}

// NewCommandHandler creates a handler bound to a site's Runtime.
func NewCommandHandler(site string, rt *Runtime, log *logger.Logger) *CommandHandler {
	return &CommandHandler{site: site, rt: rt, log: log.With("component", "commands")}
}

// Start subscribes to the command and fire subjects.
func (h *CommandHandler) Start(nc *nats.Conn) error {
	subscriptions := []struct {
		subject string
		handler nats.MsgHandler
	}{
		{fmt.Sprintf("acc.cmd.%s.*.posture", h.site), h.onPosture},
		{fmt.Sprintf("acc.cmd.%s.*.unlock", h.site), h.onUnlock},
		{fmt.Sprintf("acc.evt.%s.fire", h.site), h.onFire},
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
	point := subjectToken(msg.Subject, 3)
	if point == "" {
		h.log.Warn("posture command with no point", "subject", msg.Subject)
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
			"point", point, "until", cmd.Until)
	}

	now := time.Now().UTC()
	if cmd.Posture == "clear" {
		h.rt.ClearPosture(point, cmd.Actor, cmd.Reason, now)
		h.log.Info("posture override cleared", "point", point, "actor", cmd.Actor)
		return
	}
	if !validPosture(cmd.Posture) {
		h.log.Warn("posture command with invalid posture", "point", point, "posture", cmd.Posture)
		return
	}
	h.rt.SetPosture(point, cmd.Posture, cmd.Actor, cmd.Reason, now)
	h.log.Info("posture override set", "point", point, "posture", cmd.Posture, "actor", cmd.Actor)
}

func (h *CommandHandler) onUnlock(msg *nats.Msg) {
	point := subjectToken(msg.Subject, 3)
	if point == "" {
		h.log.Warn("unlock command with no point", "subject", msg.Subject)
		return
	}
	var cmd struct {
		Seconds int    `json:"seconds"`
		Actor   string `json:"actor"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(msg.Data, &cmd); err != nil {
		h.log.Error("bad unlock command", "subject", msg.Subject, "error", err)
		return
	}
	h.rt.Unlock(point, cmd.Seconds, cmd.Actor, cmd.Reason)
}

func (h *CommandHandler) onFire(msg *nats.Msg) {
	var sig struct {
		Active bool `json:"active"`
	}
	if err := json.Unmarshal(msg.Data, &sig); err != nil {
		h.log.Error("bad fire signal", "subject", msg.Subject, "error", err)
		return
	}
	h.rt.SetFire(h.site, sig.Active, time.Now().UTC())
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

// subjectToken returns the idx-th dot-separated token of a subject, or "".
func subjectToken(subject string, idx int) string {
	parts := strings.Split(subject, ".")
	if idx < 0 || idx >= len(parts) {
		return ""
	}
	return parts[idx]
}

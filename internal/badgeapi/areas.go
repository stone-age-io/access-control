package badgeapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/policysnapshot"
)

// The badge tier's non-door actions: arming and disarming an area, and driving an aux
// output.
//
//	POST /api/badge/areas/{areaId}/arm
//	POST /api/badge/areas/{areaId}/disarm
//	POST /api/badge/outputs/{outputId}/pulse
//
// # Authorized by the same graph, not by a capability
//
// Each runs the real pure decider — policy.DecideArea / policy.DecideOutput — over a
// live snapshot of ACC_POLICY, exactly as the unlock route runs policy.Decide. So a
// badge holder can never arm, disarm, or drive anything their access groups do not
// grant, their schedule does not currently allow, or their pass has expired for. There
// is no second authorization implementation to drift from the first.
//
// On top of that floor sits the per-record opt-in (migration 1750000039):
// `areas.allow_remote_arm` and `aux_output.allow_remote`, both default false. Being
// granted an area is necessary but not sufficient — disarming a building from a phone
// with nobody present is a different act from disarming it at the keypad by the door.
//
// # Why arm/disarm is a record write and output is a NATS publish
//
// This mirrors internal/commandapi exactly, for the reason in its package doc:
// arm-state must survive a reboot, so it is a DURABLE write to `areas.arm_override`
// that the mirror propagates to KV, where every participating controller converges.
// There is deliberately no `cmd.arm` subject. An aux output, by contrast, is a
// momentary physical act with no state to remember, so it is a fire-and-forget
// `cmd.output` on the command plane.
//
// # What a badge holder deliberately CANNOT do
//
//   - Clear an override (the operator route's `arm-clear`). "Revert to the schedule"
//     is an operator's concept of the system's intent; a holder means "arm" or
//     "disarm". Nothing is lost by omitting it: accessd's internal/armrelease already
//     releases a one-shot disarm on a scheduled area once its base state comes back
//     round to disarmed, so a holder's disarm does not strand the area off-schedule.
//   - Latch an output on. The route drives one verb, `pulse` — a momentary action is
//     self-limiting, where energizing a relay from a phone and walking away is not.
//     This is a choice about what this SURFACE offers, not a claim that on/off/pulse
//     are separately authorized (DecideOutput says they are not); an install that
//     needs latching from a badge should get its own opt-in and its own thinking.
type armAction struct {
	action string // policy.ArmActionArm | policy.ArmActionDisarm
	value  string // the arm_override value to write
}

func (h *handler) registerAreaRoutes(se *core.ServeEvent) {
	se.Router.POST("/api/badge/areas/{areaId}/arm", func(e *core.RequestEvent) error {
		return h.setArm(e, armAction{action: policy.ArmActionArm, value: "armed"})
	}).Bind(apis.RequireAuth(BadgeCollection))
	se.Router.POST("/api/badge/areas/{areaId}/disarm", func(e *core.RequestEvent) error {
		return h.setArm(e, armAction{action: policy.ArmActionDisarm, value: "disarmed"})
	}).Bind(apis.RequireAuth(BadgeCollection))
	se.Router.POST("/api/badge/outputs/{outputId}/pulse", h.pulseOutput).Bind(apis.RequireAuth(BadgeCollection))
}

// actionResponse is the shape both routes answer with — the same {ok, reason} the
// unlock route uses, so the client has one error path for every badge action.
type actionResponse struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason"` // stable policy reason code
}

// --- POST /api/badge/areas/{areaId}/{arm|disarm} ---

func (h *handler) setArm(e *core.RequestEvent, act armAction) error {
	cardholder := e.Auth

	area, err := h.app.FindRecordById("areas", e.Request.PathValue("areaId"))
	if err != nil {
		return e.NotFoundError("area not found", err)
	}
	areaCode := area.GetString("code")

	// The per-area opt-in, checked first so an area that is not remotely armable never
	// consults the policy graph — and so probing one tells the caller nothing about
	// their own access.
	if !area.GetBool("allow_remote_arm") {
		h.auditAction(e, cardholder.Id, "areas", areaCode, "badge_remote_"+act.action, false, "remote_arm_not_allowed")
		return e.ForbiddenError("this area cannot be armed remotely", nil)
	}

	snap, err := h.snapshot(e.Request.Context())
	if err != nil {
		// Fail CLOSED: with no policy we cannot authorize anything.
		h.log.Error("badge arm: policy snapshot unavailable", "cardholder", cardholder.Id, "error", err)
		return e.InternalServerError("policy unavailable", err)
	}

	creds, err := h.credentialsFor(cardholder.Id)
	if err != nil {
		return e.InternalServerError("failed to load credentials", err)
	}
	if len(creds) == 0 {
		h.auditAction(e, cardholder.Id, "areas", areaCode, "badge_remote_"+act.action, false, "no_credential")
		return e.ForbiddenError("no credential is issued to this badge", nil)
	}

	// Any one of the holder's credentials granting is a grant, exactly as at a reader
	// where they would present whichever one works. Report the last denial if none do.
	now := time.Now().UTC()
	reason := "denied"
	for _, cred := range creds {
		d := snap.SimulateArea(cred.GetString("value"), areaCode, act.action, now)
		if !d.Allow {
			reason = d.Reason
			continue
		}
		area.Set("arm_override", act.value)
		if err := h.app.Save(area); err != nil {
			h.log.Error("badge arm: save failed", "area", areaCode, "error", err)
			return e.InternalServerError("failed to set area arm state", err)
		}
		h.auditAction(e, cardholder.Id, "areas", areaCode, "badge_remote_"+act.action, true, d.Reason)
		h.log.Info("badge area arm override set",
			"area", areaCode, "override", act.value, "cardholder", cardholder.Id, "reason", d.Reason)
		return e.JSON(http.StatusOK, actionResponse{OK: true, Reason: d.Reason})
	}

	h.auditAction(e, cardholder.Id, "areas", areaCode, "badge_remote_"+act.action, false, reason)
	h.log.Info("badge area arm denied",
		"area", areaCode, "action", act.action, "cardholder", cardholder.Id, "reason", reason)
	return e.JSON(http.StatusForbidden, actionResponse{OK: false, Reason: reason})
}

// --- POST /api/badge/outputs/{outputId}/pulse ---

func (h *handler) pulseOutput(e *core.RequestEvent) error {
	cardholder := e.Auth

	output, err := h.app.FindRecordById("aux_output", e.Request.PathValue("outputId"))
	if err != nil {
		return e.NotFoundError("aux output not found", err)
	}
	code := output.GetString("code")

	if !output.GetBool("allow_remote") {
		h.auditAction(e, cardholder.Id, "aux_output", code, "badge_remote_output", false, "remote_output_not_allowed")
		return e.ForbiddenError("this output cannot be driven remotely", nil)
	}

	snap, err := h.snapshot(e.Request.Context())
	if err != nil {
		h.log.Error("badge output: policy snapshot unavailable", "cardholder", cardholder.Id, "error", err)
		return e.InternalServerError("policy unavailable", err)
	}

	creds, err := h.credentialsFor(cardholder.Id)
	if err != nil {
		return e.InternalServerError("failed to load credentials", err)
	}
	if len(creds) == 0 {
		h.auditAction(e, cardholder.Id, "aux_output", code, "badge_remote_output", false, "no_credential")
		return e.ForbiddenError("no credential is issued to this badge", nil)
	}

	now := time.Now().UTC()
	reason := "denied"
	for _, cred := range creds {
		d := snap.SimulateOutput(cred.GetString("value"), code, now)
		if !d.Allow {
			reason = d.Reason
			continue
		}
		if err := h.publishOutputPulse(snap, output, code, cardholder.Id); err != nil {
			h.log.Error("badge output: publish failed", "aux", code, "error", err)
			return e.InternalServerError("failed to publish command", err)
		}
		h.auditAction(e, cardholder.Id, "aux_output", code, "badge_remote_output", true, d.Reason)
		h.log.Info("badge aux output pulsed", "aux", code, "cardholder", cardholder.Id, "reason", d.Reason)
		return e.JSON(http.StatusAccepted, actionResponse{OK: true, Reason: d.Reason})
	}

	h.auditAction(e, cardholder.Id, "aux_output", code, "badge_remote_output", false, reason)
	h.log.Info("badge aux output denied", "aux", code, "cardholder", cardholder.Id, "reason", reason)
	return e.JSON(http.StatusForbidden, actionResponse{OK: false, Reason: reason})
}

// publishOutputPulse emits the same cmd.output an operator's output button emits, with
// `seconds: 0` so the controller uses the output's own configured pulse duration —
// a badge holder does not choose how long a relay stays closed.
func (h *handler) publishOutputPulse(snap *policysnapshot.Snapshot, output *core.Record, code, cardholderID string) error {
	locCode, ok := snap.OutputLocation(code)
	if !ok || locCode == "" {
		loc, err := h.app.FindRecordById("locations", output.GetString("location"))
		if err != nil {
			return errors.New("aux output location unresolved: " + code)
		}
		locCode = loc.GetString("code")
	}
	payload, err := json.Marshal(map[string]any{
		"action":  "pulse",
		"seconds": 0,
		"actor":   "badge:" + cardholderID,
		"reason":  "remote_output",
	})
	if err != nil {
		return err
	}
	return h.nc.Publish(h.subj.Output(locCode, code), payload)
}

// --- the /api/badge/me projections ---

// badgeArea is one area on this badge. Like badgePortal it carries the record id (what
// the routes take) and a display name, never the KV code — and never the area's
// membership, peers, or the controllers behind it.
type badgeArea struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Location  string `json:"location"` // human-readable location NAME, not its code
	CanArm    bool   `json:"canArm"`
	CanDisarm bool   `json:"canDisarm"`
	// Remote is the per-area opt-in. False means the holder's grant is real but only
	// usable at a keypad, which the UI says rather than hiding the area.
	Remote bool `json:"remote"`
	// State is the policy INTENT — armed / disarmed / unknown. Not a report from the
	// hardware: see armStateFor and Snapshot.BaseArmState.
	State string `json:"state"`
}

// Area states reported to the client. "unknown" is a real answer, not an error: an
// area whose schedule has not loaded yet has an arm-state we must not guess at.
const (
	AreaArmed    = "armed"
	AreaDisarmed = "disarmed"
	AreaUnknown  = "unknown"
)

// badgeOutput is one aux output on this badge.
type badgeOutput struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Remote   bool   `json:"remote"`
}

// areasForBadge resolves the graph's area codes to display records, dropping any code
// with no PocketBase row (mid-rename, or KV ahead of the DB) rather than showing a
// nameless area. Sorted by name so the list is stable between polls.
func (h *handler) areasForBadge(snap *policysnapshot.Snapshot, cardholderID string, now time.Time) []badgeArea {
	rights := snap.AreasFor(cardholderID)
	codes := make([]string, 0, len(rights))
	for code := range rights {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	out := make([]badgeArea, 0, len(codes))
	for _, code := range codes {
		rec, err := h.app.FindFirstRecordByData("areas", "code", code)
		if err != nil {
			continue
		}
		r := rights[code]
		a := badgeArea{
			ID:        rec.Id,
			Name:      rec.GetString("name"),
			CanArm:    r.CanArm,
			CanDisarm: r.CanDisarm,
			Remote:    rec.GetBool("allow_remote_arm"),
			State:     AreaUnknown,
		}
		if a.Name == "" {
			a.Name = code
		}
		a.State = armStateFor(rec, snap, code, now)
		a.Location = h.locationName(rec.GetString("location"))
		out = append(out, a)
	}
	return out
}

// armStateFor resolves what a badge should say about an area's arm-state: the durable
// override → the scheduled/standing base → "unknown".
//
// The override comes from the POCKETBASE RECORD, not from the policy snapshot, even though
// the snapshot carries a copy. accessd owns that record and the badge routes are what write
// it, so it is authoritative and immediately consistent — whereas the snapshot's copy is
// behind the KV mirror plus a few seconds of snapshot cache. Without this, a holder who
// tapped Disarm and re-read their badge (which the client does immediately, to show the new
// state) would be told "armed", which reads as "it didn't work". That is exactly the bug
// this shape avoids.
//
// The scheduled/standing tiers still come from the snapshot: resolving them needs the
// schedule graph, the location's timezone and its holidays, and they change only when an
// operator edits them — where a few seconds of lag is invisible.
//
// An unresolvable base is reported as "unknown" rather than guessed at. A badge that said
// "disarmed" about an area it could not resolve would be telling the holder the building is
// unprotected.
func armStateFor(rec *core.Record, snap *policysnapshot.Snapshot, code string, now time.Time) string {
	switch rec.GetString("arm_override") {
	case "armed":
		return AreaArmed
	case "disarmed":
		return AreaDisarmed
	}
	armed, resolved := snap.BaseArmState(code, now)
	switch {
	case !resolved:
		return AreaUnknown
	case armed:
		return AreaArmed
	default:
		return AreaDisarmed
	}
}

// outputsForBadge is areasForBadge's sibling for aux outputs.
func (h *handler) outputsForBadge(snap *policysnapshot.Snapshot, cardholderID string) []badgeOutput {
	codes := snap.OutputsFor(cardholderID)
	sort.Strings(codes)

	out := make([]badgeOutput, 0, len(codes))
	for _, code := range codes {
		rec, err := h.app.FindFirstRecordByData("aux_output", "code", code)
		if err != nil {
			continue
		}
		o := badgeOutput{
			ID:       rec.Id,
			Name:     rec.GetString("name"),
			Remote:   rec.GetBool("allow_remote"),
			Location: h.locationName(rec.GetString("location")),
		}
		if o.Name == "" {
			o.Name = code
		}
		out = append(out, o)
	}
	return out
}

// locationName resolves a location record id to its display name (falling back to its
// code, then to empty). Shared by every badge projection, because a badge shows people
// where a thing is, never the location code the wire uses.
func (h *handler) locationName(locationID string) string {
	if locationID == "" {
		return ""
	}
	loc, err := h.app.FindRecordById("locations", locationID)
	if err != nil {
		return ""
	}
	if name := loc.GetString("name"); name != "" {
		return name
	}
	return loc.GetString("code")
}

package policy

import "time"

// This file holds the deciders for the non-portal targets an access group grants:
// an area's arm-state, and an aux output.
//
// # Why these are siblings of Decide rather than part of it
//
// Decide answers one question — "should this credential open this portal right now?"
// — and its evaluation order is a documented contract that runs on every tap on
// every controller. Widening it to cover three target kinds would put a posture gate
// and a strike pulse in the path of a decision that has neither, and would make the
// contract harder to state than the thing it protects. So each target gets its own
// entry point, and the part that must never differ — whether the pass itself is
// usable — is the shared subjectFor ladder.
//
// # Why authorization is pure, when arm-state is not
//
// CLAUDE.md is emphatic that an area's arm-state does not belong in this package:
// resolving it needs the current time against a schedule plus a durable override,
// which is operational state, like posture. That is about RESOLVING the state.
// Deciding whether a person may CHANGE it is a different question with the same
// shape as "may they open this door" — a walk of user → roles → groups against a
// schedule window — so it is pure, table-testable, and belongs here.
//
// A consequence worth stating: nothing here reads an area's current arm-state. A
// grant to disarm is a grant whether the area is armed or not; asking to disarm an
// already-disarmed area is a no-op, not a denial. Keeping the two apart is what lets
// the same function serve accessd's badge routes today and an OSDP keypad at the
// reader later, where the arm-state is resolved somewhere else entirely.

// DecideArea answers "may this credential's holder perform this arm action on this
// area right now?" as a pure function. The caller resolves the area's timezone (once
// per location, as for Decide) and passes the current instant in UTC. action is
// ArmActionArm or ArmActionDisarm.
//
// Evaluation order, deny-overrides first:
//
//  1. Unknown area                  -> deny_unknown_area
//  2. Credential / user deny        -> the shared ladder (see subjectFor)
//  3. Grant walk (roles -> groups): a group containing this area, holding the right
//     for this action, whose schedule window is open now -> allow_area_grant.
//
// The three denials the walk can produce are ordered most-specific-first, because
// they call for different fixes:
//
//   - a group had the area AND the right, but no window was open -> deny_schedule_closed
//   - a group had the area but not this right                    -> deny_no_area_right
//   - no group had the area at all                               -> deny_no_access
//
// An unrecognized action is deny_no_area_right: no right can grant it.
//
// There is deliberately NO posture-style gate here. Posture is a door concept
// (whether the strike is held, whether the credential is consulted); an area has no
// equivalent, and inventing one — "a lockdown blocks disarming" — would be a policy
// decision this function has no business making.
func DecideArea(p *Policy, loc *time.Location, cred, area, action string, atUTC time.Time) Decision {
	a, ok := p.Areas[area]
	if !ok {
		return Decision{Reason: ReasonDenyUnknownArea}
	}

	u, denial, ok := subjectFor(p, cred, atUTC)
	if !ok {
		return denial
	}

	areaReachable, rightHeld := false, false
	for _, roleCode := range u.Roles {
		role, ok := p.Roles[roleCode]
		if !ok {
			continue // role not yet synced
		}
		for _, groupCode := range role.Groups {
			g, ok := p.Groups[groupCode]
			if !ok {
				continue // group not yet synced
			}
			if _, has := g.Areas[area]; !has {
				continue
			}
			areaReachable = true
			if !g.grantsArmAction(action) {
				continue
			}
			rightHeld = true
			sched, ok := p.Schedules[g.Schedule]
			if !ok {
				continue // schedule not yet synced; can't confirm an open window
			}
			if windowOpen(sched, loc, atUTC, p.Holidays[a.Location]) {
				return Decision{Allow: true, Reason: ReasonAllowAreaGrant, User: u.ID}
			}
		}
	}

	switch {
	case rightHeld:
		return Decision{Reason: ReasonDenyScheduleClosed, User: u.ID}
	case areaReachable:
		return Decision{Reason: ReasonDenyNoAreaRight, User: u.ID}
	default:
		return Decision{Reason: ReasonDenyNoAccess, User: u.ID}
	}
}

// DecideOutput answers "may this credential's holder drive this aux output right
// now?" — the same walk as DecideArea without the arm/disarm split, because an
// output has one action (drive it) rather than two opposed ones.
//
//  1. Unknown output           -> deny_unknown_output
//  2. Credential / user deny   -> the shared ladder
//  3. Grant walk               -> allow_output_grant, else deny_schedule_closed
//     (a group had it, no window open) or deny_no_access (no group had it)
//
// What the output physically does — on, off, or a momentary pulse — is not
// authorized separately. A relay that unlatches a gate is the same relay whichever
// verb drives it, so a right to drive it is a right to drive it; splitting on/off
// would suggest a distinction the hardware does not have.
func DecideOutput(p *Policy, loc *time.Location, cred, output string, atUTC time.Time) Decision {
	o, ok := p.Outputs[output]
	if !ok {
		return Decision{Reason: ReasonDenyUnknownOutput}
	}

	u, denial, ok := subjectFor(p, cred, atUTC)
	if !ok {
		return denial
	}

	reachable := false
	for _, roleCode := range u.Roles {
		role, ok := p.Roles[roleCode]
		if !ok {
			continue
		}
		for _, groupCode := range role.Groups {
			g, ok := p.Groups[groupCode]
			if !ok {
				continue
			}
			if _, has := g.Outputs[output]; !has {
				continue
			}
			reachable = true
			sched, ok := p.Schedules[g.Schedule]
			if !ok {
				continue
			}
			if windowOpen(sched, loc, atUTC, p.Holidays[o.Location]) {
				return Decision{Allow: true, Reason: ReasonAllowOutputGrant, User: u.ID}
			}
		}
	}

	if reachable {
		return Decision{Reason: ReasonDenyScheduleClosed, User: u.ID}
	}
	return Decision{Reason: ReasonDenyNoAccess, User: u.ID}
}

// grantsArmAction reports whether this group carries the right for the given arm
// action. An unrecognized action is never granted, so a typo or a future verb fails
// closed rather than matching the first right in the list.
func (g AccessGroup) grantsArmAction(action string) bool {
	switch action {
	case ArmActionArm:
		return g.CanArm
	case ArmActionDisarm:
		return g.CanDisarm
	default:
		return false
	}
}

// ArmRights flattens a wire `areaRights` list into the two booleans AccessGroup
// carries. Unknown entries are ignored (fail closed: they grant nothing), and an
// empty or nil list yields neither right — which is the whole reason a group with
// areas but no rights denies with ReasonDenyNoAreaRight rather than silently
// granting both.
//
// Exported because both mappers from the KV wire — the controller's PolicyStore and
// accessd's policysnapshot — need it, and a second copy of this in either would be a
// place for "empty means both" to creep back in.
func ArmRights(rights []string) (canArm, canDisarm bool) {
	for _, r := range rights {
		switch r {
		case ArmActionArm:
			canArm = true
		case ArmActionDisarm:
			canDisarm = true
		}
	}
	return canArm, canDisarm
}

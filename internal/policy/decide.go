package policy

import "time"

// Decide answers "should this credential open this access point right now?" as a
// pure function. The caller passes the effective posture (standing value from
// policy, possibly overridden by a runtime command), the credential value, the
// access-point code, the access point's timezone (resolved once per site), and
// the current instant in UTC.
//
// Evaluation order is the contract — deny-overrides come first:
//
//  1. Unknown access point        -> deny_unknown_point
//  2. Posture gate:
//        disabled                 -> deny_point_disabled
//        lockdown                 -> deny_lockdown          (beats a valid credential)
//        unlocked                 -> allow_posture_unlocked (free passage; credential not consulted)
//        secure                   -> continue
//  3. Credential / user deny:
//        unknown credential       -> deny_unknown_credential
//        non-active credential    -> deny_revoked
//        unknown or non-active user -> deny_revoked
//  4. Grant (walk roles -> groups): a group that contains this point AND whose
//     schedule window is open now -> allow_grant. If a group contained the point
//     but none were open -> deny_schedule_closed; if none contained it -> deny_no_access.
//
// Anything unrecognized fails closed (deny). Missing referents (a role/group/
// schedule not yet synced) are skipped, which is also fail-safe.
func Decide(p *Policy, loc *time.Location, posture, cred, point string, atUTC time.Time) Decision {
	ap, ok := p.Points[point]
	if !ok {
		return Decision{Reason: ReasonDenyUnknownPoint}
	}

	switch posture {
	case PostureDisabled:
		return Decision{Reason: ReasonDenyPointDisabled}
	case PostureLockdown:
		return Decision{Reason: ReasonDenyLockdown}
	case PostureUnlocked:
		return Decision{Allow: true, Reason: ReasonAllowPostureUnlocked, Pulse: ap.PulseSeconds}
	case PostureSecure:
		// fall through to credential evaluation
	default:
		// Unknown/unset posture: fail closed.
		return Decision{Reason: ReasonDenyPointDisabled}
	}

	c, ok := p.Creds[cred]
	if !ok {
		return Decision{Reason: ReasonDenyUnknownCredential}
	}
	if c.Status != StatusActive {
		return Decision{Reason: ReasonDenyRevoked, User: c.User}
	}
	u, ok := p.Users[c.User]
	if !ok || u.Status != StatusActive {
		// Credential references a user we don't have or who is suspended: deny.
		return Decision{Reason: ReasonDenyRevoked, User: c.User}
	}

	pointReachable := false
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
			if _, has := g.Points[point]; !has {
				continue
			}
			pointReachable = true
			sched, ok := p.Schedules[g.Schedule]
			if !ok {
				continue // schedule not yet synced; can't confirm an open window
			}
			if windowOpen(sched, loc, atUTC) {
				return Decision{Allow: true, Reason: ReasonAllowGrant, User: u.ID, Pulse: ap.PulseSeconds}
			}
		}
	}

	if pointReachable {
		return Decision{Reason: ReasonDenyScheduleClosed, User: u.ID}
	}
	return Decision{Reason: ReasonDenyNoAccess, User: u.ID}
}

// windowOpen reports whether the given schedule has an open window at atUTC,
// evaluated in the access point's local timezone. The UTC→local conversion
// happens exactly once. Windows that cross midnight (End <= Start) are split
// into a tail segment on the start day and a head segment on the following day,
// each gated by the correct weekday.
func windowOpen(s Schedule, loc *time.Location, atUTC time.Time) bool {
	lt := atUTC.In(loc)
	nowMin := lt.Hour()*60 + lt.Minute()
	todayWD := isoWeekday(lt)
	yesterdayWD := isoWeekday(lt.AddDate(0, 0, -1))

	for _, w := range s.Windows {
		start, ok1 := parseHHMM(w.Start)
		end, ok2 := parseHHMM(w.End)
		if !ok1 || !ok2 {
			continue // malformed window never opens
		}
		if end > start {
			// Same-day window [start, end).
			if nowMin >= start && nowMin < end && dayIn(w.Days, todayWD) {
				return true
			}
		} else {
			// Crosses midnight. Tail [start, 1440) belongs to today's weekday;
			// head [0, end) belongs to a window that opened on yesterday's weekday.
			if nowMin >= start && dayIn(w.Days, todayWD) {
				return true
			}
			if nowMin < end && dayIn(w.Days, yesterdayWD) {
				return true
			}
		}
	}
	return false
}

// parseHHMM parses "HH:MM" into minutes since local midnight (0..1440).
// "24:00" is accepted (end-of-day). Returns false on any malformed input.
func parseHHMM(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	if !isDigit(s[0]) || !isDigit(s[1]) || !isDigit(s[3]) || !isDigit(s[4]) {
		return 0, false
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if m > 59 || h > 24 || (h == 24 && m != 0) {
		return 0, false
	}
	return h*60 + m, true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// isoWeekday maps Go's Sunday=0..Saturday=6 to ISO Monday=1..Sunday=7.
func isoWeekday(t time.Time) int {
	wd := int(t.Weekday())
	if wd == 0 {
		return 7
	}
	return wd
}

func dayIn(days []int, wd int) bool {
	for _, d := range days {
		if d == wd {
			return true
		}
	}
	return false
}

package demoseed

import (
	"fmt"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// Backdated event history.
//
// The `events` collection is a REBUILDABLE PROJECTION of the ACC_EVENTS
// JetStream — the audit consumer writes it, and JetStream stays the system of
// record. Writing rows here directly is therefore a demo-only shortcut and not
// something any other code should copy: nothing replays these into the stream,
// and a `stream_seq` is deliberately left unset so a seeded row is
// distinguishable from a consumed one.
//
// What the rows are FOR is the part worth getting right. An events list of
// nothing but grants shows a working system and teaches nothing; the value of
// this screen is that a denial names its cause. So the mix below is weighted to
// put every reason code a reader will meet into the list — including the two
// that look identical on a card reader and are not: a suspended PERSON with a
// good card (deny_revoked via the user ladder) and an active person with a
// revoked CARD (deny_revoked via the credential ladder).

// eventSpec is one generated tap, with the odds it is chosen.
type eventSpec struct {
	Portal string
	// Holder is the cardholder external_id; its credential is looked up.
	Holder string
	Allow  bool
	Reason string
	Source string
	Weight int
	// Hours bounds the hour-of-day the event lands in, so a warehouse tap does
	// not appear at 03:00 on a site whose shift ends at 22:00.
	LoHour, HiHour int
}

var eventMix = []eventSpec{
	// The ordinary day: people badging in where they are granted.
	{Portal: "kc-dc1-main", Holder: "nw-elena", Allow: true, Reason: policy.ReasonAllowGrant, Source: subjects.SourceOSDP, Weight: 26, LoHour: 5, HiHour: 21},
	{Portal: "kc-dc1-dock-a", Holder: "nw-elena", Allow: true, Reason: policy.ReasonAllowGrant, Source: subjects.SourceOSDP, Weight: 22, LoHour: 5, HiHour: 21},
	{Portal: "kc-dc1-dock-a", Holder: "nw-marco", Allow: true, Reason: policy.ReasonAllowGrant, Source: subjects.SourceOSDP, Weight: 20, LoHour: 5, HiHour: 21},
	{Portal: "kc-dc1-freezer-1", Holder: "nw-marco", Allow: true, Reason: policy.ReasonAllowGrant, Source: subjects.SourceOSDP, Weight: 14, LoHour: 5, HiHour: 21},
	{Portal: "sgf-xd2-main", Holder: "nw-owen", Allow: true, Reason: policy.ReasonAllowGrant, Source: subjects.SourceOSDP, Weight: 12, LoHour: 5, HiHour: 20},
	{Portal: "sgf-xd2-dock-b", Holder: "nw-elena", Allow: true, Reason: policy.ReasonAllowGrant, Source: subjects.SourceOSDP, Weight: 10, LoHour: 5, HiHour: 20},
	{Portal: "kc-dc1-mdf", Holder: "nw-dana", Allow: true, Reason: policy.ReasonAllowGrant, Source: subjects.SourceOSDP, Weight: 6, LoHour: 8, HiHour: 18},
	{Portal: "kc-dc1-yard", Holder: "nw-owen", Allow: true, Reason: policy.ReasonAllowGrant, Source: subjects.SourceOSDP, Weight: 9, LoHour: 5, HiHour: 21},

	// The office lobby is `unlocked` during business hours, so a tap there is
	// allowed WITHOUT the credential being consulted. Different reason, same
	// green row — which is exactly why the reason column exists.
	{Portal: "kc-office-lobby", Holder: "nw-dana", Allow: true, Reason: policy.ReasonAllowPostureUnlocked, Source: subjects.SourceOSDP, Weight: 14, LoHour: 7, HiHour: 17},
	{Portal: "kc-office-lobby", Holder: "nw-raj", Allow: true, Reason: policy.ReasonAllowGrant, Source: subjects.SourceOSDP, Weight: 8, LoHour: 18, HiHour: 22},

	// Remote unlocks: an operator's door-pop and a holder's own badge screen.
	// Distinguished by `source`, which is the whole reason that field exists — a
	// button someone pressed must stay forensically separate from a card read.
	{Portal: "kc-dc1-main", Holder: "nw-tasha", Allow: true, Reason: policy.ReasonAllowCommandGrant, Source: subjects.SourceCommand, Weight: 4, LoHour: 6, HiHour: 20},
	{Portal: "kc-office-lobby", Holder: "nw-dana", Allow: true, Reason: policy.ReasonAllowGrant, Source: subjects.SourceBadge, Weight: 3, LoHour: 7, HiHour: 19},

	// ---- the denials, one per cause a reader will actually meet

	// Right person, wrong hour: the cleaning crew's window closes at 23:00.
	{Portal: "kc-dc1-main", Holder: "nw-priya", Allow: false, Reason: policy.ReasonDenyScheduleClosed, Source: subjects.SourceOSDP, Weight: 7, LoHour: 23, HiHour: 23},
	// Warehouse associate at a comms cabinet: no group grants it.
	{Portal: "kc-dc1-mdf", Holder: "nw-marco", Allow: false, Reason: policy.ReasonDenyNoAccess, Source: subjects.SourceOSDP, Weight: 5, LoHour: 6, HiHour: 20},
	// Enrolled contractor with a working card and no grants at all.
	{Portal: "sgf-xd2-main", Holder: "nw-casey", Allow: false, Reason: policy.ReasonDenyNoAccess, Source: subjects.SourceOSDP, Weight: 4, LoHour: 8, HiHour: 17},
	// Suspended PERSON, active card.
	{Portal: "kc-dc1-dock-a", Holder: "nw-glen", Allow: false, Reason: policy.ReasonDenyRevoked, Source: subjects.SourceOSDP, Weight: 4, LoHour: 5, HiHour: 21},
	// Active person, revoked CARD. Same reason code, different half of the
	// ladder — the credential detail is what tells them apart.
	{Portal: "kc-dc1-dock-a", Holder: "nw-brett", Allow: false, Reason: policy.ReasonDenyRevoked, Source: subjects.SourceOSDP, Weight: 4, LoHour: 5, HiHour: 21},
	// A visitor pass presented after its valid_until.
	{Portal: "kc-office-lobby", Holder: "nw-visit-expired", Allow: false, Reason: policy.ReasonDenyExpired, Source: subjects.SourceOSDP, Weight: 3, LoHour: 8, HiHour: 17},
	// A card nobody enrolled — the most common real-world denial, and the one
	// with no cardholder to show.
	{Portal: "kc-dc1-main", Allow: false, Reason: policy.ReasonDenyUnknownCredential, Source: subjects.SourceOSDP, Weight: 5, LoHour: 5, HiHour: 22},
}

// alarmSpec is one backdated alarm row.
type alarmSpec struct {
	Portal, Location, Kind string
	DaysAgo                int
	LoHour, HiHour         int
	// Acknowledged alarms sit in history; unacknowledged ones are what the Alarm
	// Console is for, so a few are deliberately left open.
	Acked  bool
	AckBy  string
	Detail string
}

var alarmMix = []alarmSpec{
	{Portal: "kc-dc1-dock-a", Location: "KC-DC1", Kind: "held", DaysAgo: 0, LoHour: 9, HiHour: 11,
		Detail: "Door held open past 45s during a pallet transfer."},
	{Portal: "kc-dc1-freezer-1", Location: "KC-DC1", Kind: "held", DaysAgo: 0, LoHour: 13, HiHour: 15,
		Detail: "Freezer door held open past 60s."},
	{Portal: "sgf-xd2-dock-b", Location: "SGF-XD2", Kind: "forced", DaysAgo: 1, LoHour: 2, HiHour: 4,
		Detail: "Door opened with no valid grant and no request-to-exit."},
	{Portal: "kc-dc1-mdf", Location: "KC-DC1", Kind: "forced", DaysAgo: 2, LoHour: 1, HiHour: 3,
		Acked: true, AckBy: "admin@local.dev", Detail: "Comms cabinet forced. Investigated: contractor without a badge."},
	{Portal: "kc-office-server", Location: "KC-OFFICE", Kind: "held", DaysAgo: 3, LoHour: 10, HiHour: 12,
		Acked: true, AckBy: "admin@local.dev", Detail: "Server room propped during a switch swap."},
	{Portal: "kc-dc1-yard", Location: "KC-DC1", Kind: "held", DaysAgo: 4, LoHour: 6, HiHour: 8,
		Acked: true, AckBy: "admin@local.dev", Detail: "Yard gate held for an inbound trailer."},
}

// seedEvents writes the backdated projection rows.
func (s *seeder) seedEvents() error {
	// Credentials by holder, so an event row can carry the value a reader would
	// have presented rather than a made-up one.
	credByHolder := map[string]string{}
	holderName := map[string]string{}
	for _, ch := range cardholders {
		if ch.Credential != "" {
			credByHolder[ch.ExternalID] = ch.Credential
		}
		holderName[ch.ExternalID] = ch.Name
	}
	portalLoc := map[string]string{}
	portalType := map[string]string{}
	for _, p := range portals {
		portalLoc[p.Code] = p.Location
		portalType[p.Code] = p.Type
	}

	// Idempotency: events carry no natural key, so the guard is the count. A
	// re-run tops up to the target rather than looking each row up — which is the
	// right shape for a log, where two similar rows are not a duplicate.
	existing, err := s.app.FindAllRecords("events")
	if err != nil {
		return err
	}
	if len(existing) >= s.opts.Events {
		s.res.Matched["events"] += len(existing)
		return nil
	}

	total := 0
	for _, e := range eventMix {
		total += e.Weight
	}

	// One of EVERY spec first, then weight-fill the remainder.
	//
	// Pure weighted sampling was the first version and it was wrong: the rarest
	// causes carry the lowest weights precisely because they are rare, so
	// deny_expired had roughly a one-in-four chance of being absent from an
	// 80-event history — and at a small --events it would usually be missing.
	// Coverage of the reason codes is the POINT of this history, so it is
	// guaranteed rather than sampled. The weights then shape what the list looks
	// like, which is all they were ever for.
	want := s.opts.Events - len(existing)
	for i := 0; i < want; i++ {
		var spec eventSpec
		if i < len(eventMix) {
			spec = eventMix[i]
		} else {
			spec = s.pickEvent(total)
		}

		// Spread over the last three weeks, weighted toward recent days so the
		// dashboard's "today" counters are not empty.
		daysAgo := -s.rng.Intn(21)
		if s.rng.Intn(3) == 0 {
			daysAgo = -s.rng.Intn(3)
		}
		ts := s.at(daysAgo, spec.LoHour, spec.HiHour)

		cred := credByHolder[spec.Holder]
		if spec.Reason == policy.ReasonDenyUnknownCredential {
			// A card the system has never seen. Deliberately outside the seeded
			// NW-CARD- range so it cannot collide with a real one.
			cred = fmt.Sprintf("UNKNOWN-%06d", s.rng.Intn(999999))
		}

		if err := s.event(map[string]any{
			"location":   portalLoc[spec.Portal],
			"portal":     spec.Portal,
			"type":       portalType[spec.Portal],
			"kind":       "tap",
			"credential": cred,
			"user":       holderName[spec.Holder],
			"allow":      spec.Allow,
			"reason":     spec.Reason,
			"source":     spec.Source,
			"ts":         ts.Format(time.RFC3339),
			"payload": map[string]any{
				"seeded": true,
			},
		}); err != nil {
			return err
		}
	}

	// Alarms last, so the newest rows in the console are the open ones.
	for _, a := range alarmMix {
		ts := s.at(-a.DaysAgo, a.LoHour, a.HiHour)
		row := map[string]any{
			"location":     a.Location,
			"portal":       a.Portal,
			"type":         portalType[a.Portal],
			"kind":         "alarm",
			"allow":        false,
			"reason":       a.Kind,
			"ts":           ts.Format(time.RFC3339),
			"acknowledged": a.Acked,
			"payload": map[string]any{
				"alarm": a.Kind, "detail": a.Detail, "seeded": true,
			},
		}
		if a.Acked {
			row["ack_by"] = a.AckBy
			row["ack_at"] = ts.Add(time.Duration(20+s.rng.Intn(90)) * time.Minute).Format(time.RFC3339)
		}
		if err := s.event(row); err != nil {
			return err
		}
	}
	return nil
}

// pickEvent chooses a spec by weight. Deterministic given the seed.
func (s *seeder) pickEvent(total int) eventSpec {
	n := s.rng.Intn(total)
	for _, e := range eventMix {
		n -= e.Weight
		if n < 0 {
			return e
		}
	}
	return eventMix[len(eventMix)-1]
}

// event writes one projection row. Unlike every other phase this is an
// unconditional insert: see the note at the top of the file.
func (s *seeder) event(set map[string]any) error {
	c, err := s.app.FindCollectionByNameOrId("events")
	if err != nil {
		return err
	}
	rec := core.NewRecord(c)
	for k, v := range set {
		rec.Set(k, v)
	}
	if err := s.app.Save(rec); err != nil {
		return fmt.Errorf("save event: %w", err)
	}
	s.res.mark("events", true)
	return nil
}

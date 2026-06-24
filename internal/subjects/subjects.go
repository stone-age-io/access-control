// Package subjects is the single source of truth for every NATS subject used by
// stone-access: how location, portal-type, and portal codes map onto the tap /
// command / event hierarchy, for both the publishers that build them and the
// consumers that parse them back. Centralizing it here keeps the controller's
// emitters, the controller's command/reader subscriptions, and accessd's audit
// consumer from drifting apart on subject structure.
//
// Every subject leads with the app token ("acc" by default): the access app
// owns the {app}.> subtree, and a portal is a Thing addressed underneath it as
// {app}.{location}.{type}.{thing}. Rooting at a literal app token is what keeps
// the ACC_EVENTS stream's subjects disjoint from every sibling app's stream on a
// shared NATS account — JetStream forbids overlapping stream subjects, and a
// subject that LED with a wildcard (e.g. *.*.*.acc.evt.>) would intersect any
// stream rooted at a literal first token (things.>, cameras.>, kiosk.*.event.>,
// …). accessd and all controllers must construct their Subjects from the SAME
// app token, since they publish and subscribe to each other's traffic. The app
// token is the only value that would change to give a deployment its own
// namespace; until that need is real, callers use Default().
//
// {location}, {type}, {thing} are each a single NATS token (enforced at the
// mirror boundary). {thing} is the portal code; {type} is the portal type.
//
// Subject hierarchy:
//
//	{app}.{location}.{type}.{thing}.tap          credential presentation (reader -> controller)
//	{app}.{location}.{type}.{thing}.cmd.posture  set/clear a runtime posture override
//	{app}.{location}.{type}.{thing}.cmd.grant    momentary grant pulse (operator-initiated)
//	{app}.{location}.evt.fire                    fire-alarm input (location-scoped; control input + audited)
//	{app}.{location}.{type}.{thing}.evt.tap      decision outcome
//	{app}.{location}.{type}.{thing}.evt.state    effective-posture change
//	{app}.{location}.{type}.{thing}.evt.alarm    forced / held-open alarm
//	{app}.{location}.ctrl.{code}.heartbeat       controller liveness (NOT audited)
//
// The {app}.*.…evt subtree is the audit surface; EventsWildcards() captures it
// via two patterns of different fixed arity (location-scoped fire at 4 tokens;
// portal events at 6+), both rooted at the literal {app} so they overlap neither
// each other nor a sibling app's stream.
//
// A controller is addressed under the reserved {app}.{location}.ctrl.{code}
// namespace (ctrl is not a portal type). Its heartbeat sits deliberately OUTSIDE
// the .evt subtree (5 tokens, no evt) so the events stream's {app}.*.*.*.evt.>
// pattern cannot capture it — heartbeats update the controllers record directly,
// not the audit log. Like commands, it rides core NATS, fire-and-forget.
package subjects

import (
	"fmt"
	"strings"
)

// DefaultApp is the app-discriminator segment used unless a deployment overrides it.
const DefaultApp = "acc"

// Subjects builds and parses subjects for one app namespace. The zero value is
// usable and behaves as the default app ("acc").
type Subjects struct {
	app string
}

// New returns a Subjects for the given app token; an empty token falls back to
// DefaultApp.
func New(app string) Subjects { return Subjects{app: app} }

// Default returns a Subjects for DefaultApp.
func Default() Subjects { return Subjects{app: DefaultApp} }

// App is the discriminator segment every subject leads with, defaulting to DefaultApp.
func (s Subjects) App() string {
	if s.app == "" {
		return DefaultApp
	}
	return s.app
}

// --- inbound: presentations & commands ---

// Tap is the subject a reader publishes a credential presentation to.
func (s Subjects) Tap(location, ptype, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.tap", s.App(), location, ptype, thing)
}

// Posture is the subject an operator/issuer publishes a posture command to.
func (s Subjects) Posture(location, ptype, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.cmd.posture", s.App(), location, ptype, thing)
}

// Grant is the subject an operator/issuer publishes a momentary grant command to
// (the same physical effect as a policy grant: a one-shot strike pulse).
func (s Subjects) Grant(location, ptype, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.cmd.grant", s.App(), location, ptype, thing)
}

// TapWildcard is the per-location subscription a controller uses to receive taps
// for any portal at the location.
func (s Subjects) TapWildcard(location string) string {
	return fmt.Sprintf("%s.%s.*.*.tap", s.App(), location)
}

// PostureWildcard is the per-location subscription a controller uses to receive
// posture commands for any of its portals.
func (s Subjects) PostureWildcard(location string) string {
	return fmt.Sprintf("%s.%s.*.*.cmd.posture", s.App(), location)
}

// GrantWildcard is the per-location subscription a controller uses to receive
// grant commands for any of its portals.
func (s Subjects) GrantWildcard(location string) string {
	return fmt.Sprintf("%s.%s.*.*.cmd.grant", s.App(), location)
}

// AuxOutType is the fixed {type} subject segment for auxiliary outputs — they are
// Things addressed like portals, with this reserved type token.
const AuxOutType = "auxout"

// AreaType is the fixed {type} subject segment for areas. An area's intrusion
// alarm is addressed as a Thing of this type — {app}.{location}.area.{areacode}
// .evt.alarm — so it reuses the generic EventAlarm constructor and is captured by
// the existing 6-token portal-event wildcard with no new stream subject.
const AreaType = "area"

// Output is the subject an operator publishes an auxiliary-output command to
// (on/off/pulse). Aux outputs ride the same {app}.{location}.{type}.{thing}
// hierarchy as portals, with the fixed AuxOutType token.
func (s Subjects) Output(location, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.cmd.output", s.App(), location, AuxOutType, thing)
}

// OutputWildcard is the per-location subscription a controller uses to receive
// output commands for any of its aux outputs.
func (s Subjects) OutputWildcard(location string) string {
	return fmt.Sprintf("%s.%s.*.*.cmd.output", s.App(), location)
}

// Fire is the location-scoped fire-alarm-input subject. It lives under evt (not
// cmd) so it is both a control input the controller subscribes to and an audited
// event captured by the events stream.
func (s Subjects) Fire(location string) string {
	return fmt.Sprintf("%s.%s.evt.fire", s.App(), location)
}

// --- outbound: events ---

// EventTap is the subject a decision outcome is emitted to.
func (s Subjects) EventTap(location, ptype, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.evt.tap", s.App(), location, ptype, thing)
}

// EventState is the subject an effective-posture change is emitted to.
func (s Subjects) EventState(location, ptype, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.evt.state", s.App(), location, ptype, thing)
}

// EventAlarm is the subject a forced/held-open alarm is emitted to.
func (s Subjects) EventAlarm(location, ptype, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.evt.alarm", s.App(), location, ptype, thing)
}

// --- controller health (heartbeat) ---

// Heartbeat is the subject a controller publishes periodic liveness to:
// {app}.{location}.ctrl.{code}.heartbeat. It is controller-scoped (not a portal
// event) and lives outside the .evt subtree on purpose, so the events stream
// never captures it; accessd subscribes over core NATS and updates the
// controllers record's last_seen/status directly.
func (s Subjects) Heartbeat(location, code string) string {
	return fmt.Sprintf("%s.%s.ctrl.%s.heartbeat", s.App(), location, code)
}

// HeartbeatWildcard is accessd's subscription for every controller's heartbeat,
// across all locations and codes.
func (s Subjects) HeartbeatWildcard() string {
	return fmt.Sprintf("%s.*.ctrl.*.heartbeat", s.App())
}

// EventsWildcards is the ACC_EVENTS stream's subject set and the audit consumer's
// filter. Two patterns of DIFFERENT fixed arity so they cannot overlap each
// other, both rooted at the literal {app} so they cannot overlap a sibling app's
// stream (JetStream rejects a stream/consumer whose subjects overlap):
//
//	{app}.*.evt.fire        location-scoped fire (exactly 4 tokens)
//	{app}.*.*.*.evt.>       portal events (>=6 tokens)
//
// The fire pattern is fixed-arity (literal `fire`, no trailing `>`) on purpose: a
// `>` there would let it expand to 6+ tokens and overlap the portal pattern. A
// 4-token subject can never satisfy the >=6-token portal pattern, so the two are
// disjoint. Leading with the literal {app} (never a wildcard) is what keeps the
// whole set disjoint from streams rooted at other literals (things.>, cameras.>,
// kiosk.*.event.>, …) on a shared account.
func (s Subjects) EventsWildcards() []string {
	return []string{
		fmt.Sprintf("%s.*.evt.fire", s.App()),
		fmt.Sprintf("%s.*.*.*.evt.>", s.App()),
	}
}

// --- parsing ---

// AlarmWildcards is the notification sink's consumer filter: the alarm/fire subset
// of the events surface, so the sink is never delivered taps/state just to drop
// them. Both patterns are covered by the ACC_EVENTS stream subjects and do not
// overlap each other (6-token alarm vs 4-token fire):
//
//	{app}.*.*.*.evt.alarm   portal/area forced/held/intrusion alarms
//	{app}.*.evt.fire        location-scoped fire input
func (s Subjects) AlarmWildcards() []string {
	return []string{
		fmt.Sprintf("%s.*.*.*.evt.alarm", s.App()),
		fmt.Sprintf("%s.*.evt.fire", s.App()),
	}
}

// ParseEvent splits an event subject into its location/type/thing/kind. Two forms
// are recognized; ok is false for anything else (wrong app, wrong shape):
//
//	{app}.{location}.evt.fire                  -> location, "",   "",    "fire"
//	{app}.{location}.{type}.{thing}.evt.{kind} -> location, type, thing, kind
func (s Subjects) ParseEvent(subject string) (location, ptype, thing, kind string, ok bool) {
	parts := strings.Split(subject, ".")
	switch len(parts) {
	case 4: // app . location . evt . fire
		if parts[0] != s.App() || parts[2] != "evt" || parts[3] != "fire" {
			return "", "", "", "", false
		}
		return parts[1], "", "", "fire", true
	case 6: // app . location . type . thing . evt . kind
		if parts[0] != s.App() || parts[4] != "evt" {
			return "", "", "", "", false
		}
		return parts[1], parts[2], parts[3], parts[5], true
	default:
		return "", "", "", "", false
	}
}

// ParseHeartbeat extracts the location and controller code from a heartbeat
// subject {app}.{location}.ctrl.{code}.heartbeat. ok is false for anything that
// is not a well-formed heartbeat subject (wrong app, wrong shape, not ctrl-scoped).
func (s Subjects) ParseHeartbeat(subject string) (location, code string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 5 || parts[0] != s.App() || parts[2] != "ctrl" || parts[4] != "heartbeat" {
		return "", "", false
	}
	return parts[1], parts[3], true
}

// ParseCommand splits a command subject {app}.{location}.{type}.{thing}.cmd.{action}.
// ok is false for anything that is not a well-formed command subject.
func (s Subjects) ParseCommand(subject string) (location, ptype, thing, action string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 6 || parts[0] != s.App() || parts[4] != "cmd" {
		return "", "", "", "", false
	}
	return parts[1], parts[2], parts[3], parts[5], true
}

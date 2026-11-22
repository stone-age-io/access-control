// Package subjects is the single source of truth for every NATS subject used by
// stone-access: how location, portal-type, and portal codes map onto the tap /
// command / event hierarchy, for both the publishers that build them and the
// consumers that parse them back. Centralizing it here keeps the controller's
// emitters, the controller's command/reader subscriptions, and accessd's audit
// consumer from drifting apart on subject structure.
//
// The grammar is the ecosystem's (ADR 0008 in platform-docs):
//
//	{app}.{scope}.{channel}.{rest}
//
// The app token is "acc" by default, and access control's scope is the location
// code: a fixed one token. The channel comes right after it, so one filter per
// channel covers every location, and the audit stream is the single pattern
// {app}.*.evt.>. Rooting at a literal app token is what keeps the ACC_EVENTS
// stream's subjects disjoint from every sibling app's stream on a shared NATS
// account: JetStream forbids overlapping stream subjects. accessd and all
// controllers must construct their Subjects from the SAME app token, since they
// publish and subscribe to each other's traffic.
//
// {location}, {type}, {thing} are each a single NATS token (enforced at the
// mirror boundary). {thing} is the portal code; {type} is the portal type.
//
// Subject hierarchy:
//
//	{app}.{location}.tap.{type}.{thing}          credential presentation (reader -> controller)
//	{app}.{location}.cmd.{type}.{thing}.posture  set/clear a runtime posture override
//	{app}.{location}.cmd.{type}.{thing}.grant    momentary grant pulse (operator-initiated)
//	{app}.{location}.cmd.auxout.{thing}.output   auxiliary output on/off/pulse
//	{app}.{location}.evt.fire                    fire-alarm input (location-scoped; control input + audited)
//	{app}.{location}.evt.{type}.{thing}.tap      decision outcome
//	{app}.{location}.evt.{type}.{thing}.state    effective-posture change
//	{app}.{location}.evt.{type}.{thing}.alarm    forced / held-open alarm
//	{app}.{location}.evt.ctrl.{code}.state       controller liveness TRANSITION (audited)
//	{app}.{location}.heartbeat.ctrl.{code}       controller liveness beat (NOT audited)
//	{app}.{location}.cmd.ctrl.{code}.health      controller health request/reply (NOT audited)
//
// `tap` is a channel of access control's own: raw reader input, which no stream
// captures. The evt channel is the audit surface. A controller is addressed with
// the reserved {type} ctrl (ctrl is not a portal type). Its heartbeat rides the
// heartbeat channel, so the events stream never captures it: heartbeats update
// the controllers record directly, not the audit log. Like commands, it rides
// core NATS, fire-and-forget.
package subjects

import (
	"fmt"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
)

// DefaultApp is the app-discriminator segment used unless a deployment overrides it.
const DefaultApp = "acc"

// The channels (ADR 0008), plus access control's own raw-input channel.
const (
	ChannelTap       = "tap"
	ChannelCmd       = "cmd"
	ChannelEvt       = "evt"
	ChannelHeartbeat = "heartbeat"
)

// Event `source` values: what produced a tap event. Stable strings that flow
// verbatim into the events projection and the UI, like decision reason codes.
//
// This is the one canonical list, kept here because both sides of the wire stamp
// it: the two READER transports are set at the edge (internal/drivers re-exports
// them as drivers.Source* for the reader implementations), while the two REMOTE
// sources are set centrally by accessd for acts that never touched a reader. That
// distinction is the whole point of the field: a physical read must stay
// forensically distinguishable from a button someone pressed.
const (
	SourceNATS    = "nats"    // simulated tap published on the tap subject (dev)
	SourceOSDP    = "osdp"    // a real card read on the RS485 bus
	SourceCommand = "command" // an operator's grant door-pop
	SourceBadge   = "badge"   // a cardholder's own remote unlock (badge tier)
)

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
	return fmt.Sprintf("%s.%s.%s.%s.%s", s.App(), location, ChannelTap, ptype, thing)
}

// command builds {app}.{location}.cmd.{type}.{thing}.{action}.
func (s Subjects) command(location, ptype, thing, action string) string {
	return fmt.Sprintf("%s.%s.%s.%s.%s.%s", s.App(), location, ChannelCmd, ptype, thing, action)
}

// Posture is the subject an operator/issuer publishes a posture command to.
func (s Subjects) Posture(location, ptype, thing string) string {
	return s.command(location, ptype, thing, "posture")
}

// Grant is the subject an operator/issuer publishes a momentary grant command to
// (the same physical effect as a policy grant: a one-shot strike pulse).
func (s Subjects) Grant(location, ptype, thing string) string {
	return s.command(location, ptype, thing, "grant")
}

// TapWildcard is the per-location subscription a controller uses to receive taps
// for any portal at the location.
func (s Subjects) TapWildcard(location string) string {
	return fmt.Sprintf("%s.%s.%s.*.*", s.App(), location, ChannelTap)
}

// commandWildcard is a per-location subscription to one command action.
func (s Subjects) commandWildcard(location, action string) string {
	return fmt.Sprintf("%s.%s.%s.*.*.%s", s.App(), location, ChannelCmd, action)
}

// PostureWildcard is the per-location subscription a controller uses to receive
// posture commands for any of its portals.
func (s Subjects) PostureWildcard(location string) string {
	return s.commandWildcard(location, "posture")
}

// GrantWildcard is the per-location subscription a controller uses to receive
// grant commands for any of its portals.
func (s Subjects) GrantWildcard(location string) string {
	return s.commandWildcard(location, "grant")
}

// AuxOutType is the fixed {type} subject segment for auxiliary outputs: they are
// Things addressed like portals, with this reserved type token.
const AuxOutType = "auxout"

// AreaType is the fixed {type} subject segment for areas. An area's intrusion
// alarm is addressed as a Thing of this type, {app}.{location}.evt.area.{areacode}
// .alarm, so it reuses the generic EventAlarm constructor and is captured by the
// events stream with no new subject.
const AreaType = "area"

// CtrlType is the reserved {type} subject segment for controllers. A controller's
// heartbeat is {app}.{location}.heartbeat.ctrl.{code}, on its own channel so the
// stream never captures it.
//
// A controller's LIVENESS TRANSITION is a different thing from its heartbeat and
// is addressed as a Thing of this type, {app}.{location}.evt.ctrl.{code}.state,
// so it reuses the generic EventState constructor and is captured by the events
// stream with no new subject. Heartbeats are a flood and stay off the stream; a
// transition is one event per outage and belongs in the timeline.
const CtrlType = "ctrl"

// Output is the subject an operator publishes an auxiliary-output command to
// (on/off/pulse). Aux outputs ride the same hierarchy as portals, with the
// fixed AuxOutType token.
func (s Subjects) Output(location, thing string) string {
	return s.command(location, AuxOutType, thing, "output")
}

// OutputWildcard is the per-location subscription a controller uses to receive
// output commands for any of its aux outputs.
func (s Subjects) OutputWildcard(location string) string {
	return s.commandWildcard(location, "output")
}

// Fire is the location-scoped fire-alarm-input subject. It lives under evt (not
// cmd) so it is both a control input the controller subscribes to and an audited
// event captured by the events stream.
func (s Subjects) Fire(location string) string {
	return fmt.Sprintf("%s.%s.%s.fire", s.App(), location, ChannelEvt)
}

// --- outbound: events ---

// event builds {app}.{location}.evt.{type}.{thing}.{kind}.
func (s Subjects) event(location, ptype, thing, kind string) string {
	return fmt.Sprintf("%s.%s.%s.%s.%s.%s", s.App(), location, ChannelEvt, ptype, thing, kind)
}

// EventTap is the subject a decision outcome is emitted to.
func (s Subjects) EventTap(location, ptype, thing string) string {
	return s.event(location, ptype, thing, "tap")
}

// EventState is the subject an effective-posture change is emitted to.
func (s Subjects) EventState(location, ptype, thing string) string {
	return s.event(location, ptype, thing, "state")
}

// EventAlarm is the subject a forced/held-open alarm is emitted to.
func (s Subjects) EventAlarm(location, ptype, thing string) string {
	return s.event(location, ptype, thing, "alarm")
}

// --- controller health (heartbeat) ---

// Heartbeat is the subject a controller publishes periodic liveness to:
// {app}.{location}.heartbeat.ctrl.{code}. It is on the heartbeat channel, not
// evt, so the events stream never captures it; accessd subscribes over core
// NATS and updates the controllers record's last_seen/status directly.
func (s Subjects) Heartbeat(location, code string) string {
	return fmt.Sprintf("%s.%s.%s.%s.%s", s.App(), location, ChannelHeartbeat, CtrlType, code)
}

// HeartbeatWildcard is accessd's subscription for every controller's heartbeat,
// across all locations and codes.
func (s Subjects) HeartbeatWildcard() string {
	return fmt.Sprintf("%s.*.%s.%s.*", s.App(), ChannelHeartbeat, CtrlType)
}

// Health is the subject a controller answers health requests on:
// {app}.{location}.cmd.ctrl.{code}.health. It is request/reply over core NATS
// on the cmd channel, which no stream captures.
func (s Subjects) Health(location, code string) string {
	return s.command(location, CtrlType, code, "health")
}

// EventsWildcards is the ACC_EVENTS stream's subject set and the audit consumer's
// filter: the whole evt channel at every location, which takes in the
// location-scoped fire input (4 tokens) and every portal, area and controller
// event (6 tokens). Leading with the literal {app} (never a wildcard) is what
// keeps it disjoint from streams rooted at other literals on a shared account.
func (s Subjects) EventsWildcards() []string {
	return []string{fmt.Sprintf("%s.*.%s.>", s.App(), ChannelEvt)}
}

// NotifyWildcards is the notification sinks' consumer filter: the pageable subset
// of the events surface, so a sink is never delivered ordinary taps just to drop
// them. All three patterns are covered by the ACC_EVENTS stream subject and are
// pairwise non-overlapping, which JetStream requires of a consumer's filters:
//
//	{app}.*.evt.*.*.alarm    portal/area forced/held/intrusion alarms
//	{app}.*.evt.fire         location-scoped fire input (4 tokens, disjoint by arity)
//	{app}.*.evt.ctrl.*.state controller liveness transitions
//
// The last is disjoint from the first by its final token, and the fire pattern
// is disjoint from both by arity. The ctrl pattern pins its {type} to the literal
// `ctrl`, so it captures liveness transitions WITHOUT capturing portal or area
// state (posture changes and arm/disarm), which are timeline events nobody
// should be emailed about.
func (s Subjects) NotifyWildcards() []string {
	return []string{
		fmt.Sprintf("%s.*.%s.*.*.alarm", s.App(), ChannelEvt),
		fmt.Sprintf("%s.*.%s.fire", s.App(), ChannelEvt),
		fmt.Sprintf("%s.*.%s.%s.*.state", s.App(), ChannelEvt, CtrlType),
	}
}

// TapEventWildcard is the disarm sink's consumer filter: every portal decision
// event. The sink inspects each for an authorizing credential grant at an
// entry (disarm_on_grant) portal and durably disarms that portal's area.
func (s Subjects) TapEventWildcard() string {
	return fmt.Sprintf("%s.*.%s.*.*.tap", s.App(), ChannelEvt)
}

// --- parsing ---

// IsEvent reports whether subject is on the evt channel, for any app token: the
// subjects the ACC_EVENTS stream captures. A publisher uses it to decide whether
// a message needs a Nats-Msg-Id (ADR 0008 rule 3).
func IsEvent(subject string) bool {
	parts := strings.SplitN(subject, ".", 4)
	return len(parts) >= 4 && parts[2] == ChannelEvt
}

// EventMsg builds the message for one publish. A subject on the evt channel gets
// a Nats-Msg-Id, made once per event, so the stream stores the event once even if
// a publish is retried within its duplicate window. Anything else, such as a
// heartbeat or a command, goes without one: no stream captures it.
func EventMsg(subject string, data []byte) *nats.Msg {
	msg := &nats.Msg{Subject: subject, Data: data}
	if IsEvent(subject) {
		msg.Header = nats.Header{}
		msg.Header.Set(jetstream.MsgIDHeader, nuid.Next())
	}
	return msg
}

// ParseEvent splits an event subject into its location/type/thing/kind. Two forms
// are recognized; ok is false for anything else (wrong app, wrong shape):
//
//	{app}.{location}.evt.fire                  -> location, "",   "",    "fire"
//	{app}.{location}.evt.{type}.{thing}.{kind} -> location, type, thing, kind
func (s Subjects) ParseEvent(subject string) (location, ptype, thing, kind string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) < 4 || parts[0] != s.App() || parts[2] != ChannelEvt {
		return "", "", "", "", false
	}
	switch len(parts) {
	case 4: // app . location . evt . fire
		if parts[3] != "fire" {
			return "", "", "", "", false
		}
		return parts[1], "", "", "fire", true
	case 6: // app . location . evt . type . thing . kind
		return parts[1], parts[3], parts[4], parts[5], true
	default:
		return "", "", "", "", false
	}
}

// ParseHeartbeat extracts the location and controller code from a heartbeat
// subject {app}.{location}.heartbeat.ctrl.{code}. ok is false for anything that
// is not a well-formed heartbeat subject (wrong app, wrong shape, not ctrl-scoped).
func (s Subjects) ParseHeartbeat(subject string) (location, code string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 5 || parts[0] != s.App() || parts[2] != ChannelHeartbeat || parts[3] != CtrlType {
		return "", "", false
	}
	return parts[1], parts[4], true
}

// ParseCommand splits a command subject {app}.{location}.cmd.{type}.{thing}.{action}.
// ok is false for anything that is not a well-formed command subject.
func (s Subjects) ParseCommand(subject string) (location, ptype, thing, action string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 6 || parts[0] != s.App() || parts[2] != ChannelCmd {
		return "", "", "", "", false
	}
	return parts[1], parts[3], parts[4], parts[5], true
}

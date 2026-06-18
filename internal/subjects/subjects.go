// Package subjects is the single source of truth for every NATS subject used by
// stone-access: how location, portal-type, and portal codes map onto the tap /
// command / event hierarchy, for both the publishers that build them and the
// consumers that parse them back. Centralizing it here keeps the controller's
// emitters, the controller's command/reader subscriptions, and accessd's audit
// consumer from drifting apart on subject structure.
//
// Subjects follow the Stone-Age.io platform convention: a portal is a Thing
// addressed {location}.{type}.{thing}, and "acc" is an app-discriminator segment
// placed BEFORE the verb so the audit filter never captures a non-access Thing's
// events on a shared NATS account. accessd and all controllers must construct
// their Subjects from the SAME app token, since they publish and subscribe to
// each other's traffic. The app token is the only value that would change to give
// a deployment its own namespace; until that need is real, callers use Default().
//
// {location}, {type}, {thing} are each a single NATS token (enforced at the
// mirror boundary). {thing} is the portal code; {type} is the portal type.
//
// Subject hierarchy:
//
//	{location}.{type}.{thing}.{app}.tap          credential presentation (reader -> controller)
//	{location}.{type}.{thing}.{app}.cmd.posture  set/clear a runtime posture override
//	{location}.{type}.{thing}.{app}.cmd.unlock   momentary unlock pulse
//	{location}.{app}.evt.fire                    fire-alarm input (location-scoped; control input + audited)
//	{location}.{type}.{thing}.{app}.evt.tap      decision outcome
//	{location}.{type}.{thing}.{app}.evt.state    effective-posture change
//	{location}.{type}.{thing}.{app}.evt.alarm    forced / held-open alarm
//
// The {app}.evt subtree is the audit surface; EventsWildcards() captures it via
// two token-count-disjoint patterns (location-scoped fire is 4 tokens; portal
// events are 6), both pinning a literal {app}.evt so they can't match a foreign
// Thing's events.
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

// App is the discriminator segment every subject carries, defaulting to DefaultApp.
func (s Subjects) App() string {
	if s.app == "" {
		return DefaultApp
	}
	return s.app
}

// --- inbound: presentations & commands ---

// Tap is the subject a reader publishes a credential presentation to.
func (s Subjects) Tap(location, ptype, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.tap", location, ptype, thing, s.App())
}

// Posture is the subject an operator/issuer publishes a posture command to.
func (s Subjects) Posture(location, ptype, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.cmd.posture", location, ptype, thing, s.App())
}

// Unlock is the subject an operator/issuer publishes an unlock command to.
func (s Subjects) Unlock(location, ptype, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.cmd.unlock", location, ptype, thing, s.App())
}

// TapWildcard is the per-location subscription a controller uses to receive taps
// for any portal at the location.
func (s Subjects) TapWildcard(location string) string {
	return fmt.Sprintf("%s.*.*.%s.tap", location, s.App())
}

// PostureWildcard is the per-location subscription a controller uses to receive
// posture commands for any of its portals.
func (s Subjects) PostureWildcard(location string) string {
	return fmt.Sprintf("%s.*.*.%s.cmd.posture", location, s.App())
}

// UnlockWildcard is the per-location subscription a controller uses to receive
// unlock commands for any of its portals.
func (s Subjects) UnlockWildcard(location string) string {
	return fmt.Sprintf("%s.*.*.%s.cmd.unlock", location, s.App())
}

// Fire is the location-scoped fire-alarm-input subject. It lives under evt (not
// cmd) so it is both a control input the controller subscribes to and an audited
// event captured by the events stream.
func (s Subjects) Fire(location string) string {
	return fmt.Sprintf("%s.%s.evt.fire", location, s.App())
}

// --- outbound: events ---

// EventTap is the subject a decision outcome is emitted to.
func (s Subjects) EventTap(location, ptype, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.evt.tap", location, ptype, thing, s.App())
}

// EventState is the subject an effective-posture change is emitted to.
func (s Subjects) EventState(location, ptype, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.evt.state", location, ptype, thing, s.App())
}

// EventAlarm is the subject a forced/held-open alarm is emitted to.
func (s Subjects) EventAlarm(location, ptype, thing string) string {
	return fmt.Sprintf("%s.%s.%s.%s.evt.alarm", location, ptype, thing, s.App())
}

// EventsWildcards is the ACC_EVENTS stream's subject set and the audit consumer's
// filter. Two token-count-disjoint patterns, both pinning a literal {app}.evt:
//
//	{location}.{app}.evt.>           location-scoped fire (4 tokens)
//	{location}.{type}.{thing}.{app}.evt.>   portal events (>=6 tokens)
//
// Neither matches a foreign Thing event (e.g. warehouse-a.camera.cam-042.evt.motion
// has no {app} segment before evt).
func (s Subjects) EventsWildcards() []string {
	return []string{
		fmt.Sprintf("*.%s.evt.>", s.App()),
		fmt.Sprintf("*.*.*.%s.evt.>", s.App()),
	}
}

// --- parsing ---

// ParseEvent splits an event subject into its location/type/thing/kind. Two forms
// are recognized; ok is false for anything else (wrong app, wrong shape):
//
//	{location}.{app}.evt.fire                  -> location, "",   "",    "fire"
//	{location}.{type}.{thing}.{app}.evt.{kind} -> location, type, thing, kind
func (s Subjects) ParseEvent(subject string) (location, ptype, thing, kind string, ok bool) {
	parts := strings.Split(subject, ".")
	switch len(parts) {
	case 4: // location . app . evt . fire
		if parts[1] != s.App() || parts[2] != "evt" || parts[3] != "fire" {
			return "", "", "", "", false
		}
		return parts[0], "", "", "fire", true
	case 6: // location . type . thing . app . evt . kind
		if parts[3] != s.App() || parts[4] != "evt" {
			return "", "", "", "", false
		}
		return parts[0], parts[1], parts[2], parts[5], true
	default:
		return "", "", "", "", false
	}
}

// ParseCommand splits a command subject {location}.{type}.{thing}.{app}.cmd.{action}.
// ok is false for anything that is not a well-formed command subject.
func (s Subjects) ParseCommand(subject string) (location, ptype, thing, action string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 6 || parts[3] != s.App() || parts[4] != "cmd" {
		return "", "", "", "", false
	}
	return parts[0], parts[1], parts[2], parts[5], true
}

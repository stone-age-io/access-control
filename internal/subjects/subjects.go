// Package subjects is the single source of truth for every NATS subject used by
// stone-access: how site and access-point codes map onto the tap / command /
// event hierarchy, for both the publishers that build them and the consumers
// that parse them back. Centralizing it here keeps the controller's emitters,
// the controller's command/reader subscriptions, and accessd's audit consumer
// from drifting apart on subject structure.
//
// Every subject hangs off one root namespace (default "acc"). accessd and all
// controllers must construct their Subjects from the SAME root, since they
// publish and subscribe to each other's traffic. The root is the only value
// that would change to give a deployment its own namespace on a shared NATS
// account; until that need is real, callers use Default().
//
// Subject hierarchy:
//
//	{root}.tap.{site}.{point}            credential presentation (reader -> controller)
//	{root}.cmd.{site}.{point}.posture    set/clear a runtime posture override
//	{root}.cmd.{site}.{point}.unlock     momentary unlock pulse
//	{root}.evt.{site}.fire               fire-alarm input (control input + audited event)
//	{root}.evt.{site}.{point}.tap        decision outcome
//	{root}.evt.{site}.{point}.state      effective-posture change
//	{root}.evt.{site}.{point}.alarm      forced / held-open alarm
//
// The {root}.evt.> subtree is the audit surface: everything under it is captured
// by the ACC_EVENTS stream and projected into the events collection.
package subjects

import (
	"fmt"
	"strings"
)

// DefaultRoot is the subject namespace used unless a deployment overrides it.
const DefaultRoot = "acc"

// Subjects builds and parses subjects under a single root namespace. The zero
// value is usable and behaves as the default root ("acc").
type Subjects struct {
	root string
}

// New returns a Subjects rooted at the given namespace; an empty root falls back
// to DefaultRoot.
func New(root string) Subjects { return Subjects{root: root} }

// Default returns a Subjects rooted at DefaultRoot.
func Default() Subjects { return Subjects{root: DefaultRoot} }

// Root is the namespace every subject hangs off, defaulting to DefaultRoot.
func (s Subjects) Root() string {
	if s.root == "" {
		return DefaultRoot
	}
	return s.root
}

// --- inbound: presentations & commands ---

// Tap is the subject a reader publishes a credential presentation to.
func (s Subjects) Tap(site, point string) string {
	return fmt.Sprintf("%s.tap.%s.%s", s.Root(), site, point)
}

// Posture is the subject an operator/issuer publishes a posture command to.
func (s Subjects) Posture(site, point string) string {
	return fmt.Sprintf("%s.cmd.%s.%s.posture", s.Root(), site, point)
}

// Unlock is the subject an operator/issuer publishes an unlock command to.
func (s Subjects) Unlock(site, point string) string {
	return fmt.Sprintf("%s.cmd.%s.%s.unlock", s.Root(), site, point)
}

// PostureWildcard is the per-site subscription a controller uses to receive
// posture commands for any of its points.
func (s Subjects) PostureWildcard(site string) string {
	return fmt.Sprintf("%s.cmd.%s.*.posture", s.Root(), site)
}

// UnlockWildcard is the per-site subscription a controller uses to receive
// unlock commands for any of its points.
func (s Subjects) UnlockWildcard(site string) string {
	return fmt.Sprintf("%s.cmd.%s.*.unlock", s.Root(), site)
}

// Fire is the site-level fire-alarm-input subject. It lives under evt (not cmd)
// so it is both a control input the controller subscribes to and an audited
// event captured by the events stream.
func (s Subjects) Fire(site string) string {
	return fmt.Sprintf("%s.evt.%s.fire", s.Root(), site)
}

// --- outbound: events ---

// EventTap is the subject a decision outcome is emitted to.
func (s Subjects) EventTap(site, point string) string {
	return fmt.Sprintf("%s.evt.%s.%s.tap", s.Root(), site, point)
}

// EventState is the subject an effective-posture change is emitted to.
func (s Subjects) EventState(site, point string) string {
	return fmt.Sprintf("%s.evt.%s.%s.state", s.Root(), site, point)
}

// EventAlarm is the subject a forced/held-open alarm is emitted to.
func (s Subjects) EventAlarm(site, point string) string {
	return fmt.Sprintf("%s.evt.%s.%s.alarm", s.Root(), site, point)
}

// EventsWildcard matches every event subject — the ACC_EVENTS stream's subject
// set and the audit consumer's filter.
func (s Subjects) EventsWildcard() string {
	return fmt.Sprintf("%s.evt.>", s.Root())
}

// --- parsing ---

// ParseEvent splits an event subject into its site/point/kind. Two forms are
// recognized; ok is false for anything else (wrong root, wrong shape):
//
//	{root}.evt.{site}.fire           -> site, "",    "fire"
//	{root}.evt.{site}.{point}.{kind} -> site, point, kind
func (s Subjects) ParseEvent(subject string) (site, point, kind string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) < 4 || parts[0] != s.Root() || parts[1] != "evt" {
		return "", "", "", false
	}
	switch len(parts) {
	case 4:
		return parts[2], "", parts[3], true
	case 5:
		return parts[2], parts[3], parts[4], true
	default:
		return "", "", "", false
	}
}

// ParseCommand splits a command subject {root}.cmd.{site}.{point}.{action}.
// ok is false for anything that is not a well-formed command subject.
func (s Subjects) ParseCommand(subject string) (site, point, action string, ok bool) {
	parts := strings.Split(subject, ".")
	if len(parts) != 5 || parts[0] != s.Root() || parts[1] != "cmd" {
		return "", "", "", false
	}
	return parts[2], parts[3], parts[4], true
}

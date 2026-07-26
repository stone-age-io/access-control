package badgeapi

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"

	// Side-effect import registers the schema + fixture migrations.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

// TestQRPayload is the security-relevant table in this package: whether a badge's QR
// code is an identifier (opens nothing, safe on a lanyard for years) or the
// credential value itself (works at a scanner, so a photographable key).
func TestQRPayload(t *testing.T) {
	for _, tc := range []struct {
		name       string
		kind       string
		cardID     string
		credValue  string
		wantQR     string
		wantKind   string
		wantSecret bool
	}{
		{
			name: "staff badge identifies and never carries the credential",
			kind: KindHolder, cardID: "ch123", credValue: "CARD-001",
			wantQR: "ch123", wantKind: QRIdentifier, wantSecret: false,
		},
		{
			name: "visitor pass carries the credential and is flagged secret",
			kind: KindVisitor, cardID: "ch456", credValue: "QR-abc123",
			wantQR: "QR-abc123", wantKind: QRCredential, wantSecret: true,
		},
		{
			name: "expired visitor shows no payload rather than a dead code",
			kind: KindVisitor, cardID: "ch456", credValue: "",
			wantQR: "", wantKind: QRCredential, wantSecret: false,
		},
		{
			name: "staff badge with no credential still identifies",
			kind: KindHolder, cardID: "ch789", credValue: "",
			wantQR: "ch789", wantKind: QRIdentifier, wantSecret: false,
		},
		{
			name: "unknown kind falls back to identify-only (fail safe)",
			kind: "", cardID: "ch000", credValue: "CARD-999",
			wantQR: "ch000", wantKind: QRIdentifier, wantSecret: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			qr, kind, secret := qrPayload(tc.kind, tc.cardID, tc.credValue)
			if qr != tc.wantQR || kind != tc.wantKind || secret != tc.wantSecret {
				t.Errorf("qrPayload(%q, %q, %q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.kind, tc.cardID, tc.credValue, qr, kind, secret,
					tc.wantQR, tc.wantKind, tc.wantSecret)
			}
			// The invariant that matters: a non-visitor badge must NEVER echo the
			// credential value, whatever else changes.
			if tc.kind != KindVisitor && tc.credValue != "" && qr == tc.credValue {
				t.Errorf("kind %q leaked the credential value into the QR payload", tc.kind)
			}
		})
	}
}

func newApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

// mkCred builds an unsaved credentials record with the given validity bounds. A zero
// time means "unbounded" (the field is left empty), matching the wire contract.
func mkCred(t *testing.T, app core.App, value string, from, until time.Time) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("credentials")
	if err != nil {
		t.Fatalf("credentials collection: %v", err)
	}
	r := core.NewRecord(col)
	r.Set("value", value)
	r.Set("status", "active")
	if !from.IsZero() {
		dt, err := types.ParseDateTime(from)
		if err != nil {
			t.Fatalf("parse valid_from: %v", err)
		}
		r.Set("valid_from", dt)
	}
	if !until.IsZero() {
		dt, err := types.ParseDateTime(until)
		if err != nil {
			t.Fatalf("parse valid_until: %v", err)
		}
		r.Set("valid_until", dt)
	}
	return r
}

// TestEvaluatePass covers the state a badge holder is shown, which is their ONLY
// explanation of why their badge does not work.
//
// The case that produced it: a cardholder with roles and doors but no credential yet
// was told "your pass is not currently valid" — true, unactionable, and reading like a
// fault in the pass rather than a step nobody had taken. `none` and `expired` are
// different sentences to a person, so they are different states here.
func TestEvaluatePass(t *testing.T) {
	app := newApp(t)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	hour := time.Hour

	unbounded := mkCred(t, app, "UNBOUNDED", time.Time{}, time.Time{})
	current := mkCred(t, app, "CURRENT", now.Add(-hour), now.Add(hour))
	past := mkCred(t, app, "PAST", now.Add(-2*hour), now.Add(-hour))
	longPast := mkCred(t, app, "LONG-PAST", now.Add(-72*hour), now.Add(-48*hour))
	future := mkCred(t, app, "FUTURE", now.Add(hour), now.Add(2*hour))
	farFuture := mkCred(t, app, "FAR-FUTURE", now.Add(48*hour), now.Add(72*hour))

	for _, tc := range []struct {
		name         string
		creds        []*core.Record
		status       string
		wantState    string
		wantDescribe string // credential value, or "" for nil
	}{
		{
			name: "no credential at all is not an expiry", creds: nil, status: "active",
			wantState: PassNone, wantDescribe: "",
		},
		{
			name: "in-window credential is valid", creds: []*core.Record{current}, status: "active",
			wantState: PassValid, wantDescribe: "CURRENT",
		},
		{
			name: "unbounded credential is valid", creds: []*core.Record{unbounded}, status: "active",
			wantState: PassValid, wantDescribe: "UNBOUNDED",
		},
		{
			name: "past window reads as expired", creds: []*core.Record{past}, status: "active",
			wantState: PassExpired, wantDescribe: "PAST",
		},
		{
			name: "future window reads as not yet valid", creds: []*core.Record{future}, status: "active",
			wantState: PassNotYetValid, wantDescribe: "FUTURE",
		},
		{
			// The holder can act on a pass that is coming; one that has gone only explains.
			name:  "a coming pass is described in preference to a gone one",
			creds: []*core.Record{past, future}, status: "active",
			wantState: PassNotYetValid, wantDescribe: "FUTURE",
		},
		{
			name:  "describes the soonest of several coming passes",
			creds: []*core.Record{farFuture, future}, status: "active",
			wantState: PassNotYetValid, wantDescribe: "FUTURE",
		},
		{
			name:  "describes the last of several gone passes",
			creds: []*core.Record{longPast, past}, status: "active",
			wantState: PassExpired, wantDescribe: "PAST",
		},
		{
			// policy.Decide denies a suspended cardholder at the reader, so a badge
			// claiming to be valid would be contradicting the door.
			name:  "a suspended cardholder outranks a live credential",
			creds: []*core.Record{current}, status: "suspended",
			wantState: PassSuspended, wantDescribe: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, describe := evaluatePass(tc.creds, tc.status, now)
			if state != tc.wantState {
				t.Errorf("state = %q, want %q", state, tc.wantState)
			}
			if tc.wantDescribe == "" {
				if describe != nil {
					t.Errorf("describe = %q, want nil", describe.GetString("value"))
				}
				return
			}
			if describe == nil {
				t.Fatalf("describe = nil, want %q", tc.wantDescribe)
			}
			if v := describe.GetString("value"); v != tc.wantDescribe {
				t.Errorf("describe = %q, want %q", v, tc.wantDescribe)
			}
		})
	}
}

// TestActiveCredential covers the window logic that decides whether a badge shows a
// live QR or reads as expired. It mirrors the bounds policy.Decide enforces at the
// edge, so a badge must not claim to be valid when a reader would deny it.
func TestActiveCredential(t *testing.T) {
	app := newApp(t)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	hour := time.Hour

	unbounded := mkCred(t, app, "UNBOUNDED", time.Time{}, time.Time{})
	future := mkCred(t, app, "FUTURE", now.Add(hour), now.Add(2*hour))
	past := mkCred(t, app, "PAST", now.Add(-2*hour), now.Add(-hour))
	current := mkCred(t, app, "CURRENT", now.Add(-hour), now.Add(hour))

	for _, tc := range []struct {
		name  string
		creds []*core.Record
		want  string // credential value, or "" for nil
	}{
		{"no credentials", nil, ""},
		{"unbounded is active", []*core.Record{unbounded}, "UNBOUNDED"},
		{"in-window is active", []*core.Record{current}, "CURRENT"},
		{"not yet valid is not active", []*core.Record{future}, ""},
		{"already expired is not active", []*core.Record{past}, ""},
		{"skips expired, finds the live one", []*core.Record{past, current}, "CURRENT"},
		{"skips not-yet-valid, finds the live one", []*core.Record{future, current}, "CURRENT"},
		{"all out of window", []*core.Record{past, future}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := activeCredential(tc.creds, now)
			if tc.want == "" {
				if got != nil {
					t.Errorf("activeCredential = %q, want nil", got.GetString("value"))
				}
				return
			}
			if got == nil {
				t.Fatalf("activeCredential = nil, want %q", tc.want)
			}
			if v := got.GetString("value"); v != tc.want {
				t.Errorf("activeCredential = %q, want %q", v, tc.want)
			}
		})
	}
}

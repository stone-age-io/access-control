package badgeapi

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

// mkCardholder saves a minimal cardholder to hang a badge login off.
func mkCardholder(t *testing.T, app core.App, name, email string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("cardholders")
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}
	r := core.NewRecord(col)
	r.Set("name", name)
	r.Set("email", email)
	r.Set("status", "active")
	if err := app.Save(r); err != nil {
		t.Fatalf("save cardholder: %v", err)
	}
	return r
}

// TestUpsertHolderBadgeWithoutPassword covers the OTP/OAuth-only path: the record must
// still carry a password (PocketBase requires one) but must NOT claim the holder knows
// it, or a later change would demand a proof they cannot give.
func TestUpsertHolderBadgeWithoutPassword(t *testing.T) {
	app := newApp(t)
	ch := mkCardholder(t, app, "Ada", "ada@example.com")

	rec, err := upsertHolderBadge(app, nil, ch.Id, "ada@example.com", "")
	if err != nil {
		t.Fatalf("upsertHolderBadge: %v", err)
	}

	if got := rec.GetString("kind"); got != KindHolder {
		t.Errorf("kind = %q, want %q", got, KindHolder)
	}
	if rec.GetBool("password_set") {
		t.Error("password_set is true, but no password was supplied — a later change would demand a proof the holder cannot give")
	}
	if rec.GetString("cardholder") != ch.Id {
		t.Errorf("cardholder = %q, want %q", rec.GetString("cardholder"), ch.Id)
	}
	// The record must still be saveable, i.e. the throwaway satisfied PocketBase's
	// non-blank password requirement.
	if rec.Id == "" {
		t.Fatal("badge login was not saved")
	}
}

// TestThrowawayPasswordIsRandom is the reason the placeholder is generated rather than
// a literal: a constant would become a working password for every OTP-only badge in
// every install the moment anyone read the source.
//
// It tests the GENERATOR, not a saved record. Two saved records cannot be compared for
// this — bcrypt salts each hash independently, so two identical plaintexts still
// produce different hashes and the comparison would pass whatever the input was. The
// plain value is not retained on the record after save either.
func TestThrowawayPasswordIsRandom(t *testing.T) {
	const n = 50
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		v, err := newVisitorCredentialValue()
		if err != nil {
			t.Fatalf("newVisitorCredentialValue: %v", err)
		}
		if seen[v] {
			t.Fatalf("generator repeated a value after %d draws: %q", i, v)
		}
		seen[v] = true
		// 16 random bytes as unpadded base32 is 26 chars, plus the "V-" prefix.
		if len(v) < 20 {
			t.Fatalf("throwaway %q is too short to be unguessable", v)
		}
	}
}

// TestUpsertHolderBadgeThrowawayIsUnusable: an OTP-only badge must not be sign-in-able
// with any password an attacker could reasonably try, including the empty string and
// the obvious literals a placeholder might otherwise have been.
func TestUpsertHolderBadgeThrowawayIsUnusable(t *testing.T) {
	app := newApp(t)
	ch := mkCardholder(t, app, "Ada", "ada@example.com")

	rec, err := upsertHolderBadge(app, nil, ch.Id, "ada@example.com", "")
	if err != nil {
		t.Fatalf("upsertHolderBadge: %v", err)
	}

	for _, guess := range []string{
		"", "password", "changeme", "unused", "badge", "throwaway",
		"unused-password-not-a-sign-in-path",
	} {
		if rec.ValidatePassword(guess) {
			t.Errorf("OTP-only badge accepts the guessable password %q", guess)
		}
	}
}

// TestUpsertHolderBadgeWithPassword covers the operator-set path — the one that makes
// the badge tier usable on an install with no SMTP.
func TestUpsertHolderBadgeWithPassword(t *testing.T) {
	app := newApp(t)
	ch := mkCardholder(t, app, "Ada", "ada@example.com")

	const pw = "correct-horse-battery"
	rec, err := upsertHolderBadge(app, nil, ch.Id, "ada@example.com", pw)
	if err != nil {
		t.Fatalf("upsertHolderBadge: %v", err)
	}

	if !rec.GetBool("password_set") {
		t.Error("password_set is false after an operator set a password")
	}
	if !rec.ValidatePassword(pw) {
		t.Error("the supplied password does not validate against the saved record")
	}
}

// TestUpsertHolderBadgeReusesExisting guards the unique index on `cardholder`: a second
// issue for the same person must update their login, not fail on a constraint or leave
// them with two.
func TestUpsertHolderBadgeReusesExisting(t *testing.T) {
	app := newApp(t)
	ch := mkCardholder(t, app, "Ada", "ada@example.com")

	first, err := upsertHolderBadge(app, nil, ch.Id, "ada@example.com", "")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	const pw = "reset-by-the-front-desk"
	second, err := upsertHolderBadge(app, first, ch.Id, "ada@example.com", pw)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if second.Id != first.Id {
		t.Errorf("re-issue created a new record (%q -> %q); the cardholder index allows only one", first.Id, second.Id)
	}
	if !second.GetBool("password_set") || !second.ValidatePassword(pw) {
		t.Error("re-issuing with a password did not set it")
	}

	all, err := app.FindRecordsByFilter(BadgeCollection, "cardholder = {:c}", "", 0, 0,
		map[string]any{"c": ch.Id})
	if err != nil {
		t.Fatalf("list badge logins: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("cardholder has %d badge logins, want 1", len(all))
	}
}

// TestUpsertHolderBadgeKeepsPasswordWhenOmitted: re-issuing to change only the email
// must not silently wipe a password the holder is relying on.
func TestUpsertHolderBadgeKeepsPasswordWhenOmitted(t *testing.T) {
	app := newApp(t)
	ch := mkCardholder(t, app, "Ada", "ada@example.com")

	const pw = "correct-horse-battery"
	first, err := upsertHolderBadge(app, nil, ch.Id, "ada@example.com", pw)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second, err := upsertHolderBadge(app, first, ch.Id, "ada.l@example.com", "")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if !second.GetBool("password_set") {
		t.Error("password_set was cleared by a re-issue that supplied no password")
	}
	if !second.ValidatePassword(pw) {
		t.Error("the existing password stopped working after a re-issue that supplied no password")
	}
	if got := second.Email(); got != "ada.l@example.com" {
		t.Errorf("email = %q, want the updated address", got)
	}
}

// TestAuthorizePasswordChange is the gate that decides whether a holder must prove the
// current password. Both directions are security-relevant: demanding a proof nobody can
// give strands an OTP holder, and skipping one where a password exists lets a stolen
// session lock the real holder out.
func TestAuthorizePasswordChange(t *testing.T) {
	app := newApp(t)
	ch := mkCardholder(t, app, "Ada", "ada@example.com")

	const pw = "correct-horse-battery"

	otpOnly, err := upsertHolderBadge(app, nil, ch.Id, "ada@example.com", "")
	if err != nil {
		t.Fatalf("upsert otp-only: %v", err)
	}

	ch2 := mkCardholder(t, app, "Grace", "grace@example.com")
	withPassword, err := upsertHolderBadge(app, nil, ch2.Id, "grace@example.com", pw)
	if err != nil {
		t.Fatalf("upsert with password: %v", err)
	}

	for _, tc := range []struct {
		name    string
		rec     *core.Record
		old     string
		allowed bool
	}{
		{"no password yet: session alone authorizes a first password", otpOnly, "", true},
		{"no password yet: a bogus old password is simply ignored", otpOnly, "whatever", true},
		{"has a password: the current one must be supplied", withPassword, "", false},
		{"has a password: a wrong one is refused", withPassword, "not-it", false},
		{"has a password: the correct one is accepted", withPassword, pw, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := authorizePasswordChange(tc.rec, tc.old)
			if allowed := msg == ""; allowed != tc.allowed {
				t.Errorf("authorizePasswordChange = %q (allowed=%v), want allowed=%v", msg, allowed, tc.allowed)
			}
		})
	}
}

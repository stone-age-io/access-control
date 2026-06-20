// Package mirror publishes the PocketBase policy graph into NATS KV, one key
// per record. It binds PocketBase record-change hooks: each create/update
// writes the record's KV key; each delete removes it. Relations are resolved to
// stable codes when marshaling, per the policykv contract.
//
// This is deliberately dumb: one record in, one key out. No aggregation, no
// whole-policy rebuild, no debounce — a 5k-row CSV import is 5k independent
// puts, and a single revocation is one small key write.
package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/policykv"
)

const opTimeout = 5 * time.Second

// mirroredCollections are the policy collections whose changes are mirrored to
// KV. The events collection is written by the audit consumer, not mirrored.
var mirroredCollections = []string{
	"locations", "schedules", "controllers", "portals", "access_groups",
	"roles", "cardholders", "credentials", "holidays",
}

// Publisher writes policy records to the KV bucket.
type Publisher struct {
	kv  jetstream.KeyValue
	log *logger.Logger
	m   *metrics.Metrics
}

// Register wires the record hooks on the given PocketBase app and returns the
// Publisher (call SyncAll once after registering to reconcile existing data).
// The kv handle is the ACC_POLICY bucket. Errors writing to KV are logged (and
// counted) but do not fail the PocketBase operation — these are after-commit
// hooks, so the record is already persisted and will re-sync on the next change.
func Register(app core.App, kv jetstream.KeyValue, log *logger.Logger, m *metrics.Metrics) *Publisher {
	p := &Publisher{kv: kv, log: log.With("component", "mirror"), m: m}
	app.OnRecordAfterCreateSuccess(mirroredCollections...).BindFunc(p.onCreate)
	app.OnRecordAfterUpdateSuccess(mirroredCollections...).BindFunc(p.onUpdate)
	app.OnRecordAfterDeleteSuccess(mirroredCollections...).BindFunc(p.onDelete)
	return p
}

// SyncAll reconciles the whole KV bucket against PocketBase: it publishes every
// current policy record and prunes any KV key with no backing record. This
// covers records seeded by migrations (which predate the hooks) and any changes
// made while accessd was down — notably credential deletes, which must not
// linger in KV. Idempotent: Put overwrites, and missing keys delete cleanly.
func (p *Publisher) SyncAll(ctx context.Context, app core.App) error {
	expected := make(map[string]struct{})
	published := 0
	for _, col := range mirroredCollections {
		recs, err := app.FindAllRecords(col)
		if err != nil {
			return fmt.Errorf("mirror sync: list %s: %w", col, err)
		}
		for _, r := range recs {
			key, val, err := keyAndValue(app, r)
			if err != nil {
				p.log.Error("mirror sync: build failed", "collection", col, "id", r.Id, "error", err)
				continue
			}
			if _, err := p.kv.Put(ctx, key, val); err != nil {
				p.log.Error("mirror sync: put failed", "key", key, "error", err)
				continue
			}
			expected[key] = struct{}{}
			p.m.IncKVApply("put")
			published++
		}
	}

	pruned := 0
	keys, err := p.kv.Keys(ctx)
	if err != nil && !errors.Is(err, jetstream.ErrNoKeysFound) {
		p.log.Error("mirror sync: list KV keys failed", "error", err)
	} else {
		for _, key := range keys {
			if _, ok := expected[key]; ok {
				continue
			}
			p.del(key) // stale: no backing record
			pruned++
		}
	}

	p.log.Info("policy KV sync complete", "published", published, "pruned", pruned)
	return nil
}

func (p *Publisher) onCreate(e *core.RecordEvent) error {
	p.publish(e.App, e.Record)
	return e.Next()
}

func (p *Publisher) onUpdate(e *core.RecordEvent) error {
	// If the natural key changed (a rename), drop the stale key first.
	if orig := e.Record.Original(); orig != nil {
		if oldKey, err := recordKey(orig); err == nil {
			if newKey, err := recordKey(e.Record); err == nil && oldKey != newKey {
				p.del(oldKey)
			}
		}
	}
	p.publish(e.App, e.Record)
	return e.Next()
}

func (p *Publisher) onDelete(e *core.RecordEvent) error {
	if key, err := recordKey(e.Record); err == nil {
		p.del(key)
	} else {
		p.log.Error("mirror delete: bad key", "collection", e.Record.Collection().Name, "id", e.Record.Id, "error", err)
	}
	return e.Next()
}

func (p *Publisher) publish(app core.App, r *core.Record) {
	key, val, err := keyAndValue(app, r)
	if err != nil {
		p.log.Error("mirror publish: build failed",
			"collection", r.Collection().Name, "id", r.Id, "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if _, err := p.kv.Put(ctx, key, val); err != nil {
		p.log.Error("mirror publish: KV put failed", "key", key, "error", err)
		return
	}
	p.m.IncKVApply("put")
	p.log.Debug("mirrored record", "key", key)
}

func (p *Publisher) del(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := p.kv.Delete(ctx, key); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
		p.log.Error("mirror delete: KV delete failed", "key", key, "error", err)
		return
	}
	p.m.IncKVApply("delete")
	p.log.Debug("removed mirrored record", "key", key)
}

// recordKey derives the KV key from a record's natural key. Cheap: it touches
// only the natural-key field, so it is safe to call on Original() for renames.
func recordKey(r *core.Record) (string, error) {
	switch name := r.Collection().Name; name {
	case "locations":
		return naturalKey(policykv.PrefixLocation, r.GetString("code"))
	case "schedules":
		return naturalKey(policykv.PrefixSched, r.GetString("code"))
	case "portals":
		return naturalKey(policykv.PrefixPortal, r.GetString("code"))
	case "controllers":
		return naturalKey(policykv.PrefixController, r.GetString("code"))
	case "access_groups":
		return naturalKey(policykv.PrefixGroup, r.GetString("code"))
	case "roles":
		return naturalKey(policykv.PrefixRole, r.GetString("code"))
	case "cardholders":
		return naturalKey(policykv.PrefixUser, r.Id)
	case "credentials":
		return naturalKey(policykv.PrefixCred, r.GetString("value"))
	case "holidays":
		return naturalKey(policykv.PrefixHoliday, r.Id)
	default:
		return "", fmt.Errorf("not a mirrored collection: %s", name)
	}
}

func naturalKey(prefix, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("empty natural key for prefix %q", prefix)
	}
	return prefix + key, nil
}

// keyAndValue builds both the KV key and the marshaled policykv value, resolving
// relations to codes.
func keyAndValue(app core.App, r *core.Record) (string, []byte, error) {
	key, err := recordKey(r)
	if err != nil {
		return "", nil, err
	}

	var payload any
	switch r.Collection().Name {
	case "locations":
		if err := validToken("location code", r.GetString("code")); err != nil {
			return "", nil, err
		}
		payload = policykv.Location{
			Code:        r.GetString("code"),
			Name:        r.GetString("name"),
			Timezone:    r.GetString("timezone"),
			FAISuppress: r.GetBool("fai_suppress"),
		}
	case "schedules":
		payload = policykv.Schedule{
			Code:    r.GetString("code"),
			Windows: parseWindows(r),
			// Stored inverted (default observe): see 1750000003_holidays.go.
			ObserveHolidays: !r.GetBool("ignore_holidays"),
		}
	case "portals":
		if err := validToken("portal code", r.GetString("code")); err != nil {
			return "", nil, err
		}
		if err := validToken("portal type", r.GetString("type")); err != nil {
			return "", nil, err
		}
		posture := r.GetString("posture")
		if posture == "" {
			posture = "secure" // standing default
		}
		// Scheduled posture is both-or-neither: an auto_posture with no
		// auto_schedule (or a schedule that doesn't resolve) is incomplete
		// automation, so drop both (fail-safe: the portal keeps its standing posture).
		autoPosture := r.GetString("auto_posture")
		autoSchedule := resolveCode(app, "schedules", r.GetString("auto_schedule"))
		if autoPosture == "" || autoSchedule == "" {
			autoPosture, autoSchedule = "", ""
		}
		payload = policykv.Portal{
			Code:            r.GetString("code"),
			Type:            r.GetString("type"),
			Location:        resolveCode(app, "locations", r.GetString("location")),
			Posture:         posture,
			PulseSeconds:    r.GetInt("pulse_seconds"),
			AutoPosture:     autoPosture,
			AutoSchedule:    autoSchedule,
			Controller:      resolveCode(app, "controllers", r.GetString("controller")),
			LockRelay:       r.GetInt("lock_relay"),
			DpsInput:        r.GetInt("dps_input"),
			RexInput:        r.GetInt("rex_input"),
			HeldOpenSeconds: r.GetInt("held_open_seconds"),
		}
	case "controllers":
		if err := validToken("controller code", r.GetString("code")); err != nil {
			return "", nil, err
		}
		payload = policykv.Controller{
			Code:     r.GetString("code"),
			Name:     r.GetString("name"),
			Location: resolveCode(app, "locations", r.GetString("location")),
			Model:    r.GetString("model"),
		}
	case "access_groups":
		payload = policykv.AccessGroup{
			Code:     r.GetString("code"),
			Portals:  resolveCodes(app, "portals", r.GetStringSlice("portals")),
			Schedule: resolveCode(app, "schedules", r.GetString("schedule")),
		}
	case "roles":
		payload = policykv.Role{
			Code:   r.GetString("code"),
			Groups: resolveCodes(app, "access_groups", r.GetStringSlice("access_groups")),
		}
	case "cardholders":
		payload = policykv.User{
			ID:     r.Id,
			Status: defaultStr(r.GetString("status"), "active"),
			Roles:  resolveCodes(app, "roles", r.GetStringSlice("roles")),
		}
	case "credentials":
		payload = policykv.Credential{
			Value:      r.GetString("value"),
			User:       r.GetString("user"), // cardholder id (relation value as-is)
			Status:     defaultStr(r.GetString("status"), "active"),
			ValidFrom:  dateRFC3339(r, "valid_from"),
			ValidUntil: dateRFC3339(r, "valid_until"),
		}
	case "holidays":
		payload = policykv.Holiday{
			Location:  resolveCode(app, "locations", r.GetString("location")),
			Date:      dateOnly(r, "date"),
			Recurring: r.GetBool("recurring"),
		}
	default:
		return "", nil, fmt.Errorf("not a mirrored collection: %s", r.Collection().Name)
	}

	val, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("marshal %s: %w", key, err)
	}
	return key, val, nil
}

// parseWindows reads the JSON `windows` field into the wire shape. Marshaling
// then unmarshaling normalizes whatever concrete type PocketBase returns.
func parseWindows(r *core.Record) []policykv.Window {
	raw, err := json.Marshal(r.Get("windows"))
	if err != nil {
		return nil
	}
	var windows []policykv.Window
	_ = json.Unmarshal(raw, &windows)
	return windows
}

// resolveCode returns the `code` of a related record by id, or "" if the id is
// empty or unresolvable (fail-safe: a dangling reference simply won't grant).
func resolveCode(app core.App, collection, id string) string {
	if id == "" {
		return ""
	}
	rec, err := app.FindRecordById(collection, id)
	if err != nil {
		return ""
	}
	return rec.GetString("code")
}

// resolveCodes resolves a slice of related ids to codes, skipping any that
// don't resolve.
func resolveCodes(app core.App, collection string, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if code := resolveCode(app, collection, id); code != "" {
			out = append(out, code)
		}
	}
	return out
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// dateRFC3339 reads a PocketBase DateField as a normalized RFC 3339 UTC string,
// or "" when the field is unset. Keeping the wire format fixed (rather than
// PocketBase's "2006-01-02 15:04:05.000Z") lets the controller parse with a
// single layout.
func dateRFC3339(r *core.Record, field string) string {
	dt := r.GetDateTime(field)
	if dt.IsZero() {
		return ""
	}
	return dt.Time().UTC().Format(time.RFC3339)
}

// dateOnly reads a PocketBase DateField as a "YYYY-MM-DD" calendar day (the date
// part only), or "" when unset. A holiday is a calendar day, not an instant.
func dateOnly(r *core.Record, field string) string {
	dt := r.GetDateTime(field)
	if dt.IsZero() {
		return ""
	}
	return dt.Time().UTC().Format("2006-01-02")
}

// reservedTokens are subject keywords a location/portal code or portal type must
// not collide with, or positional subject parsing would misfire.
var reservedTokens = map[string]bool{
	"acc": true, "evt": true, "cmd": true, "tap": true, "fire": true,
}

// validToken rejects a value that cannot serve as a NATS subject segment: empty,
// containing a separator/wildcard/whitespace, or a reserved keyword. Mirrors the
// single-token rule enforced on subjects.app in config.validate. A record that
// fails this is not mirrored (logged + skipped by the caller), which is fail-safe
// — the portal/location simply never syncs, so the controller default-denies it.
func validToken(what, v string) error {
	if v == "" {
		return fmt.Errorf("%s is empty", what)
	}
	if strings.ContainsAny(v, ". \t*>") {
		return fmt.Errorf("%s %q must be a single NATS token (no '.', '*', '>', or whitespace)", what, v)
	}
	if reservedTokens[v] {
		return fmt.Errorf("%s %q is a reserved subject keyword", what, v)
	}
	return nil
}

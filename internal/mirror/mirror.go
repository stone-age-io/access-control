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
	"sites", "schedules", "access_points", "access_groups",
	"roles", "cardholders", "credentials",
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
	case "sites":
		return naturalKey(policykv.PrefixSite, r.GetString("code"))
	case "schedules":
		return naturalKey(policykv.PrefixSched, r.GetString("code"))
	case "access_points":
		return naturalKey(policykv.PrefixPoint, r.GetString("code"))
	case "access_groups":
		return naturalKey(policykv.PrefixGroup, r.GetString("code"))
	case "roles":
		return naturalKey(policykv.PrefixRole, r.GetString("code"))
	case "cardholders":
		return naturalKey(policykv.PrefixUser, r.Id)
	case "credentials":
		return naturalKey(policykv.PrefixCred, r.GetString("value"))
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
	case "sites":
		payload = policykv.Site{
			Code:        r.GetString("code"),
			Name:        r.GetString("name"),
			Timezone:    r.GetString("timezone"),
			FAISuppress: r.GetBool("fai_suppress"),
		}
	case "schedules":
		payload = policykv.Schedule{
			Code:    r.GetString("code"),
			Windows: parseWindows(r),
		}
	case "access_points":
		posture := r.GetString("posture")
		if posture == "" {
			posture = "secure" // standing default
		}
		payload = policykv.AccessPoint{
			Code:         r.GetString("code"),
			Site:         resolveCode(app, "sites", r.GetString("site")),
			Posture:      posture,
			PulseSeconds: r.GetInt("pulse_seconds"),
		}
	case "access_groups":
		payload = policykv.AccessGroup{
			Code:     r.GetString("code"),
			Points:   resolveCodes(app, "access_points", r.GetStringSlice("access_points")),
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
			Value:  r.GetString("value"),
			User:   r.GetString("user"), // cardholder id (relation value as-is)
			Status: defaultStr(r.GetString("status"), "active"),
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

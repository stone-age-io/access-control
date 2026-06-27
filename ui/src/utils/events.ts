import type { AccessEvent } from '@/types/pocketbase'

/**
 * Single source of truth for how an event is coloured and labelled across the
 * app (Overview, Events, Alarm Console). Before this, each view hand-rolled its
 * own `kindBadge`/`typeBadge`, which drifted apart.
 */

/** Badge class for an event's kind: tap allow/deny, fire/alarm warn, else ghost. */
export function eventKindBadge(e: AccessEvent): string {
  if (e.kind === 'tap') return e.allow ? 'badge-success' : 'badge-error'
  if (e.kind === 'fire' || e.kind === 'alarm') return 'badge-warning'
  return 'badge-ghost'
}

/**
 * PocketBase filter clauses for an event time range, comparing `ts` and falling
 * back to `created` on rows with no `ts` (mirrors the ts||created display + sort).
 * A bare `ts >= from` would drop empty-ts rows; a bare `ts <= to` would wrongly
 * keep them (empty string sorts before any date). Args are UTC ISO strings;
 * '' = no bound. Returns 0–2 clauses to AND into a filter.
 */
export function tsRangeClauses(fromISO: string, toISO: string): string[] {
  const clauses: string[] = []
  if (fromISO) clauses.push(`(ts >= "${fromISO}" || (ts = "" && created >= "${fromISO}"))`)
  if (toISO) clauses.push(`(ts <= "${toISO}" || (ts = "" && created <= "${toISO}"))`)
  return clauses
}

/**
 * Plain-English gloss of a policy.Decide reason code (the stable contract in
 * internal/policy/policy.go). Used by the access simulator and as a tooltip on
 * event reasons. Returns '' for an unrecognized code so callers can fall back to
 * the raw/title-cased value.
 */
const REASON_EXPLANATIONS: Record<string, string> = {
  allow_grant: 'Granted — the cardholder holds a credential with access to this portal, and a granting schedule is open now.',
  allow_posture_unlocked: 'Allowed — the portal posture is Unlocked (strike held open; the credential is not consulted).',
  allow_posture_free_access: 'Allowed — the portal posture is Free Access (any tap opens; the credential is not consulted).',
  allow_command_grant: 'Allowed — an operator-initiated grant (no credential).',
  deny_unknown_credential: "Denied — this credential value isn't in the policy (not enrolled, or not yet synced to the edge).",
  deny_revoked: 'Denied — the credential or its cardholder is revoked or suspended.',
  deny_not_yet_valid: "Denied — the credential isn't valid yet (the time is before its valid-from date).",
  deny_expired: 'Denied — the credential has expired (the time is after its valid-until date).',
  deny_no_access: 'Denied — the cardholder has no access group that grants this portal.',
  deny_schedule_closed: 'Denied — the cardholder can reach this portal, but no granting schedule is open at this time.',
  deny_lockdown: 'Denied — the portal is in Lockdown, which overrides any valid credential.',
  deny_point_disabled: 'Denied — the portal is Disabled (out of service).',
  deny_unknown_point: 'Denied — no portal with this code exists in the policy.',
}

/** Plain-English explanation of a decision reason code, or '' if unrecognized. */
export function reasonExplanation(code: string): string {
  return REASON_EXPLANATIONS[code] || ''
}

/** The specific alarm sub-type (intrusion/forced/held/tamper) from the payload, else the kind. */
export function alarmType(e: AccessEvent): string {
  return (e.payload?.type as string) || e.kind || 'alarm'
}

/**
 * The window the Alarm Console and the Overview headline bound the unacked-alarm
 * query to, so a long-unacked row — or a stream replay that resurrects old rows
 * (the v1 ack-on-projection wart) — can't make the console unusable. A dedicated
 * active_alarms projection is the deferred fix.
 */
export const ALARM_WINDOW_DAYS = 7

/** ISO cutoff for the alarm window: now minus ALARM_WINDOW_DAYS. */
export function alarmWindowCutoffISO(): string {
  return new Date(Date.now() - ALARM_WINDOW_DAYS * 86400000).toISOString()
}

/**
 * PocketBase filter for the unacknowledged alarm/fire set inside the recent
 * window — the single source of truth shared by the Alarm Console and the
 * Overview headline count, so the two can't drift. `extra` clauses (e.g. a type
 * or location narrowing) are AND-ed on.
 */
export function unackedAlarmFilter(extra: string[] = []): string {
  return [
    '(kind = "alarm" || kind = "fire")',
    'acknowledged = false',
    `created > "${alarmWindowCutoffISO()}"`,
    ...extra,
  ].join(' && ')
}

/**
 * Filter clause narrowing the console to a single alarm sub-type. `fire` is its
 * own event kind; the rest live in the alarm payload (`payload.type`), so they
 * narrow within the kind="alarm" half of the set. '' = no narrowing.
 */
export function alarmTypeClause(type: string): string[] {
  if (!type) return []
  if (type === 'fire') return ['kind = "fire"']
  return [`payload.type = "${type}"`]
}

/** Badge class for an alarm row keyed on its specific type (fire/tamper warn, trips error). */
export function alarmTypeBadge(e: AccessEvent): string {
  const t = alarmType(e)
  if (e.kind === 'fire' || t === 'tamper_24h') return 'badge-warning'
  if (t === 'intrusion' || t === 'forced' || t === 'held') return 'badge-error'
  return 'badge-ghost'
}

/**
 * Human label for the "thing" an event happened to. Intrusion alarms name the
 * tripped point in the payload; everything else carries the portal, falling back
 * to the location.
 */
export function eventThing(e: AccessEvent): string {
  const point = e.payload?.point as string | undefined
  if (point) return e.portal ? `${e.portal} · ${point}` : point
  return e.portal || e.location || '—'
}

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

/** The specific alarm sub-type (intrusion/forced/held/tamper) from the payload, else the kind. */
export function alarmType(e: AccessEvent): string {
  return (e.payload?.type as string) || e.kind || 'alarm'
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

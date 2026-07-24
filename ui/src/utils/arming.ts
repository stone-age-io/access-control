import type { PointStatus } from '@/types/pocketbase'
import type { SoftTone } from './badges'

/**
 * Aggregate an area's arm-state from its per-controller arm shadows.
 *
 * An area can span several controllers; each writes its OWN shadow row
 * (point_status kind=area, same code, distinct controller) and stamps the FULL
 * participant set into payload.peers. We use peers as the denominator so a
 * participant that was offline at arm time (and never wrote a shadow) shows as
 * missing rather than being silently ignored:
 *
 *   - armed     every peer reported armed
 *   - disarmed  every peer reported disarmed
 *   - partial   peers disagree, or a peer hasn't reported (arming/converging)
 *   - unknown   no shadow rows at all
 *
 * Falls back to the reporting controllers when no row carries peers (older
 * shadow), which degrades gracefully to "what we can see."
 */
export type ArmState = 'armed' | 'disarmed' | 'partial' | 'unknown'

export interface ArmAggregate {
  state: ArmState
  armed: number
  total: number
}

export function aggregateArm(rows: PointStatus[]): ArmAggregate {
  if (rows.length === 0) return { state: 'unknown', armed: 0, total: 0 }

  const withPeers = rows.find((r) => Array.isArray(r.payload?.peers))
  const peers: string[] =
    (withPeers?.payload?.peers as string[] | undefined) ?? rows.map((r) => r.controller)
  const total = peers.length

  const byController = new Map(rows.map((r) => [r.controller, r.state]))
  let armed = 0
  let disarmed = 0
  for (const p of peers) {
    const st = byController.get(p)
    if (st === 'armed') armed++
    else if (st === 'disarmed') disarmed++
    // missing → neither (pending/converging)
  }

  if (total > 0 && armed === total) return { state: 'armed', armed, total }
  if (total > 0 && disarmed === total) return { state: 'disarmed', armed, total }
  return { state: 'partial', armed, total }
}

/**
 * Roll several areas' aggregated states into one glanceable state — for a
 * location or site summary. Any converging/mixed member ⇒ partial; all-armed or
 * all-disarmed collapse to that; nothing reporting ⇒ unknown.
 */
export function rollupArmStates(states: ArmState[]): ArmState {
  if (!states.length) return 'unknown'
  if (states.some((s) => s === 'partial')) return 'partial'
  const armed = states.filter((s) => s === 'armed').length
  const disarmed = states.filter((s) => s === 'disarmed').length
  if (armed === states.length) return 'armed'
  if (disarmed === states.length) return 'disarmed'
  if (armed === 0 && disarmed === 0) return 'unknown'
  return 'partial'
}

/** Soft-badge tone for an aggregated arm-state (armed = error, converging = warning). */
export function armTone(state: ArmState): SoftTone {
  switch (state) {
    case 'armed':
      return 'error'
    case 'partial':
      return 'warning'
    default:
      return 'neutral' // disarmed, unknown
  }
}

/** A short human label for an aggregated arm-state. */
export function armLabel(agg: ArmAggregate): string {
  switch (agg.state) {
    case 'armed':
      return 'Armed'
    case 'disarmed':
      return 'Disarmed'
    case 'partial':
      return `Arming… (${agg.armed}/${agg.total})`
    default:
      return 'Unknown'
  }
}

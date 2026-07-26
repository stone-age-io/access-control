import type { Cardholder, Credential } from '@/types/pocketbase'
import type { SoftTone } from '@/utils/badges'

/**
 * One visit's pass, reduced to the single state an operator reads.
 *
 * # Why the credential and not the person
 *
 * Validity lives on the CREDENTIAL, which is what the edge enforces: `policy.Decide` reads
 * `valid_from`/`valid_until` at the door, offline included. Anything the console derived
 * from somewhere else — the cardholder row, the badge login, a mint timestamp — could
 * disagree with the doors, and the doors would win.
 *
 * # Why it is shared
 *
 * The list and the detail page both answer "is this pass working". They were separate
 * derivations, which is how the same visit ends up described two ways on two screens — the
 * kind of discrepancy that teaches an operator to stop trusting the console. One function,
 * one answer.
 *
 * # Ordering
 *
 * A suspended CARDHOLDER outranks every credential: `policy.Decide` denies them at a reader
 * regardless, so showing a live pass would contradict the door. After that the newest
 * credential decides, matching what the badge tier's own `evaluatePass` does — a visitor who
 * was reissued a pass is described by the pass they now hold, not the one they used to.
 */
export interface VisitorPass {
  label: string
  tone: SoftTone
  /** The credential the label describes, or null when there is none. */
  credential: Credential | null
}

/**
 * `credentials` may be in any order; the newest is found here rather than assumed, so a
 * caller cannot get a different answer by sorting differently.
 */
export function visitorPassState(
  cardholder: Cardholder | null,
  credentials: Credential[],
): VisitorPass {
  if (cardholder?.status === 'suspended') {
    return { label: 'suspended', tone: 'warning', credential: null }
  }
  if (!credentials.length) {
    return { label: 'no pass', tone: 'neutral', credential: null }
  }

  const newest = [...credentials].sort(
    (a, b) => new Date(b.created || 0).getTime() - new Date(a.created || 0).getTime(),
  )[0]

  if (newest.status === 'revoked') return { label: 'revoked', tone: 'neutral', credential: newest }
  if (newest.status === 'suspended') return { label: 'suspended', tone: 'warning', credential: newest }

  const now = Date.now()
  const until = newest.valid_until ? new Date(newest.valid_until).getTime() : 0
  const from = newest.valid_from ? new Date(newest.valid_from).getTime() : 0
  // An unparseable bound reads as UNBOUNDED rather than as expired: the same fail-direction
  // the edge takes, so the console never claims a pass is dead that the doors will honour.
  if (until && !isNaN(until) && until < now) {
    return { label: 'expired', tone: 'warning', credential: newest }
  }
  if (from && !isNaN(from) && from > now) {
    return { label: 'not yet valid', tone: 'neutral', credential: newest }
  }
  return { label: 'active', tone: 'success', credential: newest }
}

/** The state values the list page offers as a filter, in the order they are shown. */
export const VISITOR_STATES = ['active', 'not yet valid', 'expired', 'revoked', 'no pass'] as const
export type VisitorStateFilter = (typeof VISITOR_STATES)[number]

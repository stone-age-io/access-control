/**
 * What a badge can show, as one list — the single derivation behind both switchers.
 *
 * # Why this is shared and not a component's private computed
 *
 * Two hosts render the same set of views over the same payload: the holder's own
 * BadgeView, which puts them in a bottom navigation bar, and the operator's
 * BadgePreviewModal, which puts them in a row of tabs inside a dialog. They disagree
 * about chrome and must never disagree about CONTENT — a segment the holder sees and the
 * operator does not is exactly the discrepancy the preview exists to rule out.
 *
 * So the list lives here and BadgeAccessPanel became a pure renderer: it is told which
 * view to draw and draws it. Neither host owns the definition.
 *
 * # The adaptive rule
 *
 * A segment exists only if the holder has something in it. A site has hundreds of points
 * and an operator is hunting; a holder typically has a handful of doors and no areas or
 * controls at all, so fixed segments would be mostly-empty chrome over a three-item list.
 * The common badge collapses to Badge + Plan, or Badge + Portals.
 */
import type { BadgeArea, BadgeLive, BadgeMe, BadgeOutput, BadgePortal } from '@/types/badge'

/** One view of what a badge grants. */
export type BadgeViewKey =
  | 'plan'
  | 'portals'
  | 'areas'
  | 'controls'
  | 'onsite'
  /**
   * Nothing is granted at all. It still gets a segment, because "nothing is assigned to
   * you yet" is information a holder needs — a badge that signs in and then offers no way
   * to reach that sentence reads as broken rather than as empty.
   */
  | 'empty'

/** A view, plus the badge FACE — the flattened set a bottom navigation bar shows. */
export type BadgeTabKey = 'badge' | BadgeViewKey

export interface BadgeNavItem {
  key: BadgeTabKey
  label: string
  /** Emoji, not an icon font: five glyphs are not worth a dependency on a phone. */
  icon: string
  /**
   * Shown beside the label. For a list segment it is how many rows are in it. For the
   * plan it is how many SITES there are, omitted at one — the plan shows one picture at a
   * time, so "Plan 1" would be counting the thing already on screen.
   */
  count?: number
}

/**
 * Split a badge's grants by whether they can be driven from the phone.
 *
 * One definition of "remote", used by the nav counts and by the lists themselves, so a
 * segment's count can never disagree with the number of rows under it.
 */
export interface BadgeGrants {
  portals: BadgePortal[]
  areas: BadgeArea[]
  outputs: BadgeOutput[]
}

export function remoteGrants(me: BadgeMe): BadgeGrants {
  return {
    portals: me.portals.filter((p) => p.remoteUnlock),
    areas: me.areas.filter((a) => a.remote),
    outputs: me.outputs.filter((o) => o.remote),
  }
}

/**
 * The inverse: real grants that need a walk. Shown rather than hidden, because a holder
 * who could not see them would believe their badge does less than it does.
 */
export function onSiteGrants(me: BadgeMe): BadgeGrants {
  return {
    portals: me.portals.filter((p) => !p.remoteUnlock),
    areas: me.areas.filter((a) => !a.remote),
    outputs: me.outputs.filter((o) => !o.remote),
  }
}

function total(g: BadgeGrants): number {
  return g.portals.length + g.areas.length + g.outputs.length
}

/**
 * The access views this badge has something to put in, in the order they are shown.
 *
 * `plans` comes from GET /api/badge/live and is normally empty — a floor plan is an
 * upgrade a location opts into. It lands after `me` does, so the Plan segment appears a
 * moment later, which is also why the caller resolves its selection against this list
 * rather than storing an index.
 */
export function badgeViews(me: BadgeMe, plans: BadgeLive['locations']): BadgeNavItem[] {
  const out: BadgeNavItem[] = []
  const remote = remoteGrants(me)

  // First, because "which door am I at" is the question a plan answers and a list does not.
  if (plans.length) {
    out.push({
      key: 'plan',
      label: 'Plan',
      icon: '🗺️',
      count: plans.length > 1 ? plans.length : undefined,
    })
  }
  // "Portals", not "Doors": what the rest of the system calls them, and a portal is not
  // always a door — a holder whose badge opens the vehicle gate should not read it as one.
  if (remote.portals.length) {
    out.push({ key: 'portals', label: 'Portals', icon: '🚪', count: remote.portals.length })
  }
  if (remote.areas.length) {
    out.push({ key: 'areas', label: 'Areas', icon: '🛡️', count: remote.areas.length })
  }
  if (remote.outputs.length) {
    out.push({ key: 'controls', label: 'Controls', icon: '⚡', count: remote.outputs.length })
  }
  // Last, and beside the three kinds rather than under them: the split a holder cares
  // about is press-a-button versus walk-to, and past a few doors that list wants grouping
  // by building, which BadgeOnSiteList does and the action lists do not.
  const onSite = total(onSiteGrants(me))
  if (onSite) {
    out.push({ key: 'onsite', label: 'On site', icon: '📍', count: onSite })
  }

  if (!out.length) out.push({ key: 'empty', label: 'Access', icon: '🚪' })
  return out
}

/** The badge face, always first: it is the thing the badge IS. */
export const BADGE_FACE: BadgeNavItem = { key: 'badge', label: 'Badge', icon: '👤' }

import type { Portal, AuxInput, AuxOutput } from '@/types/pocketbase'

/**
 * Kinds of record that can be pinned to a floor plan. Areas are excluded on
 * purpose — they have no single physical position (they span controllers), so
 * on the map they ride the context-bar chip/drawer instead of a marker.
 */
export type PlaceKind = 'portal' | 'aux_input' | 'aux_output'

/**
 * A floor-plan marker, normalized across portals + aux I/O so one Leaflet layer
 * can render them all. `id` is namespaced (`${kind}:${recordId}`) because it is
 * the marker's map-wide key and PocketBase ids are only unique *within* a
 * collection — a portal and an aux input could otherwise collide. `recordId` is
 * the bare id for routing saves/links back to the right collection.
 */
export interface Placeable {
  id: string
  recordId: string
  kind: PlaceKind
  code: string
  name: string
  floorplan_position?: { x: number; y: number } | null
}

/** Emoji + human label per kind — matches the sidebar/nav icons. */
export const PLACE_KIND_META: Record<PlaceKind, { emoji: string; label: string; plural: string }> = {
  portal: { emoji: '🚪', label: 'Portal', plural: 'Portals' },
  aux_input: { emoji: '🔌', label: 'Input', plural: 'Aux inputs' },
  aux_output: { emoji: '🔆', label: 'Output', plural: 'Aux outputs' },
}

/** The PocketBase collection name backing each kind (for updates/queries). */
export const PLACE_KIND_COLLECTION: Record<PlaceKind, string> = {
  portal: 'portals',
  aux_input: 'aux_input',
  aux_output: 'aux_output',
}

/** The detail-route base for each kind. */
export const PLACE_KIND_ROUTE: Record<PlaceKind, string> = {
  portal: '/portals',
  aux_input: '/aux-inputs',
  aux_output: '/aux-outputs',
}

/** Live "device shadow" status key prefix per kind (see point_status.key). */
export const PLACE_KIND_STATUS_PREFIX: Record<PlaceKind, string> = {
  portal: 'portal',
  aux_input: 'auxin',
  aux_output: 'auxout',
}

type Positionable = { id: string; code: string; name?: string; floorplan_position?: { x: number; y: number } | null }

export function toPlaceable(kind: PlaceKind, r: Positionable): Placeable {
  return {
    id: `${kind}:${r.id}`,
    recordId: r.id,
    kind,
    code: r.code,
    name: r.name || '',
    floorplan_position: r.floorplan_position ?? null,
  }
}

export const portalPlaceable = (p: Portal) => toPlaceable('portal', p)
export const auxInputPlaceable = (a: AuxInput) => toPlaceable('aux_input', a)
export const auxOutputPlaceable = (a: AuxOutput) => toPlaceable('aux_output', a)

/** Split a namespaced marker id back into its kind + bare record id. */
export function parseMarkerId(markerId: string): { kind: PlaceKind; recordId: string } {
  const i = markerId.indexOf(':')
  return { kind: markerId.slice(0, i) as PlaceKind, recordId: markerId.slice(i + 1) }
}

/** A record is "placed" when it has valid {x, y} floor-plan coordinates. */
export function isPlaced(p: { floorplan_position?: { x: number; y: number } | null }): boolean {
  const pos = p.floorplan_position
  return !!pos && typeof pos.x === 'number' && typeof pos.y === 'number'
}

/** The live status key for a placeable, e.g. "auxout.gate-relay". */
export function statusKeyFor(kind: PlaceKind, code: string): string {
  return `${PLACE_KIND_STATUS_PREFIX[kind]}.${code}`
}

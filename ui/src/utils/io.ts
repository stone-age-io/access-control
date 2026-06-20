import type { Portal, AuxInput, AuxOutput } from '@/types/pocketbase'

/** One consumer of a relay or input line on a controller. */
export interface Occupant {
  /** The PocketBase record id of the owning portal / aux point. */
  recordId: string
  /** Human label, e.g. "lobby-main · DPS" or "gate-strike · aux out". */
  label: string
  /** Detail-page route for the owning record. */
  to: string
}

/** A controller's relay and input occupancy, keyed by 1-based logical index. An
 *  index with more than one occupant is a conflict. Index 0 ("not wired") is never
 *  a key. */
export interface ControllerIO {
  relays: Map<number, Occupant[]>
  inputs: Map<number, Occupant[]>
}

function add(map: Map<number, Occupant[]>, index: number, occ: Occupant) {
  if (!index) return // 0 = unwired / none
  const arr = map.get(index)
  if (arr) arr.push(occ)
  else map.set(index, [occ])
}

/** Build the relay/input occupancy maps for a controller from the records bound to
 *  it (its portals' lock/DPS/REX plus its aux inputs and outputs). Relays and inputs
 *  are separate namespaces. */
export function buildControllerIO(
  portals: Portal[],
  auxInputs: AuxInput[],
  auxOutputs: AuxOutput[],
): ControllerIO {
  const relays = new Map<number, Occupant[]>()
  const inputs = new Map<number, Occupant[]>()
  for (const p of portals) {
    const to = `/portals/${p.id}`
    add(relays, p.lock_relay, { recordId: p.id, label: `${p.code} · lock`, to })
    add(inputs, p.dps_input, { recordId: p.id, label: `${p.code} · DPS`, to })
    add(inputs, p.rex_input, { recordId: p.id, label: `${p.code} · REX`, to })
  }
  for (const a of auxOutputs) {
    add(relays, a.relay_index, { recordId: a.id, label: `${a.code} · aux out`, to: `/aux-outputs/${a.id}` })
  }
  for (const a of auxInputs) {
    add(inputs, a.input_index, { recordId: a.id, label: `${a.code} · aux in`, to: `/aux-inputs/${a.id}` })
  }
  return { relays, inputs }
}

/** Occupants of `index` other than the record identified by `selfId` — i.e. the
 *  records that would collide if `selfId` also claimed `index`. Empty for index 0. */
export function conflictsAt(usage: Map<number, Occupant[]>, index: number, selfId?: string): Occupant[] {
  if (!index) return []
  return (usage.get(index) || []).filter((o) => o.recordId !== selfId)
}

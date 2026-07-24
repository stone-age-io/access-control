import type { Capability } from '@/types/pocketbase'

/**
 * Capability metadata + named presets for the operator-management UI.
 *
 * Capabilities are the enforcement primitive (mirrored to PocketBase
 * `users.permissions` and the collection API rules in migration 1750000016).
 * Presets are a UI convenience only — they tick capability boxes; nothing about a
 * preset is stored. This keeps `permissions` the single source of truth and
 * avoids a role-name that can drift out of sync with the actual grants.
 */

export interface CapabilityMeta {
  value: Capability
  label: string
  hint: string
}

/** Display order for the capability checklist. */
export const CAPABILITIES: CapabilityMeta[] = [
  { value: 'enroll', label: 'Enroll people', hint: 'Create & edit cardholders and credentials.' },
  { value: 'policy', label: 'Access policy', hint: 'Edit roles, access groups, schedules, and holidays.' },
  { value: 'topology', label: 'Hardware / topology', hint: 'Edit locations, controllers, portals, and aux I/O.' },
  { value: 'command', label: 'Commands', hint: 'Grant, set posture, drive aux outputs, and arm areas.' },
  { value: 'operators', label: 'Manage operators', hint: 'Manage operator accounts, read the audit log, hard-delete records.' },
]

export interface Preset {
  name: string
  caps: Capability[]
}

/** Named bundles shown as quick-apply buttons. Order matters for matching. */
export const PRESETS: Preset[] = [
  { name: 'Read-only', caps: [] },
  { name: 'Enrollment', caps: ['enroll'] },
  { name: 'Command Ops', caps: ['command', 'policy'] },
  { name: 'Facilities', caps: ['topology'] },
  { name: 'Admin', caps: ['enroll', 'policy', 'topology', 'command', 'operators'] },
]

function sameSet(a: readonly string[], b: readonly string[]): boolean {
  if (a.length !== b.length) return false
  const set = new Set(a)
  return b.every((x) => set.has(x))
}

/**
 * Human label for a permission set: the matching preset name, else "Custom".
 * (An empty set matches the Read-only preset.)
 */
export function presetLabel(perms: readonly string[]): string {
  const match = PRESETS.find((p) => sameSet(p.caps, perms))
  return match ? match.name : 'Custom'
}

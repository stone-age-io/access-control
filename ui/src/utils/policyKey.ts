// Live preview of the NATS KV key a record occupies in the ACC_POLICY bucket.
// Mirrors the key scheme in internal/policykv/wire.go — "<prefix><natural-key>"
// — so the UI shows operators exactly what the mirror will write. The natural
// key is the stable `code` for most collections, the PocketBase record id for
// cardholders and holidays, and the raw value for credentials.

const PREFIX: Record<string, string> = {
  locations: 'location.',
  schedules: 'sched.',
  controllers: 'controller.',
  portals: 'portal.',
  access_groups: 'group.',
  roles: 'role.',
  cardholders: 'user.',
  credentials: 'cred.',
  holidays: 'holiday.',
  aux_input: 'auxin.',
  aux_output: 'auxout.',
}

/** Build the KV key for a record, or '' when the natural key isn't set yet. */
export function policyKey(collection: string, rec: Record<string, any>): string {
  const prefix = PREFIX[collection]
  if (!prefix) return ''
  let natural = ''
  if (collection === 'cardholders' || collection === 'holidays') natural = rec.id || ''
  else if (collection === 'credentials') natural = rec.value || ''
  else natural = rec.code || ''
  if (!natural) return ''
  return prefix + natural
}

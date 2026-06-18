// Live preview of the NATS KV key a record occupies in the ACC_POLICY bucket.
// Mirrors the key scheme in internal/policykv/wire.go — "<prefix><natural-key>"
// — so the UI shows operators exactly what the mirror will write. The natural
// key is the stable `code` for most collections, the cardholder's PocketBase id
// for cardholders, and the raw value for credentials.

const PREFIX: Record<string, string> = {
  locations: 'location.',
  schedules: 'sched.',
  portals: 'portal.',
  access_groups: 'group.',
  roles: 'role.',
  cardholders: 'user.',
  credentials: 'cred.',
}

/** Build the KV key for a record, or '' when the natural key isn't set yet. */
export function policyKey(collection: string, rec: Record<string, any>): string {
  const prefix = PREFIX[collection]
  if (!prefix) return ''
  let natural = ''
  if (collection === 'cardholders') natural = rec.id || ''
  else if (collection === 'credentials') natural = rec.value || ''
  else natural = rec.code || ''
  if (!natural) return ''
  return prefix + natural
}

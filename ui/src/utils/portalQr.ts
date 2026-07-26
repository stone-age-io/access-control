/**
 * Door placard QR payload format, shared by the page that PRINTS placards and the
 * page that SCANS them so the two cannot drift.
 *
 * The payload is `portal:<code>` — a door's identity, deliberately not a credential
 * and not a token. That is what makes a placard safe to mount in public: scanning it
 * proves nothing and grants nothing. Authorization for the resulting unlock comes
 * entirely from the operator's session and their `command` capability, checked
 * server-side by POST /api/portals/{id}/grant.
 *
 * Contrast with a badge QR, which for a visitor IS the credential — see
 * internal/badgeapi's qrPayload.
 */
export const PORTAL_QR_PREFIX = 'portal:'

/**
 * Extracts a portal code from a scanned placard payload, or null when the payload is
 * not one of ours. Tolerant of surrounding whitespace (scanners sometimes append a
 * newline) but otherwise strict — an unrecognized code must not be guessed at.
 */
export function parsePortalQr(raw: string): string | null {
  const value = (raw || '').trim()
  if (!value.startsWith(PORTAL_QR_PREFIX)) return null
  const code = value.slice(PORTAL_QR_PREFIX.length).trim()
  return code || null
}

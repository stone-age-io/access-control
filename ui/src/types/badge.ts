/**
 * Badge-tier API shapes (internal/badgeapi). Separate from types/pocketbase.ts
 * because these are NOT collection records — they are purpose-built responses that
 * deliberately omit everything a badge holder must not see (portal codes, relay
 * indices, area membership, other people).
 */

/** What a badge's QR code encodes. */
export type BadgeQrKind =
  /** An opaque cardholder id. Opens nothing; safe to display indefinitely. */
  | 'identifier'
  /** The credential value itself. A working key — short-lived visitor passes only. */
  | 'credential'

export type BadgeKind = 'holder' | 'visitor'

/** One door on this badge. */
export interface BadgePortal {
  /** PocketBase portal record id — what POST /api/badge/unlock/{id} takes. */
  id: string
  /** Display name (never the policy code). */
  name: string
  /** Human-readable location name (never the location code). */
  location: string
  /**
   * Whether this door opted into remote unlock (`portals.allow_remote_unlock`).
   * False means the badge works there in person but not from a phone.
   */
  remoteUnlock: boolean
}

/** GET /api/badge/me */
export interface BadgeMe {
  name: string
  email: string
  kind: BadgeKind | ''
  /**
   * Cardholder record id, for building the protected-file URL client-side. The API
   * deliberately sends no ready-made URL: the photo is a protected file, so a bare
   * URL would 403 — build it with `badgePb.files.getURL()` + `getToken()`.
   */
  photoRecord: string
  /** Photo filename, or '' when none. */
  photoFile: string
  /** Payload to encode, or '' when there is nothing valid to show. */
  qr: string
  qrKind: BadgeQrKind
  /** True when `qr` is a working credential, so the UI can warn the holder. */
  qrSecret: boolean
  /** RFC3339, or '' when unbounded. */
  validFrom: string
  validUntil: string
  /** True when no active, in-window credential exists right now. */
  expired: boolean
  portals: BadgePortal[]
}

/** POST /api/badge/visitors (operator-only) */
export interface MintVisitorRequest {
  name: string
  email: string
  /** roles record id; must have visitor_preset set. */
  role: string
  /** RFC3339; empty = now. */
  validFrom?: string
  /** RFC3339; required — a visitor pass must expire. */
  validUntil: string
  label?: string
}

export interface MintVisitorResponse {
  cardholderId: string
  badgeUserId: string
  credentialId: string
  email: string
  validFrom: string
  validUntil: string
  /** False when SMTP is unconfigured or the send failed; minting still succeeded. */
  inviteSent: boolean
  badgeUrl: string
  /** True when an existing visitor was refreshed rather than a duplicate created. */
  reused: boolean
}

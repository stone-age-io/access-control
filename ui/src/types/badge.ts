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

/**
 * Whether this badge is usable right now, and if not, why. Decided server-side
 * (badgeapi's evaluatePass) and rendered as one branch — never re-derived here. It
 * replaced an `expired` boolean that reported "no credential has ever been issued" as
 * "your pass is not currently valid".
 */
export type BadgePassState =
  /** A credential is active and in-window. */
  | 'valid'
  /** A credential exists but its window has passed. */
  | 'expired'
  /** A credential exists but its window has not opened yet. */
  | 'not_yet_valid'
  /** No usable credential is issued at all — nothing has expired; nothing exists. */
  | 'none'
  /** The cardholder is suspended, so the badge is withdrawn regardless of credentials. */
  | 'suspended'

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
  /**
   * The window of the credential `passState` describes — the live one when valid, the
   * soonest to open when not yet valid, the last to close when expired. RFC3339, or ''
   * when there is nothing to describe or the bound is unbounded.
   */
  validFrom: string
  validUntil: string
  /** Whether the badge works right now, and if not, why. */
  passState: BadgePassState
  portals: BadgePortal[]
}

/**
 * The badge-tier fields on a cardholder record.
 *
 * There is no separate login record any more: a cardholder who may sign in is one with
 * `badge_login` set. So these are fields on the same row the Cardholders pages already
 * read and write, which is why issuing a badge login needs no bespoke route — it is an
 * ordinary PATCH the collection rules already govern.
 */
export interface CardholderBadgeFields {
  /** May this person sign in to see their badge? The gate behind the AuthRule. */
  badge_login?: boolean
  /** Which badge shape. Blank means an ordinary cardholder (see 1750000000). */
  kind?: BadgeKind | ''
  /** True when the holder knows their password; false = OTP/OAuth only. */
  password_set?: boolean
  /** PocketBase sets this itself on a first successful OTP sign-in. */
  verified?: boolean
}

/** POST /api/badge/invite/{cardholderId} (operator + `enroll`) */
export interface BadgeInviteResponse {
  /** False when SMTP is unconfigured or the send failed; the login still works. */
  sent: boolean
  email: string
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

/** POST /api/badge/visitors/{id}/revoke (operator-only) */
export interface RevokeVisitorResponse {
  cardholderId: string
}

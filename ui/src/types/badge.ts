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

/**
 * An area's arm-state as the badge reports it.
 *
 * This is the POLICY INTENT resolved from the mirrored graph (override → scheduled →
 * standing), not a report from the hardware — the operator console's per-controller arm
 * shadow is the authoritative live view, and it can say "a box never reported", which a
 * badge holder has no use for. `unknown` is a real answer: an area whose schedule has
 * not loaded is one whose state must not be guessed at.
 */
export type BadgeAreaState = 'armed' | 'disarmed' | 'unknown'

/** One area on this badge. */
export interface BadgeArea {
  /** PocketBase area record id — what POST /api/badge/areas/{id}/arm takes. */
  id: string
  name: string
  location: string
  /**
   * Arm and disarm are separate rights (`access_groups.area_rights`). Both false never
   * appears here: the server omits an area the badge holds no right over.
   */
  canArm: boolean
  canDisarm: boolean
  /**
   * Whether this area opted into remote arming (`areas.allow_remote_arm`). False means
   * the grant is real but usable only at a keypad — shown rather than hidden, so the
   * holder is not misinformed about what their badge does.
   */
  remote: boolean
  state: BadgeAreaState
}

/** One aux output on this badge. */
export interface BadgeOutput {
  /** PocketBase aux_output record id — what POST /api/badge/outputs/{id}/pulse takes. */
  id: string
  name: string
  location: string
  /** Whether this relay opted into remote driving (`aux_output.allow_remote`). */
  remote: boolean
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
  /** Empty for the overwhelming majority of badges, which grant doors only. */
  areas: BadgeArea[]
  outputs: BadgeOutput[]
}

/** The {ok, reason} every badge ACTION answers with — unlock, arm, disarm, pulse. */
export interface BadgeActionResponse {
  ok: boolean
  /** Stable policy reason code (internal/policy). */
  reason: string
}

/**
 * One pinnable thing on a floor plan. `x`/`y` are pixel coordinates in the IMAGE's own
 * space (what the operator's placement editor writes); the client converts them to
 * percentages once the image reports its natural size, so the server needs to know
 * nothing about the picture.
 */
export interface BadgeLivePoint {
  id: string
  name: string
  x: number
  y: number
  /** The per-record remote opt-in. False = identifiable on the plan, usable in person. */
  remote: boolean
}

/** One site's floor plan with this badge's own things on it. */
export interface BadgeLiveLocation {
  id: string
  name: string
  /** Ready-made URL for the plan image (a public file field — no file token needed). */
  floorplan: string
  portals: BadgeLivePoint[]
  outputs: BadgeLivePoint[]
}

/**
 * GET /api/badge/live
 *
 * Empty `locations` is the normal case and not an error: a site appears only when an
 * operator opted it in (`locations.badge_floorplan`), has uploaded a plan, AND this badge
 * has something placed on it. Areas never appear — they have no single position.
 */
export interface BadgeLive {
  locations: BadgeLiveLocation[]
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

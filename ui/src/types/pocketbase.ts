// TypeScript interfaces for the stone-access PocketBase collections.
// Mirrors pbmigrations/1750000000_collections.go. Relations are stored as the
// related record's PocketBase id; `expand` holds the resolved record(s) when a
// query requests them.

export interface BaseRecord {
  id: string
  created: string
  updated: string
  collectionId?: string
  collectionName?: string
  expand?: Record<string, any>
}

export type Posture = 'secure' | 'unlocked' | 'lockdown' | 'disabled'
export type CardholderStatus = 'active' | 'suspended'
export type CredentialStatus = 'active' | 'revoked' | 'suspended'
export type CredentialType = 'nkey' | 'wiegand' | 'pin' | 'mobile'
export type EventKind = 'tap' | 'state' | 'alarm' | 'fire' | 'command'

/** A building/campus; owns the timezone. */
export interface Site extends BaseRecord {
  code: string
  name: string
  /** IANA timezone name, e.g. "America/New_York". */
  timezone: string
  /** Suppress forced/held-open alarms while the site fire input is active. */
  fai_suppress: boolean
}

/** One weekly time window. days are ISO weekdays (1=Mon..7=Sun). */
export interface ScheduleWindow {
  days: number[]
  /** "HH:MM" 24h. */
  start: string
  /** "HH:MM" 24h. end <= start crosses midnight. */
  end: string
}

/** Reusable weekly time windows. */
export interface Schedule extends BaseRecord {
  code: string
  name: string
  windows: ScheduleWindow[]
}

/** A controllable opening (door/gate/turnstile/elevator). */
export interface AccessPoint extends BaseRecord {
  code: string
  site: string
  name: string
  posture: Posture | ''
  pulse_seconds: number
  expand?: { site?: Site }
}

/** A set of points under one schedule (an "access level"). */
export interface AccessGroup extends BaseRecord {
  code: string
  name: string
  access_points: string[]
  schedule: string
  expand?: { access_points?: AccessPoint[]; schedule?: Schedule }
}

/** A named bundle of access groups assigned to cardholders. */
export interface Role extends BaseRecord {
  code: string
  name: string
  access_groups: string[]
  expand?: { access_groups?: AccessGroup[] }
}

/** A person who holds credentials (not a PocketBase login). */
export interface Cardholder extends BaseRecord {
  external_id: string
  name: string
  email: string
  status: CardholderStatus | ''
  roles: string[]
  expand?: { roles?: Role[] }
}

/** An opaque string presented at a reader, mapped to one cardholder. */
export interface Credential extends BaseRecord {
  value: string
  type: CredentialType | ''
  user: string
  status: CredentialStatus | ''
  label: string
  expand?: { user?: Cardholder }
}

/** Queryable projection of the JetStream audit stream. */
export interface AccessEvent extends BaseRecord {
  site: string
  access_point: string
  kind: EventKind | ''
  credential: string
  user: string
  allow: boolean
  reason: string
  payload: Record<string, any>
  ts: string
}

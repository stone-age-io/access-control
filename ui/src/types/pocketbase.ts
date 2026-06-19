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
export type PortalType = 'door' | 'turnstile' | 'elevator' | 'gate' | 'logical'
export type EventKind = 'tap' | 'state' | 'alarm' | 'fire' | 'command'
export type ControllerModel = 'kincony-server-mini' | 'kincony-pi5r8'
export type ControllerStatus = 'online' | 'offline'

/** A building/campus; owns the timezone. */
export interface Location extends BaseRecord {
  code: string
  name: string
  /** IANA timezone name, e.g. "America/New_York". */
  timezone: string
  /** Suppress forced/held-open alarms while the location fire input is active. */
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

/** An edge box (e.g. a KinCony Server-Mini) that drives the portals assigned to it. */
export interface Controller extends BaseRecord {
  code: string
  name: string
  location: string
  model: ControllerModel | ''
  /** Liveness, written by accessd from heartbeats (M4); not mirrored to KV. */
  last_seen: string
  status: ControllerStatus | ''
  expand?: { location?: Location }
}

/** A controllable opening (door/gate/turnstile/elevator/logical). */
export interface Portal extends BaseRecord {
  code: string
  type: PortalType | ''
  location: string
  name: string
  posture: Posture | ''
  pulse_seconds: number
  /** The edge box that drives this portal (empty if unassigned). */
  controller: string
  /** Logical hardware indices on that box; the model template maps them to lines. */
  lock_relay: number
  dps_input: number
  rex_input: number
  /** Door-open-too-long threshold (seconds). */
  held_open_seconds: number
  expand?: { location?: Location; controller?: Controller }
}

/** A set of portals under one schedule (an "access level"). */
export interface AccessGroup extends BaseRecord {
  code: string
  name: string
  portals: string[]
  schedule: string
  expand?: { portals?: Portal[]; schedule?: Schedule }
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
  location: string
  portal: string
  type: string
  kind: EventKind | ''
  credential: string
  user: string
  allow: boolean
  reason: string
  payload: Record<string, any>
  ts: string
}

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

export type Posture = 'secure' | 'free_access' | 'unlocked' | 'lockdown' | 'disabled'
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
  /** When true, holidays do NOT close this schedule's windows (stored inverted; UI shows "Observe holidays" = !ignore_holidays). */
  ignore_holidays: boolean
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
  /** OSDP PD address of this portal's reader on the controller's RS485 bus (used when reader=="osdp"). */
  reader_address: number
  /** While auto_schedule's window is open, the controller adopts this posture instead of the standing one ('' = no automation). */
  auto_posture: Posture | ''
  /** Schedules relation id that gates auto_posture ('' = no automation). Both-or-neither with auto_posture. */
  auto_schedule: string
  expand?: { location?: Location; controller?: Controller; auto_schedule?: Schedule }
}

/** A named auxiliary digital input bound to a controller (observe-only). */
export interface AuxInput extends BaseRecord {
  code: string
  name: string
  location: string
  controller: string
  /** Logical input index on the box; the model template maps it to a line. */
  input_index: number
  expand?: { location?: Location; controller?: Controller }
}

/** A named auxiliary output relay driven by the cmd.output command. */
export interface AuxOutput extends BaseRecord {
  code: string
  name: string
  location: string
  controller: string
  /** Logical relay index on the box. */
  relay_index: number
  /** Default momentary-pulse duration (seconds) for the "pulse" action. */
  pulse_seconds: number
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
  /** ISO datetime; presentations before this deny. Empty = no lower bound. */
  valid_from: string
  /** ISO datetime; presentations after this deny. Empty = no upper bound. */
  valid_until: string
  expand?: { user?: Cardholder }
}

/** A date a location is closed; closes every window of any holiday-observing schedule that day. */
export interface Holiday extends BaseRecord {
  location: string
  /** Local calendar day (only the date part is used). */
  date: string
  name: string
  /** Match this month/day every year (for fixed-date holidays like Dec 25). */
  recurring: boolean
  expand?: { location?: Location }
}

export type DoorState = 'open' | 'closed' | 'unknown'
export type PointKind = 'portal' | 'aux_input' | 'aux_output'

/**
 * Live "device shadow" projection of the ACC_STATUS KV bucket — the current state
 * of each point the edge drives. Rebuildable read model, written by accessd's
 * status projector; subscribe via PocketBase realtime for live updates.
 */
export interface PointStatus extends BaseRecord {
  /** ACC_STATUS KV key, e.g. "portal.lobby-main" — the row identity. */
  key: string
  /** Bare point code (display). */
  code: string
  kind: PointKind | ''
  /** Headline value: door open/closed/unknown for portals; aux state otherwise. */
  state: string
  /** Effective posture (portals only). */
  posture: Posture | ''
  /** Held-open alarm active (portals only). */
  held: boolean
  controller: string
  location: string
  /** Controller-stamped instant of the last change (ISO datetime). */
  changed: string
  payload: Record<string, any>
}

export type OperatorRole = 'admin' | 'operator' | 'viewer'

/** A management-UI operator (the built-in `users` auth collection + role). */
export interface User extends BaseRecord {
  email: string
  name: string
  verified: boolean
  /** admin = full control; operator = daily ops; viewer = read-only. */
  role: OperatorRole | ''
}

export type AuditEventType = 'create' | 'update' | 'delete' | 'auth'

/** Control-plane change log (who edited which policy record), written by internal/changelog. */
export interface AuditLog extends BaseRecord {
  event_type: AuditEventType | ''
  collection_name: string
  record_id: string
  actor_email: string
  actor_id: string
  actor_collection: string
  request_ip: string
  request_method: string
  request_url: string
  timestamp: string
  before: Record<string, any> | null
  after: Record<string, any> | null
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

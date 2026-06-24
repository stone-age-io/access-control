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

/**
 * Why a portal's effective posture is what it is, as resolved by the controller:
 * its configured `standing` posture, a `scheduled` auto_posture (its window is
 * open), or an operator's manual `override`. Empty when unknown (older shadow).
 */
export type PostureSource = 'standing' | 'scheduled' | 'override'
export type CardholderStatus = 'active' | 'suspended'
export type CredentialStatus = 'active' | 'revoked' | 'suspended'
export type CredentialType = 'generic' | 'wiegand' | 'pin' | 'mobile'
export type PortalType = 'door' | 'turnstile' | 'elevator' | 'gate' | 'logical'
export type EventKind = 'tap' | 'state' | 'alarm' | 'fire' | 'command'
/** Arm-state of an area (intrusion-lite). Empty standing ⇒ disarmed. */
export type AreaArm = 'armed' | 'disarmed'
/** How an aux input behaves: observe-only, intrusion (armed-gated), or always-on tamper. */
export type PointType = 'monitor' | 'intrusion' | 'tamper_24h'
export type EventSource = 'nats' | 'osdp'
export type ControllerModel = 'kincony-server-mini' | 'kincony-pi5r8'
export type ControllerStatus = 'online' | 'offline'

/** A PocketBase geoPoint value. Note longitude is `lon`, not `lng`. */
export interface GeoPoint {
  lat: number
  lon: number
}

/** A building/campus; owns the timezone. */
export interface Location extends BaseRecord {
  code: string
  name: string
  /** IANA timezone name, e.g. "America/New_York". */
  timezone: string
  /** Suppress forced/held-open alarms while the location fire input is active. */
  fai_suppress: boolean
  /** Free-form description (UI only; not mirrored to KV). */
  description: string
  /** Geographic position for the location map (UI only; {0,0} = unset). */
  coordinates: GeoPoint
  /** Uploaded floor-plan image filename (UI only; '' = none). */
  floorplan: string
  /** Holiday calendar ids this site observes (M:N). Their dates union into the location's holiday set. */
  holiday_calendars: string[]
  expand?: { holiday_calendars?: HolidayCalendar[] }
}

/** A named, shareable set of holiday dates. A location observes one or more; many sites can share one. */
export interface HolidayCalendar extends BaseRecord {
  code: string
  name: string
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
  /** Door-position contact sense: '' / 'nc' (default, closed when shut) / 'no'. Controller-only wiring hint. */
  dps_contact: 'nc' | 'no' | ''
  /** Request-to-exit contact sense: '' / 'no' (default, closed when pressed) / 'nc'. */
  rex_contact: 'nc' | 'no' | ''
  /** Lock type: '' / 'strike' (default, fail-secure energize-to-unlock) / 'maglock' (fail-safe energize-to-lock). */
  lock_type: 'strike' | 'maglock' | ''
  /** When true, a REX press also pulses the strike (electric egress), not just shunts the forced alarm. */
  rex_unlock: boolean
  /** While auto_schedule's window is open, the controller adopts this posture instead of the standing one ('' = no automation). */
  auto_posture: Posture | ''
  /** Schedules relation id that gates auto_posture ('' = no automation). Both-or-neither with auto_posture. */
  auto_schedule: string
  /** Area relation id this portal belongs to ('' = none). While that area is armed, a forced open escalates to an area intrusion alarm. */
  area: string
  /** When true, a valid credential grant at this portal durably disarms its area (an entry door). */
  disarm_on_grant: boolean
  /** {x, y} pixel position on the location's floorplan (UI only; null/absent = not placed). */
  floorplan_position?: { x: number; y: number } | null
  expand?: { location?: Location; controller?: Controller; auto_schedule?: Schedule; area?: Area }
}

/** A named auxiliary digital input bound to a controller. */
export interface AuxInput extends BaseRecord {
  code: string
  name: string
  location: string
  controller: string
  /** Logical input index on the box; the model template maps it to a line. */
  input_index: number
  /** Contact sense: '' / 'no' (default, closed when asserted) / 'nc'. Controller-only wiring hint. */
  contact: 'nc' | 'no' | ''
  /** Arming membership: the area this input belongs to ('' = none). */
  area: string
  /** Point type: '' / 'monitor' (default, observe-only) / 'intrusion' (alarms while its area is armed) / 'tamper_24h' (alarms always). */
  point_type: PointType | ''
  expand?: { location?: Location; controller?: Controller; area?: Area }
}

/**
 * An arm-state grouping for intrusion-lite. Logical, single-location, possibly
 * spanning several controllers. Members are aux inputs (membership lives on the
 * input). Arm-state resolves arm_override → auto_arm (while auto_schedule open) →
 * standing arm; an unresolved/unknown area is disarmed (never spuriously arms).
 */
export interface Area extends BaseRecord {
  code: string
  name: string
  location: string
  /** Standing floor; empty ⇒ disarmed. */
  arm: AreaArm | ''
  /** Durable operator override ('' = none); set by the arm/disarm command. */
  arm_override: AreaArm | ''
  /** Scheduled arm-state while auto_schedule's window is open ('' = no automation). */
  auto_arm: AreaArm | ''
  /** Schedule id gating auto_arm ('' = none). Both-or-neither with auto_arm. */
  auto_schedule: string
  expand?: { location?: Location; auto_schedule?: Schedule }
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

/** A date on a holiday calendar; closes every window of any holiday-observing schedule that day. */
export interface Holiday extends BaseRecord {
  /** The owning calendar id (a location observes a set of calendars). */
  calendar: string
  /** Local calendar day (only the date part is used). */
  date: string
  name: string
  /** Match this month/day every year (for fixed-date holidays like Dec 25). */
  recurring: boolean
  expand?: { calendar?: HolidayCalendar }
}

export type DoorState = 'open' | 'closed' | 'unknown'
export type PointKind = 'portal' | 'aux_input' | 'aux_output' | 'area'

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
  /** Provenance of `posture` — standing config, scheduled, or a manual override. */
  posture_source: PostureSource | ''
  /** Held-open alarm active (portals only). */
  held: boolean
  controller: string
  location: string
  /** Controller-stamped instant of the last change (ISO datetime). */
  changed: string
  payload: Record<string, any>
}

/**
 * Operator capabilities — an orthogonal set, not a rank. Reads need none (any
 * authenticated operator reads everything); these gate writes and commands.
 * Stored as a PocketBase multi-select on `users.permissions`.
 */
export type Capability = 'enroll' | 'policy' | 'topology' | 'command' | 'operators'

/** A management-UI operator (the built-in `users` auth collection). */
export interface User extends BaseRecord {
  email: string
  name: string
  verified: boolean
  /** The operator's capability set (empty = read-only viewer). */
  permissions: Capability[]
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
  /** Reader transport that produced a tap; empty for non-tap and legacy rows. */
  source: EventSource | ''
  payload: Record<string, any>
  ts: string
  /** Operator acknowledgement of an alarm/fire (set via POST /api/events/{id}/ack). */
  acknowledged: boolean
  ack_by: string
  ack_at: string
}

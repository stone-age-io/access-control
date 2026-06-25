/**
 * Route-keyed contextual help.
 *
 * Help is authored once per resource ("topic") and surfaced on its list, detail,
 * and form pages alike. The topic is derived from the first path segment, so no
 * per-view wiring is needed — `HelpButton` (desktop) and the AppHeader help icon
 * (mobile) look up the current route here and self-hide when there is no entry.
 */
export interface HelpItem {
  term: string
  def: string
}

export interface HelpSection {
  heading?: string
  body?: string
  items?: HelpItem[]
}

export interface HelpTopic {
  title: string
  icon: string
  sections: HelpSection[]
}

const POSTURES: HelpItem[] = [
  { term: 'Secure', def: 'Default. Every tap is validated against the policy graph.' },
  { term: 'Free access', def: 'Any tap opens the door (strike pulses); the credential is not validated, but every entry is still logged.' },
  { term: 'Unlocked', def: 'Strike is held open for free passage — no tap needed.' },
  { term: 'Lockdown', def: 'Deny all — no credential opens the door.' },
  { term: 'Disabled', def: 'Deny all — the portal is taken out of service.' },
]

/** One entry per resource. Keyed by the first path segment (see helpForPath). */
const TOPICS: Record<string, HelpTopic> = {
  overview: {
    title: 'Overview',
    icon: '📊',
    sections: [
      { body: 'A snapshot of the policy graph — counts per collection — and the most recent access activity streamed in from controllers.' },
      { body: 'Click any stat to jump to that collection. Recent events are a projection of the ACC_EVENTS audit stream; they appear once controllers start publishing.' },
    ],
  },
  monitor: {
    title: 'Live Map',
    icon: '🗺️',
    sections: [
      { body: 'A live operational view: a geographic overview of your locations that drills into a per-location floor plan. Portals render with their current state — open/closed, posture, held-open — streamed up from the edge.' },
      { heading: 'Commands', body: 'Click a portal on the floor plan to send operator commands (grant, posture override) without leaving the map. Commands are fire-and-forget; the live status reflects the result and is never written back to the record.' },
      { heading: 'Coverage', body: 'A portal shows live state only while its controller is reporting. “Unknown” means the controller is offline or unassigned, or no door sensor is wired.' },
    ],
  },
  locations: {
    title: 'Locations',
    icon: '🏢',
    sections: [
      { body: 'A location is a site or building. It carries the timezone used to evaluate schedule windows and scopes a controller’s command and fire-alarm subscriptions.' },
      { heading: 'Why it matters', body: 'Schedule windows (and the holidays they observe) are evaluated in the location’s timezone, so set it correctly before relying on time-based access.' },
    ],
  },
  schedules: {
    title: 'Schedules',
    icon: '🗓️',
    sections: [
      { body: 'A schedule is a set of weekly time windows. Access groups reference one schedule to decide when their portals open; portals reference one for scheduled posture.' },
      { heading: 'Holidays', body: 'By default a schedule observes holidays — its windows are treated as closed on an observed holiday. Turn on “ignore holidays” to make a schedule run straight through them (e.g. 24/7 access).' },
    ],
  },
  'holiday-calendars': {
    title: 'Holiday Calendars',
    icon: '📆',
    sections: [
      { body: 'A holiday calendar is a named, shareable set of dates. Locations observe one or more calendars, so a single “Christmas” serves every site instead of being duplicated per location.' },
      { heading: 'Composing', body: 'A location can observe several calendars at once — e.g. a national calendar plus a site-specific shutdown calendar. The location’s closed days are the union of all the calendars it observes.' },
    ],
  },
  holidays: {
    title: 'Holidays',
    icon: '📅',
    sections: [
      { body: 'A holiday is a date on a holiday calendar. It closes holiday-observing schedules at every location that observes its calendar.' },
      { heading: 'Scope', body: 'A holiday affects a location only if that location observes the holiday’s calendar. A schedule with “ignore holidays” set is unaffected either way.' },
    ],
  },
  portals: {
    title: 'Portals',
    icon: '🚪',
    sections: [
      { body: 'A portal is a controllable opening — door, gate, turnstile, elevator, or logical. A controller drives it via the lock relay and the DPS/REX inputs.' },
      { heading: 'Postures', body: 'The standing posture is the default state. A scheduled posture (while its window is open) or a runtime command can override it on the controller.', items: POSTURES },
      { heading: 'Controller binding', body: 'A portal is bound to the controller whose code matches its Controller field — reassigning it takes effect without touching the edge box. Unassigned portals are armed by no box.' },
      { heading: 'Live status', body: 'The detail view shows the controller’s live state — door open/closed, effective posture, and whether the door is held open — streamed up from the edge. “Unknown” means no controller is reporting (offline or unassigned), or no door sensor is wired.' },
      {
        heading: 'Controls',
        body: 'Operator commands are sent to the controller (fire-and-forget); the live status reflects the result. They are operational state, never saved to the record.',
        items: [
          { term: 'Grant', def: 'Unlock once — a momentary strike pulse. The everyday “buzz someone in.”' },
          { term: 'Posture override', def: 'Hold a standing state (e.g. Unlocked for the morning, Lockdown during an incident). Distinct from Grant: it persists until changed or cleared.' },
          { term: 'Clear override', def: 'Drop the override and revert to the scheduled posture (if its window is open) or the standing posture.' },
        ],
      },
    ],
  },
  'aux-inputs': {
    title: 'Aux Inputs',
    icon: '🔌',
    sections: [
      { body: 'A named auxiliary digital input wired to a controller — like a portal’s DPS/REX but standalone and observe-only (no door logic). Use it to surface a contact, a tamper, or any dry-contact signal.' },
      { heading: 'Live status', body: 'The detail view shows the input’s current state (Active/Inactive), streamed up from the controller. “No live status” means the controller is offline or the input is unassigned.' },
      { heading: 'Binding', body: 'An aux input is monitored by the controller whose code matches its Controller field; the Input index is a logical line the controller’s model maps to a physical pin.' },
    ],
  },
  'aux-outputs': {
    title: 'Aux Outputs',
    icon: '🔆',
    sections: [
      { body: 'A named relay on a controller you can drive directly — a gate strike, a light, a siren. Not a portal: no credential logic, just an output.' },
      {
        heading: 'Controls',
        body: 'Commands are fire-and-forget; the live status reflects the standing state.',
        items: [
          { term: 'On / Off', def: 'Set the standing held state — the relay stays energized (On) or released (Off) until changed.' },
          { term: 'Pulse', def: 'Energize momentarily for the configured pulse seconds, then return to the standing state.' },
        ],
      },
      { heading: 'Binding', body: 'An aux output is driven by the controller whose code matches its Controller field; the Relay index is a logical line the model maps to a physical relay.' },
    ],
  },
  areas: {
    title: 'Areas',
    icon: '🛡️',
    sections: [
      { body: 'An area is a group of aux inputs and portals that arm together (intrusion-lite). While the area is armed, an intrusion input, a 24-hour tamper, or a forced-open on a member portal raises an intrusion alarm. A grant or REX open is normal passage and never trips.' },
      { heading: 'Arm state', body: 'The effective arm state resolves arm override → scheduled auto-arm → standing arm, failing safe to disarmed. Arming is a durable record write (not a fire-and-forget command), so a reboot can’t silently disarm an area. Each controller writes a per-controller arm shadow, so the console can tell “all armed” from “a box never reported.”' },
      { heading: 'Entry-disarm', body: 'A valid credential grant at a portal marked “disarm on grant” durably disarms that portal’s area — the inverse of arming, handled centrally because an area can span controllers.' },
    ],
  },
  controllers: {
    title: 'Controllers',
    icon: '⚙️',
    sections: [
      { body: 'A controller is an edge box that drives the portals whose Controller field points at its code. It decides taps locally and emits events; central config is just its identity and hardware selection.' },
      { heading: 'Health', body: 'Each box publishes a liveness heartbeat. Last-seen and online/offline status are updated directly from those heartbeats; a box that goes silent past the offline threshold is swept to offline.' },
    ],
  },
  'access-groups': {
    title: 'Access Groups',
    icon: '🗝️',
    sections: [
      { body: 'An access group grants a set of portals during one schedule’s windows. It’s the join between where (portals) and when (schedule).' },
      { heading: 'How access is granted', body: 'A credential opens a portal when one of its roles includes an access group that contains the portal and whose schedule window is open (and the day isn’t an observed holiday).' },
    ],
  },
  roles: {
    title: 'Roles',
    icon: '🏷️',
    sections: [
      { body: 'A role bundles access groups. Cardholders are assigned roles; the roles decide which access groups — and therefore which portals and schedules — apply to them.' },
      { body: 'The graph mirrors the operator’s mental model: user → roles → access groups → (portals + one schedule).' },
    ],
  },
  cardholders: {
    title: 'Cardholders',
    icon: '🪪',
    sections: [
      { body: 'A cardholder is a person in the system. Their roles determine what they can open; their credentials are the cards/PINs they present.' },
      { heading: 'Status', body: 'A disabled cardholder is denied everywhere, regardless of credentials or roles — the check happens before the grant walk.' },
    ],
  },
  credentials: {
    title: 'Credentials',
    icon: '🎫',
    sections: [
      { body: 'A credential is a card, fob, or PIN belonging to one cardholder. The value is what the reader presents at a tap.' },
      { heading: 'Validity', body: 'Optional valid-from / valid-until bounds gate the credential by date. Outside the window — or if the credential or its cardholder is disabled — every tap fails closed (deny).' },
    ],
  },
  import: {
    title: 'Import',
    icon: '📥',
    sections: [
      { body: 'Bulk-create cardholders and credentials from a CSV. Map the columns, preview the rows, then commit.' },
      { heading: 'Tip', body: 'Download the template to get the exact column headers. Rows that fail validation are reported without blocking the rest.' },
    ],
  },
  events: {
    title: 'Events',
    icon: '📋',
    sections: [
      { body: 'A read-only audit timeline projected from the ACC_EVENTS JetStream. Taps (allow/deny), door state, alarms (forced/held), and fire events all land here.' },
      { heading: 'Reason codes', body: 'Each tap carries a stable reason code (e.g. why it was denied). These are a fixed contract shared by the controller, the events stream, and this UI.' },
    ],
  },
  alarms: {
    title: 'Alarm Console',
    icon: '🚨',
    sections: [
      { body: 'Unacknowledged alarms across your locations, surfaced for operator response. Forced-open, held-open, intrusion, and fire events all land here.' },
      { heading: 'Acknowledging', body: 'Acknowledging an alarm stamps who and when onto the event. It records that an operator has seen it — it does not clear the underlying condition at the door, which must be resolved physically.' },
      { heading: 'Notifications', body: 'The same alarms can page operators by email when a source opts in (a portal/area’s “notify on alarm” or a location’s “notify on fire”) and an operator has the notify flag — optionally scoped to specific locations.' },
    ],
  },
  operators: {
    title: 'Operators',
    icon: '👥',
    sections: [
      { body: 'Operators are the people who sign in to manage the system — PocketBase’s built-in users auth collection, separate from the superuser break-glass account that bypasses everything.' },
      {
        heading: 'Capabilities',
        body: 'Ability is a set of orthogonal capabilities, not a rank. Read is open to any signed-in operator; only writes and commands are gated.',
        items: [
          { term: 'Enroll', def: 'Manage cardholders and credentials, including bulk Import.' },
          { term: 'Policy', def: 'Edit roles, access groups, schedules, and holidays.' },
          { term: 'Topology', def: 'Edit locations, controllers, portals, areas, and aux I/O.' },
          { term: 'Command', def: 'Send operator commands — grant, posture override, output.' },
          { term: 'Operators', def: 'Manage operator accounts and view the audit log.' },
        ],
      },
      { heading: 'Notifications', body: 'Set an operator’s notify flag to page them on alarm/fire email; optionally scope it to specific locations so they’re only paged for sites they cover.' },
    ],
  },
  'audit-log': {
    title: 'Audit Log',
    icon: '📜',
    sections: [
      { body: 'A control-plane change history: every API-driven policy edit and operator login, recorded to the audit_logs collection. This is the “who changed the configuration” trail.' },
      { heading: 'Versus Events', body: 'Distinct from Events, which is the data-plane access timeline (taps, door state, alarms). The Audit Log tracks edits to the system; Events tracks what happened at the doors.' },
      { heading: 'Scope', body: 'accessd’s own programmatic writes (heartbeats, the events projection, the KV mirror) are excluded by construction, and secrets are stripped from rows. Entries are pruned after the configured retention window.' },
    ],
  },
}

/** Resolve the help topic for a route path, e.g. "/portals/abc/edit" → portals. */
export function helpForPath(path: string): HelpTopic | null {
  const seg = path.split('/')[1] || ''
  if (!seg) return TOPICS.overview
  return TOPICS[seg] ?? null
}

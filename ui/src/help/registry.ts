/**
 * Route-keyed contextual help.
 *
 * Help is authored once per resource ("topic") and surfaced on its list, detail,
 * and form pages alike. The topic is derived from the first path segment, so no
 * per-view wiring is needed — `HelpButton`/`HelpFab` look up the current route here
 * and self-hide when there is no entry.
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
  holidays: {
    title: 'Holidays',
    icon: '📅',
    sections: [
      { body: 'A holiday is a date (or date range) on which holiday-observing schedules treat their windows as closed.' },
      { heading: 'Scope', body: 'Holidays apply to every schedule that observes them. A schedule with “ignore holidays” set is unaffected.' },
    ],
  },
  portals: {
    title: 'Portals',
    icon: '🚪',
    sections: [
      { body: 'A portal is a controllable opening — door, gate, turnstile, elevator, or logical. A controller drives it via the lock relay and the DPS/REX inputs.' },
      { heading: 'Postures', body: 'The standing posture is the default state. A scheduled posture (while its window is open) or a runtime command can override it on the controller.', items: POSTURES },
      { heading: 'Controller binding', body: 'A portal is bound to the controller whose code matches its Controller field — reassigning it takes effect without touching the edge box. Unassigned portals are armed by no box.' },
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
    icon: '🛡️',
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
}

/** Resolve the help topic for a route path, e.g. "/portals/abc/edit" → portals. */
export function helpForPath(path: string): HelpTopic | null {
  const seg = path.split('/')[1] || ''
  if (!seg) return TOPICS.overview
  return TOPICS[seg] ?? null
}

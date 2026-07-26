/**
 * Turn a decision reason code into something a badge holder can act on.
 *
 * The keys are the STABLE codes from internal/policy/policy.go — they are a public
 * contract, so this map has to spell them exactly. An earlier version invented its own
 * (`deny_schedule`, `deny_credential_expired`, `deny_posture_lockdown`), none of which
 * exist, so every denial fell through to the generic message. Unmapped codes still fall
 * through rather than showing an internal string.
 *
 * Shared by every badge action — unlock, arm, disarm, output — because they all answer
 * with `{ok, reason}` from the same reason vocabulary, and a holder should not get three
 * different phrasings of "outside your hours".
 */
export function reasonText(reason?: string): string {
  switch (reason) {
    // Shared across every target kind.
    case 'deny_schedule_closed':
      return 'Not within your allowed hours'
    case 'deny_expired':
      return 'Your pass has expired'
    case 'deny_not_yet_valid':
      return 'Your pass has not started yet'
    case 'deny_revoked':
      return 'Your pass is no longer active'
    case 'deny_unknown_credential':
      return 'Your pass is not recognised — contact your host'
    case 'deny_no_access':
      return 'Your badge does not cover this'

    // Doors.
    case 'deny_lockdown':
      return 'This door is in lockdown'
    case 'deny_point_disabled':
      return 'This door is out of service'
    case 'deny_unknown_point':
      return 'This door is no longer available'

    // Areas and outputs.
    case 'deny_unknown_area':
      return 'This area is no longer available'
    case 'deny_unknown_output':
      return 'This control is no longer available'
    // A group grants the area but not this action — a misconfiguration on the group,
    // which is why the holder is pointed at a person rather than at a setting.
    case 'deny_no_area_right':
      return 'Your badge does not allow this here — contact your host'

    // badgeapi's own pre-checks, ahead of the policy decision.
    case 'remote_unlock_not_allowed':
      return 'This door cannot be opened remotely'
    case 'remote_arm_not_allowed':
      return 'This area can only be armed on site'
    case 'remote_output_not_allowed':
      return 'This control can only be used on site'
    case 'no_credential':
      return 'No pass is issued to you'
    default:
      return ''
  }
}

/** The message for a failed badge action, including the rate-limit case. */
export function actionErrorText(err: any, fallback = 'Not allowed right now'): string {
  if (err?.status === 429) return 'Too many attempts — wait a moment'
  return reasonText(err?.response?.reason as string | undefined) || fallback
}

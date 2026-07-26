import PocketBase, { LocalAuthStore } from 'pocketbase'

/**
 * A SECOND PocketBase client, for the badge tier (`badge_users`).
 *
 * It must be a separate client instance, not the operator `pb` from ./pb: the SDK
 * holds exactly one authStore per client, so sharing one would mean a badge holder
 * signing in clobbers an operator's session in the same browser (and vice versa).
 * That is not hypothetical — an operator minting a visitor at a kiosk, then the
 * visitor signing in on the same machine, is the normal flow.
 *
 * The distinct localStorage key is what keeps the two sessions independent. Anything
 * badge-facing must import THIS client; anything operator-facing must import `pb`.
 */
export const BADGE_AUTH_STORE_KEY = 'pocketbase_badge_auth'

export const badgePb = new PocketBase('/', new LocalAuthStore(BADGE_AUTH_STORE_KEY))

// Match the operator client: don't auto-cancel in-flight requests on unmount.
badgePb.autoCancellation(false)

export default badgePb

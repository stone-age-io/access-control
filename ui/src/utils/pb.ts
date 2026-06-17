import PocketBase from 'pocketbase'

/**
 * PocketBase client singleton — the single source of truth for all API calls.
 *
 * Configured with a relative base URL ('/') so it works both in dev (via the
 * Vite proxy to accessd on :8090) and in production (the compiled UI is served
 * by accessd's embedded PocketBase from pb_public/, same origin).
 */
export const pb = new PocketBase('/')

// Don't auto-cancel in-flight requests when a component unmounts — better UX
// for list/detail navigation.
pb.autoCancellation(false)

export default pb

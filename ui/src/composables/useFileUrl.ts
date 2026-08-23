import { onMounted, ref } from 'vue'
import type PocketBase from 'pocketbase'
import { pb } from '@/utils/pb'

/**
 * Short-lived file tokens for PROTECTED file fields.
 *
 * `cardholders.photo` is protected (migration 1750000029): its URL carries no
 * implicit authorization, so PocketBase requires a token appended to every request.
 * That is the whole point — an ordinary PocketBase file URL is public to anyone
 * holding the link, forever, which is the wrong default for a photo of a person.
 *
 * # Why the token is cached, and why that is a rendering fix and not a saving
 *
 * The token is part of the URL's query string, so it is part of the browser's cache
 * key: minting a fresh one per component mount produces a URL the cache has never
 * seen, and the image is re-downloaded every single time. On the badge that was
 * visible — the holder's photo blinked on every screen change, because the face is
 * mounted and unmounted as they move around the badge. So a token is cached for a
 * TTL well inside its lifetime and every caller shares it, which keeps the URL
 * STABLE across mounts and lets the ordinary HTTP cache do its job. Nothing here
 * caches an image or a response; it only stops us inventing a new URL for one.
 *
 * # Why the cache is per client
 *
 * There are two sessions in this app (the operator `pb` and the badge `badgePb`, see
 * utils/badgePb) and a file token is bound to the session that minted it. One shared
 * slot would hand an operator's token to a holder's URL, or the reverse, and 403.
 */
const TTL_MS = 90_000

type Entry = {
  cached: { token: string; at: number } | null
  inflight: Promise<string> | null
}

const entries = new WeakMap<PocketBase, Entry>()

function entryFor(client: PocketBase): Entry {
  let entry = entries.get(client)
  if (!entry) {
    entry = { cached: null, inflight: null }
    entries.set(client, entry)
  }
  return entry
}

/** Returns a valid file token for one session, reusing the cached one until it nears expiry. */
export async function fileTokenFor(client: PocketBase): Promise<string> {
  const entry = entryFor(client)
  if (entry.cached && Date.now() - entry.cached.at < TTL_MS) return entry.cached.token
  // Collapse concurrent callers onto one request: a list view mounts every row at
  // once, and each would otherwise fire its own token call.
  if (!entry.inflight) {
    entry.inflight = client.files
      .getToken()
      .then((token) => {
        entry.cached = { token, at: Date.now() }
        return token
      })
      .finally(() => {
        entry.inflight = null
      })
  }
  return entry.inflight
}

/** The operator session's token — the common case, and what `useFileUrl` resolves. */
export function fileToken(): Promise<string> {
  return fileTokenFor(pb)
}

/**
 * Drops a session's cached token. Call on sign-out: a token is bound to the session
 * that minted it, so a stale one would 403 for the next person in the same tab.
 *
 * Dropping the whole entry rather than nulling its fields also settles the race — an
 * in-flight request resolves into the orphaned entry and is never read, where writing
 * back into a live one would repopulate the cache moments after it was cleared.
 */
export function clearFileToken(client: PocketBase = pb): void {
  entries.delete(client)
}

/**
 * Resolves protected-file URLs synchronously inside templates. The token is fetched
 * once on mount; until it lands (and if it fails) `url()` returns '', so callers
 * fall back to a non-image rendering — `Avatar` shows initials — rather than
 * emitting a URL that would 403 and render as a broken image.
 */
export function useFileUrl() {
  const token = ref('')

  onMounted(async () => {
    try {
      token.value = await fileToken()
    } catch {
      token.value = '' // fail soft — the caller's fallback is a valid rendering
    }
  })

  const url = (
    record: { id: string; collectionId?: string; collectionName?: string },
    filename: string | undefined,
    thumb?: string,
  ): string => {
    if (!filename || !token.value) return ''
    const opts: Record<string, string> = { token: token.value }
    if (thumb) opts.thumb = thumb
    return pb.files.getURL(record, filename, opts)
  }

  return { token, url }
}

import { onMounted, ref } from 'vue'
import { pb } from '@/utils/pb'

/**
 * Short-lived file tokens for PROTECTED file fields.
 *
 * `cardholders.photo` is protected (migration 1750000029): its URL carries no
 * implicit authorization, so PocketBase requires a token appended to every request.
 * That is the whole point — an ordinary PocketBase file URL is public to anyone
 * holding the link, forever, which is the wrong default for a photo of a person.
 *
 * A token is valid for a few minutes and works for every protected URL on the page,
 * so it is cached at module scope and refreshed on a TTL rather than fetched per
 * image.
 */
const TTL_MS = 90_000

let cached: { token: string; at: number } | null = null
let inflight: Promise<string> | null = null

/** Returns a valid file token, reusing the cached one until it nears expiry. */
export async function fileToken(): Promise<string> {
  if (cached && Date.now() - cached.at < TTL_MS) return cached.token
  // Collapse concurrent callers onto one request: a list view mounts every row at
  // once, and each would otherwise fire its own token call.
  if (!inflight) {
    inflight = pb.files
      .getToken()
      .then((token) => {
        cached = { token, at: Date.now() }
        return token
      })
      .finally(() => {
        inflight = null
      })
  }
  return inflight
}

/**
 * Drops the cached token. Call on sign-out: a token is bound to the session that
 * minted it, so a stale one would 403 for the next operator in the same tab.
 */
export function clearFileToken(): void {
  cached = null
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

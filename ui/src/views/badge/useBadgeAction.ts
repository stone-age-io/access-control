/**
 * Running one badge action, and showing its outcome for exactly as long as it is true.
 *
 * # Why an outcome expires
 *
 * The outcome used to be written into a map and left there, so a tap on "Loading Bay" put a
 * green "Unlocked" under the button that stayed until the badge was reloaded. That is not
 * merely untidy: a remote unlock is a momentary PULSE — the strike relocks immediately — so a
 * persistent "Unlocked" beside a door is a past event rendered as present state, which is the
 * one thing this app must never do. It is the same argument the service worker caches nothing
 * for: a badge's job is to say what is true right now.
 *
 * So every outcome is transient, and nothing on the screen outlives its own truth. Failures
 * linger longer than successes because they have to be READ — a success is a confirmation of
 * something the holder just felt happen, a failure is a sentence explaining why it did not.
 *
 * # Why it is shared
 *
 * Two surfaces run the same four actions on the same records: the action lists in
 * BadgeAccessPanel and the floor plan's selected-marker bar (BadgeFloorplan). They had a copy
 * of this each — one keyed by record id, one a pair of scalars — and the copies had already
 * drifted in how they cleared. Keying by id in one place also fixes the floor plan's version
 * for free: an outcome belongs to the marker it was produced by, so switching markers cannot
 * caption one door with another's result.
 */
import { onUnmounted, ref } from 'vue'
import { badgePb } from '@/utils/badgePb'
import { actionErrorText } from './reasonText'

export interface BadgeActionResult {
  ok: boolean
  message: string
}

/**
 * Long enough to read at arm's length while walking away, short enough that it cannot be
 * mistaken for the state of the door. Failures get more, because a denial reason is a
 * sentence rather than one word, and the holder may need to act on it.
 */
const SUCCESS_MS = 4000
const FAILURE_MS = 9000

export function useBadgeAction() {
  /** In flight, keyed by record id, so one target's spinner is not another's. */
  const busy = ref<Record<string, boolean>>({})
  /** The outcome currently worth showing, keyed the same way. */
  const results = ref<Record<string, BadgeActionResult>>({})

  const timers = new Map<string, ReturnType<typeof setTimeout>>()

  /** Drop an outcome and its timer, whether it expired or was superseded. */
  function forget(id: string) {
    const timer = timers.get(id)
    if (timer !== undefined) {
      clearTimeout(timer)
      timers.delete(id)
    }
    if (results.value[id]) {
      const next = { ...results.value }
      delete next[id]
      results.value = next
    }
  }

  function report(id: string, result: BadgeActionResult) {
    results.value = { ...results.value, [id]: result }
    timers.set(
      id,
      setTimeout(() => forget(id), result.ok ? SUCCESS_MS : FAILURE_MS),
    )
  }

  /**
   * A short tick on success, where the platform offers one.
   *
   * This is the one surface in the app where it earns its keep: the holder is at a door with
   * their hands full, and the useful confirmation is one they do not have to look at. Feature
   * detected rather than assumed — iOS Safari does not implement it, so on an iPhone the colour
   * and the sentence are the whole of the feedback, which is why neither was skipped.
   *
   * Success only. A failure has a sentence that must be read, and buzzing at someone is not a
   * way to tell them why they were refused.
   */
  function confirmHaptic() {
    if (typeof navigator !== 'undefined' && typeof navigator.vibrate === 'function') {
      navigator.vibrate(25)
    }
  }

  /**
   * POST one badge action. `after` runs only on success — it is how arming asks the shell to
   * re-fetch, since an area's state is resolved server-side and assuming the write landed as
   * requested is the one thing a badge must not do.
   */
  async function run(id: string, path: string, successText: string, after?: () => void) {
    if (busy.value[id]) return
    // A new attempt supersedes the last outcome immediately: the holder pressed again, so the
    // previous answer is no longer what they are waiting to hear.
    forget(id)
    busy.value = { ...busy.value, [id]: true }
    try {
      await badgePb.send(path, { method: 'POST' })
      report(id, { ok: true, message: successText })
      confirmHaptic()
      after?.()
    } catch (err: any) {
      report(id, { ok: false, message: actionErrorText(err) })
    } finally {
      const next = { ...busy.value }
      delete next[id]
      busy.value = next
    }
  }

  // A pending timer whose component is gone would fire into a discarded ref. Harmless in
  // practice, but this is a phone and switching views destroys these components constantly.
  onUnmounted(() => {
    for (const timer of timers.values()) clearTimeout(timer)
    timers.clear()
  })

  return { busy, results, run }
}

import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { badgePb } from '@/utils/badgePb'

/**
 * Badge-tier auth store — sign-in for cardholders and visitors, NOT operators.
 *
 * Entirely separate from stores/auth.ts even though both sign in real people: a
 * different auth collection (`cardholders`, not `users`), a different PocketBase client,
 * a different localStorage key, and no `permissions` concept at all. A badge holder has
 * no capabilities; what they can do is decided server-side by policy.Decide against
 * their own credential.
 *
 * The separation has to survive the collections being collapsed, because one human can
 * legitimately be both: the security guard who badges in AND runs the console is two
 * accounts in two privilege domains, and a token issued for a phone must never be
 * usable as a console token.
 *
 * Three sign-in methods, all enabled on the collection:
 *
 *   - OTP      — a code emailed to the address on the badge. The visitor path.
 *   - Password — email + password, for a staff holder who signs in for years and
 *                should not wait on an email each time. Also the ONLY method that
 *                works on an install with no SMTP configured at all.
 *   - OAuth2   — for someone with an existing identity at the organisation.
 *
 * The two-step OTP flow: requestOtp() returns an `otpId` that must be held and
 * passed back to verifyOtp() with the code from the email. The id alone is useless —
 * the code only reaches the invited inbox, which is what makes an emailed badge link
 * more than a bare bearer token.
 */
export const useBadgeAuthStore = defineStore('badgeAuth', () => {
  const record = ref<Record<string, any> | null>(
    badgePb.authStore.isValid ? (badgePb.authStore.record as unknown as Record<string, any>) : null,
  )

  const isAuthenticated = computed(() => !!record.value && badgePb.authStore.isValid)
  const email = computed(() => (record.value?.email as string) || '')
  const kind = computed(() => (record.value?.kind as string) || '')
  /**
   * Whether this badge has a password its holder knows. Drives "Set a password" vs
   * "Change password", and whether the current one must be supplied. Mirrors the
   * server's `password_set` — the server re-reads it and is the real gate.
   */
  const hasPassword = computed(() => !!record.value?.password_set)

  /**
   * Step 1: ask PocketBase to email a one-time code. Returns the otpId to pass to
   * verifyOtp.
   *
   * Note the deliberate absence of an "unknown address" signal: PocketBase does not
   * distinguish a known from an unknown email here, and neither should we — telling
   * a caller which addresses have badges would turn this into an enumeration oracle.
   */
  async function requestOtp(address: string): Promise<string> {
    const res = await badgePb.collection('cardholders').requestOTP(address)
    return res.otpId
  }

  /** Step 2: exchange the otpId + emailed code for a session. */
  async function verifyOtp(otpId: string, code: string) {
    const authData = await badgePb.collection('cardholders').authWithOTP(otpId, code)
    record.value = authData.record as unknown as Record<string, any>
  }

  /** Email + password sign-in, for a holder whose badge has one. */
  async function loginWithPassword(address: string, password: string) {
    const authData = await badgePb.collection('cardholders').authWithPassword(address, password)
    record.value = authData.record as unknown as Record<string, any>
  }

  /**
   * Send a password-reset email. Resolves regardless of whether the address has a
   * badge — the caller must not learn which addresses exist, same reasoning as
   * requestOtp.
   */
  async function requestPasswordReset(address: string) {
    await badgePb.collection('cardholders').requestPasswordReset(address)
  }

  /**
   * Set or change this holder's own password via POST /api/badge/password.
   *
   * `oldPassword` is required only when the badge already has one the holder knows
   * (`password_set`); a holder who signed in by OTP is setting their first password
   * and has nothing to prove.
   *
   * Changing a password rotates the record's tokenKey server-side, which invalidates
   * EVERY session including this one. So we immediately re-authenticate with the new
   * password rather than let the holder discover it as a 401 on their next tap.
   */
  async function setPassword(password: string, passwordConfirm: string, oldPassword?: string) {
    const address = email.value
    await badgePb.send('/api/badge/password', {
      method: 'POST',
      body: { password, passwordConfirm, oldPassword: oldPassword || '' },
    })
    await loginWithPassword(address, password)
  }

  /** OAuth2 sign-in for a contractor or staff member with an existing identity. */
  async function loginWithOAuth2(provider: string) {
    const authData = await badgePb.collection('cardholders').authWithOAuth2({ provider })
    record.value = authData.record as unknown as Record<string, any>
  }

  /** Which OAuth2 providers the install has configured, if any. */
  async function listOAuth2Providers(): Promise<{ name: string; displayName: string }[]> {
    try {
      const methods = await badgePb.collection('cardholders').listAuthMethods()
      return (methods.oauth2?.providers || []).map((p: any) => ({
        name: p.name,
        displayName: p.displayName || p.name,
      }))
    } catch {
      return [] // an install with no providers is the common case, not an error
    }
  }

  function logout() {
    badgePb.authStore.clear()
    record.value = null
  }

  /**
   * Restore a session on boot, verifying it is still valid server-side. A badge
   * login can be deleted or its credential revoked between visits, so a locally
   * valid token is not proof of anything.
   */
  async function initialize() {
    if (!badgePb.authStore.isValid || !badgePb.authStore.record) return
    record.value = badgePb.authStore.record as unknown as Record<string, any>
    try {
      const authData = await badgePb.collection('cardholders').authRefresh()
      record.value = authData.record as unknown as Record<string, any>
    } catch {
      logout()
    }
  }

  return {
    record,
    isAuthenticated,
    email,
    kind,
    hasPassword,
    requestOtp,
    verifyOtp,
    loginWithPassword,
    requestPasswordReset,
    setPassword,
    loginWithOAuth2,
    listOAuth2Providers,
    logout,
    initialize,
  }
})

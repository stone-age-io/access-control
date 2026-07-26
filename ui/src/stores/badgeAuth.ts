import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { badgePb } from '@/utils/badgePb'

/**
 * Badge-tier auth store — sign-in for cardholders and visitors, NOT operators.
 *
 * Entirely separate from stores/auth.ts: a different auth collection
 * (`badge_users`), a different PocketBase client, a different localStorage key, and
 * no `permissions` concept at all. A badge holder has no capabilities; what they can
 * do is decided server-side by policy.Decide against their own credential.
 *
 * Sign-in is OTP (a code emailed to the address on the badge) or OAuth2. Password
 * auth is disabled on the collection — a visitor should never have to invent a
 * password for a one-day pass, and it removes a credential-stuffing surface.
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
   * Step 1: ask PocketBase to email a one-time code. Returns the otpId to pass to
   * verifyOtp.
   *
   * Note the deliberate absence of an "unknown address" signal: PocketBase does not
   * distinguish a known from an unknown email here, and neither should we — telling
   * a caller which addresses have badges would turn this into an enumeration oracle.
   */
  async function requestOtp(address: string): Promise<string> {
    const res = await badgePb.collection('badge_users').requestOTP(address)
    return res.otpId
  }

  /** Step 2: exchange the otpId + emailed code for a session. */
  async function verifyOtp(otpId: string, code: string) {
    const authData = await badgePb.collection('badge_users').authWithOTP(otpId, code)
    record.value = authData.record as unknown as Record<string, any>
  }

  /** OAuth2 sign-in for a contractor or staff member with an existing identity. */
  async function loginWithOAuth2(provider: string) {
    const authData = await badgePb.collection('badge_users').authWithOAuth2({ provider })
    record.value = authData.record as unknown as Record<string, any>
  }

  /** Which OAuth2 providers the install has configured, if any. */
  async function listOAuth2Providers(): Promise<{ name: string; displayName: string }[]> {
    try {
      const methods = await badgePb.collection('badge_users').listAuthMethods()
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
      const authData = await badgePb.collection('badge_users').authRefresh()
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
    requestOtp,
    verifyOtp,
    loginWithOAuth2,
    listOAuth2Providers,
    logout,
    initialize,
  }
})

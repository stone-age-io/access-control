import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { pb } from '@/utils/pb'

/**
 * Auth store — operator sign-in for the management UI.
 *
 * Operators authenticate against the built-in `users` auth collection. Ability is
 * an orthogonal set of `permissions` (enroll / policy / topology / command /
 * operators), not a rank. The permissions drive both the API collection rules
 * (the real boundary) and this store's `can()` helper. Reads are a universal floor
 * for any authenticated operator. Superusers remain the break-glass account: they
 * sign in at the PocketBase dashboard (/_/), not here.
 */
export const useAuthStore = defineStore('auth', () => {
  const user = ref<Record<string, any> | null>(null)

  const isAuthenticated = computed(() => !!user.value)
  const email = computed(() => (user.value?.email as string) || '')
  const initial = computed(() => (email.value[0] || 'A').toUpperCase())

  /** The operator's capability set (empty before load / read-only viewer). */
  const permissions = computed<string[]>(() => (user.value?.permissions as string[]) ?? [])
  /** Whether the operator holds a given capability. Reads need no capability. */
  function can(cap: string): boolean {
    return permissions.value.includes(cap)
  }

  async function login(emailAddr: string, password: string) {
    const authData = await pb.collection('users').authWithPassword(emailAddr, password)
    user.value = authData.record as unknown as Record<string, any>
  }

  async function logout() {
    pb.authStore.clear()
    user.value = null
  }

  /**
   * Restore a session from localStorage on app boot, verifying the token is
   * still valid server-side. Must run before the first router navigation.
   */
  async function initializeFromAuth() {
    if (pb.authStore.isValid && pb.authStore.record) {
      user.value = pb.authStore.record as unknown as Record<string, any>
      try {
        const authData = await pb.collection('users').authRefresh()
        user.value = authData.record as unknown as Record<string, any>
      } catch {
        await logout()
      }
    }
  }

  return { user, isAuthenticated, email, initial, permissions, can, login, logout, initializeFromAuth }
})

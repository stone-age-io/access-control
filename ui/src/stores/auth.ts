import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { pb } from '@/utils/pb'

/**
 * Auth store — operator sign-in for the management UI.
 *
 * Operators authenticate against the built-in `users` auth collection, gated by
 * a `role` field (admin / operator / viewer). The role drives both the API
 * collection rules (the real boundary) and this store's UI helpers. Superusers
 * remain the break-glass account: they sign in at the PocketBase dashboard (/_/),
 * not here.
 */
export const useAuthStore = defineStore('auth', () => {
  const user = ref<Record<string, any> | null>(null)

  const isAuthenticated = computed(() => !!user.value)
  const email = computed(() => (user.value?.email as string) || '')
  const initial = computed(() => (email.value[0] || 'A').toUpperCase())

  /** Operator role: 'admin' | 'operator' | 'viewer' (or '' before load). */
  const role = computed(() => (user.value?.role as string) || '')
  const isAdmin = computed(() => role.value === 'admin')
  /** Can perform daily-ops writes (manage people/credentials, issue commands). */
  const canWrite = computed(() => role.value === 'operator' || role.value === 'admin')

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

  return { user, isAuthenticated, email, initial, role, isAdmin, canWrite, login, logout, initializeFromAuth }
})

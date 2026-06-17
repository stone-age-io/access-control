import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { pb } from '@/utils/pb'

/**
 * Auth store — single-tenant, superuser-only.
 *
 * accessd's access-control collections have nil API rules (superusers only),
 * so the management UI authenticates against PocketBase's built-in _superusers
 * auth collection. No orgs/memberships/OAuth (that lives in the platform UI).
 */
export const useAuthStore = defineStore('auth', () => {
  const user = ref<Record<string, any> | null>(null)

  const isAuthenticated = computed(() => !!user.value)
  const email = computed(() => (user.value?.email as string) || '')
  const initial = computed(() => (email.value[0] || 'A').toUpperCase())

  async function login(emailAddr: string, password: string) {
    const authData = await pb.collection('_superusers').authWithPassword(emailAddr, password)
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
        const authData = await pb.collection('_superusers').authRefresh()
        user.value = authData.record as unknown as Record<string, any>
      } catch {
        await logout()
      }
    }
  }

  return { user, isAuthenticated, email, initial, login, logout, initializeFromAuth }
})

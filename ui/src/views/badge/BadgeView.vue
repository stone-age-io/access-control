<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { badgePb } from '@/utils/badgePb'
import { useBadgeAuthStore } from '@/stores/badgeAuth'
import { useBrandingStore } from '@/stores/branding'
import BrandLogo from '@/components/common/BrandLogo.vue'
import BadgePassPanel from './BadgePassPanel.vue'
import BadgeAccessPanel from './BadgeAccessPanel.vue'
import type { BadgeMe } from '@/types/badge'

/**
 * The badge shell: one fetch of GET /api/badge/me, two tabs over it.
 *
 *   Badge   the face — photo, QR, validity, password (BadgePassPanel)
 *   Access  what it can do — doors, areas, controls (BadgeAccessPanel)
 *
 * Split because the two halves answer different questions and are reached at different
 * moments: the face is what you hold up at a desk, the Access tab is what you use with
 * your hands full outside a door. Keeping the QR on its own tab also means the thing
 * most worth photographing is not on screen while someone is pressing buttons.
 *
 * The tab is a query param so a holder can bookmark the half they actually use, and so
 * "open my doors" can be linked to directly.
 */
const route = useRoute()
const router = useRouter()
const badgeAuth = useBadgeAuthStore()
const branding = useBrandingStore()

type Tab = 'badge' | 'access'
function isTab(v: unknown): v is Tab {
  return v === 'badge' || v === 'access'
}

const me = ref<BadgeMe | null>(null)
const loading = ref(true)
const loadError = ref('')
const tab = ref<Tab>(isTab(route.query.tab) ? route.query.tab : 'badge')

function setTab(t: Tab) {
  tab.value = t
  router.replace({ path: route.path, query: { ...route.query, tab: t } }).catch(() => {})
}

/** Shown on the Access tab so a holder can see at a glance whether there is anything there. */
const actionCount = computed(() => {
  const m = me.value
  if (!m) return 0
  return m.portals.length + m.areas.length + m.outputs.length
})

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    me.value = await badgePb.send<BadgeMe>('/api/badge/me', { method: 'GET' })
  } catch (err: any) {
    if (err?.status === 401) {
      badgeAuth.logout()
      router.push({ name: 'Login', query: { as: 'badge' } })
      return
    }
    loadError.value = 'Could not load your badge. Try again in a moment.'
  } finally {
    loading.value = false
  }
}

/**
 * Re-fetch after an action that changes server-held state (arming). Deliberately a full
 * reload of the badge rather than a local patch: an area's arm-state is resolved
 * server-side from the policy graph, so asking again is the only honest way to show it.
 */
async function refresh() {
  try {
    me.value = await badgePb.send<BadgeMe>('/api/badge/me', { method: 'GET' })
  } catch {
    // Leave the previous view in place: the action itself already reported its outcome,
    // and replacing that with a load error would be misleading.
  }
}

async function signOut() {
  badgeAuth.logout()
  router.push({ name: 'Login', query: { as: 'badge' } })
}

onMounted(load)
</script>

<template>
  <div class="min-h-screen bg-base-200 p-4">
    <div class="max-w-sm mx-auto space-y-4">
      <!-- Header -->
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-2">
          <BrandLogo :size="28" />
          <span class="text-sm font-medium">{{ branding.appName }}</span>
        </div>
        <button class="btn btn-ghost btn-xs" @click="signOut">Sign out</button>
      </div>

      <div v-if="loading" class="flex justify-center p-12">
        <span class="loading loading-spinner loading-lg"></span>
      </div>

      <div v-else-if="loadError" class="alert alert-error text-sm">
        <span>{{ loadError }}</span>
        <button class="btn btn-xs" @click="load">Retry</button>
      </div>

      <template v-else-if="me">
        <div role="tablist" class="tabs tabs-boxed">
          <button
            role="tab"
            class="tab"
            :class="tab === 'badge' ? 'tab-active' : ''"
            @click="setTab('badge')"
          >
            Badge
          </button>
          <button
            role="tab"
            class="tab gap-1"
            :class="tab === 'access' ? 'tab-active' : ''"
            @click="setTab('access')"
          >
            Access
            <span v-if="actionCount" class="badge badge-sm">{{ actionCount }}</span>
          </button>
        </div>

        <BadgePassPanel v-if="tab === 'badge'" :me="me" />
        <BadgeAccessPanel v-else :me="me" @refresh="refresh" />
      </template>
    </div>
  </div>
</template>

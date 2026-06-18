<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { pb } from '@/utils/pb'
import { formatRelativeTime, formatConstant } from '@/utils/format'
import type { AccessEvent } from '@/types/pocketbase'
import BaseCard from '@/components/ui/BaseCard.vue'

interface StatCard {
  label: string
  icon: string
  path: string
  collection: string
  count: number | null
}

const stats = ref<StatCard[]>([
  { label: 'Locations', icon: '🏢', path: '/locations', collection: 'locations', count: null },
  { label: 'Schedules', icon: '🗓️', path: '/schedules', collection: 'schedules', count: null },
  { label: 'Portals', icon: '🚪', path: '/portals', collection: 'portals', count: null },
  { label: 'Access Groups', icon: '🗝️', path: '/access-groups', collection: 'access_groups', count: null },
  { label: 'Roles', icon: '🛡️', path: '/roles', collection: 'roles', count: null },
  { label: 'Cardholders', icon: '🪪', path: '/cardholders', collection: 'cardholders', count: null },
  { label: 'Credentials', icon: '🎫', path: '/credentials', collection: 'credentials', count: null },
  { label: 'Events', icon: '📋', path: '/events', collection: 'events', count: null },
])

const recentEvents = ref<AccessEvent[]>([])
const loading = ref(true)

async function loadCounts() {
  await Promise.all(
    stats.value.map(async (s) => {
      try {
        const res = await pb.collection(s.collection).getList(1, 1)
        s.count = res.totalItems
      } catch {
        s.count = 0
      }
    })
  )
}

async function loadRecentEvents() {
  try {
    const res = await pb.collection('events').getList<AccessEvent>(1, 8, { sort: '-ts,-created' })
    recentEvents.value = res.items
  } catch {
    recentEvents.value = []
  }
}

function eventBadge(e: AccessEvent): string {
  if (e.kind === 'tap') return e.allow ? 'badge-success' : 'badge-error'
  if (e.kind === 'fire' || e.kind === 'alarm') return 'badge-warning'
  return 'badge-ghost'
}

onMounted(async () => {
  loading.value = true
  await Promise.all([loadCounts(), loadRecentEvents()])
  loading.value = false
})
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-3xl font-bold">Overview</h1>
      <p class="text-base-content/70 mt-1">Policy graph at a glance and recent access activity.</p>
    </div>

    <!-- Stat grid -->
    <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
      <router-link
        v-for="s in stats"
        :key="s.collection"
        :to="s.path"
        class="card bg-base-100 shadow-xl hover:shadow-2xl hover:border-primary/40 border border-transparent transition-all"
      >
        <div class="card-body p-4">
          <div class="flex items-center justify-between">
            <span class="text-2xl">{{ s.icon }}</span>
            <span class="text-3xl font-bold tabular-nums">
              <span v-if="s.count === null" class="loading loading-dots loading-sm opacity-40"></span>
              <template v-else>{{ s.count }}</template>
            </span>
          </div>
          <div class="text-sm font-medium opacity-70 mt-1">{{ s.label }}</div>
        </div>
      </router-link>
    </div>

    <!-- Recent activity -->
    <BaseCard title="Recent Activity">
      <template #actions>
        <router-link to="/events" class="btn btn-ghost btn-sm">View all</router-link>
      </template>

      <div v-if="loading" class="flex justify-center p-6">
        <span class="loading loading-spinner loading-md opacity-40"></span>
      </div>

      <div v-else-if="recentEvents.length === 0" class="text-center py-8 opacity-50">
        <span class="text-3xl">🛈</span>
        <p class="text-sm mt-2">No events yet. They appear here once controllers start publishing to ACC_EVENTS.</p>
      </div>

      <ul v-else class="divide-y divide-base-200">
        <li v-for="e in recentEvents" :key="e.id" class="flex items-center gap-3 py-2.5">
          <span class="badge badge-sm" :class="eventBadge(e)">{{ e.kind || 'event' }}</span>
          <div class="flex-1 min-w-0">
            <div class="text-sm truncate">
              <span class="font-medium">{{ e.location || '—' }}</span>
              <span v-if="e.portal" class="opacity-60"> / {{ e.portal }}</span>
              <span v-if="e.reason" class="opacity-60"> — {{ formatConstant(e.reason) }}</span>
            </div>
            <div v-if="e.credential || e.user" class="text-xs opacity-50 truncate">
              <span v-if="e.credential" class="font-mono">{{ e.credential }}</span>
              <span v-if="e.user"> · {{ e.user }}</span>
            </div>
          </div>
          <span class="text-xs opacity-50 whitespace-nowrap">{{ formatRelativeTime(e.ts || e.created) }}</span>
        </li>
      </ul>
    </BaseCard>
  </div>
</template>

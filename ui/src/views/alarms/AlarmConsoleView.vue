<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { pb } from '@/utils/pb'
import { useAuthStore } from '@/stores/auth'
import { useAlarmAck } from '@/composables/useAlarmAck'
import { formatDate, formatConstant } from '@/utils/format'
import type { AccessEvent } from '@/types/pocketbase'
import ListLayout from '@/components/ui/ListLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'

const auth = useAuthStore()
const { acking, ack } = useAlarmAck()

const alarms = ref<AccessEvent[]>([])
const loading = ref(true)
const error = ref('')
let unsub: (() => void) | null = null

const canCommand = computed(() => auth.can('command'))

// Bound the console to recent unacked alarms so a long-unacked row — or a stream
// replay that resurrects old rows (the v1 ack-on-projection wart) — can't make the
// console unusable. A dedicated active_alarms projection is the deferred fix.
const WINDOW_DAYS = 7
function cutoffISO(): string {
  return new Date(Date.now() - WINDOW_DAYS * 86400000).toISOString()
}

function isAlarm(e: AccessEvent): boolean {
  return e.kind === 'alarm' || e.kind === 'fire'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await pb.collection('events').getList<AccessEvent>(1, 200, {
      filter: `(kind = "alarm" || kind = "fire") && acknowledged = false && created > "${cutoffISO()}"`,
      sort: '-created',
    })
    alarms.value = res.items
  } catch (err: any) {
    error.value = err?.message || 'Failed to load alarms'
  } finally {
    loading.value = false
  }
}

async function subscribe() {
  unsub = await pb.collection('events').subscribe<AccessEvent>('*', (e) => {
    const rec = e.record
    if (e.action === 'create' && isAlarm(rec) && !rec.acknowledged) {
      alarms.value = [rec, ...alarms.value]
    } else if (e.action === 'update') {
      // Acknowledged elsewhere (another operator) — drop it from the live list.
      if (rec.acknowledged) alarms.value = alarms.value.filter((a) => a.id !== rec.id)
    } else if (e.action === 'delete') {
      alarms.value = alarms.value.filter((a) => a.id !== rec.id)
    }
  })
}

async function acknowledge(e: AccessEvent) {
  if (await ack(e.id)) {
    alarms.value = alarms.value.filter((a) => a.id !== e.id)
  }
}

function alarmType(e: AccessEvent): string {
  return (e.payload?.type as string) || e.kind || 'alarm'
}

function typeBadge(e: AccessEvent): string {
  const t = alarmType(e)
  if (e.kind === 'fire' || t === 'tamper_24h') return 'badge-warning'
  if (t === 'intrusion' || t === 'forced' || t === 'held') return 'badge-error'
  return 'badge-ghost'
}

function thing(e: AccessEvent): string {
  // Intrusion alarms name the tripped point in the payload; doors carry the portal.
  const point = e.payload?.point as string | undefined
  return point ? `${e.portal} · ${point}` : e.portal || e.location || '—'
}

onMounted(() => {
  load()
  subscribe()
})
onBeforeUnmount(() => {
  if (unsub) unsub()
})
</script>

<template>
  <ListLayout
    title="Alarm Console"
    subtitle="Unacknowledged alarms — forced/held doors, intrusion trips, and fire input (last 7 days)."
    :loading="loading"
    :error="error"
    :is-empty="alarms.length === 0"
    :has-query="false"
    empty-icon="✅"
    empty-title="All clear"
    empty-message="No unacknowledged alarms."
    error-title="Failed to load alarms"
    @retry="load"
  >
    <template #actions>
      <button class="btn btn-ghost btn-sm" @click="load" :disabled="loading">Refresh</button>
    </template>

    <BaseCard :no-padding="true">
      <ul class="divide-y divide-base-200">
        <li v-for="e in alarms" :key="e.id" class="flex items-center justify-between gap-3 p-4">
          <div class="flex items-center gap-3 min-w-0">
            <span class="badge" :class="typeBadge(e)">{{ formatConstant(alarmType(e)) }}</span>
            <div class="min-w-0">
              <div class="font-medium truncate">
                <code class="text-sm">{{ thing(e) }}</code>
                <span class="opacity-50 text-xs ml-2">{{ e.location }}</span>
              </div>
              <div class="text-xs opacity-50">{{ formatDate(e.ts || e.created, 'PP p') }}</div>
            </div>
          </div>
          <button
            class="btn btn-sm btn-primary"
            :disabled="acking || !canCommand"
            :title="canCommand ? 'Acknowledge' : 'Requires the command capability'"
            @click="acknowledge(e)"
          >
            Ack
          </button>
        </li>
      </ul>
    </BaseCard>
  </ListLayout>
</template>

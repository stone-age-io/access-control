<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { usePagination } from '@/composables/usePagination'
import { useAuthStore } from '@/stores/auth'
import type { BaseRecord, Cardholder, Credential } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

/**
 * Visitor passes: the `visitor` records of the badge auth collection, with each
 * one's current credential window.
 *
 * Validity lives on the CREDENTIAL, not on the badge login, so it is fetched
 * separately per cardholder rather than being a column on the badge record — one
 * source of truth for expiry, which is the same field the edge enforces.
 */
interface BadgeUserRecord extends BaseRecord {
  email: string
  kind: string
  cardholder: string
  expand?: { cardholder?: Cardholder }
}

const toast = useToast()
const { confirm } = useConfirm()
const auth = useAuthStore()

const { items: badges, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<BadgeUserRecord>('badge_users', 50)

// cardholder id → its most relevant credential window.
const windows = ref<Record<string, { until: string; status: string }>>({})
const deleting = ref(false)

const canManage = computed(() => auth.can('enroll'))

const columns: Column<BadgeUserRecord>[] = [
  { key: 'name', label: 'Visitor' },
  { key: 'email', label: 'Email' },
  { key: 'validUntil', label: 'Valid until' },
  { key: 'state', label: 'State' },
  { key: 'actions', label: '' },
]

function queryOpts() {
  return { filter: 'kind = "visitor"', sort: '-created', expand: 'cardholder' }
}

async function reload() {
  page.value = 1
  await load(queryOpts())
  await loadWindows()
}

/**
 * Fetch each visitor's credential window. Batched into one filtered query rather
 * than one request per row.
 */
async function loadWindows() {
  const ids = badges.value.map((b) => b.cardholder).filter(Boolean)
  if (!ids.length) {
    windows.value = {}
    return
  }
  try {
    const filter = ids.map((id) => `user = "${id}"`).join(' || ')
    const creds = await pb.collection('credentials').getFullList<Credential>({
      filter,
      sort: '-created',
    })
    const next: Record<string, { until: string; status: string }> = {}
    for (const c of creds) {
      // -created sort means the first one seen per cardholder is the newest.
      if (!next[c.user]) next[c.user] = { until: c.valid_until || '', status: c.status || 'active' }
    }
    windows.value = next
  } catch {
    windows.value = {} // the list is still useful without the windows
  }
}

function untilLabel(b: BadgeUserRecord): string {
  const w = windows.value[b.cardholder]
  if (!w?.until) return '—'
  const d = new Date(w.until)
  return isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

/** Expired / revoked / active, derived from the credential, never from the login. */
function state(b: BadgeUserRecord): { label: string; tone: 'success' | 'warning' | 'neutral' } {
  const w = windows.value[b.cardholder]
  if (!w) return { label: 'no pass', tone: 'neutral' }
  if (w.status === 'revoked') return { label: 'revoked', tone: 'neutral' }
  if (w.status === 'suspended') return { label: 'suspended', tone: 'warning' }
  if (w.until && new Date(w.until).getTime() < Date.now()) return { label: 'expired', tone: 'warning' }
  return { label: 'active', tone: 'success' }
}

async function remove(b: BadgeUserRecord) {
  const ok = await confirm({
    title: 'Delete visitor login?',
    message: `Remove the badge login for ${b.email}?`,
    details:
      'Their credential is NOT revoked by this — revoke it on the credential itself if the pass should stop working. This only removes their ability to view the badge.',
    confirmText: 'Delete login',
    variant: 'warning',
  })
  if (!ok) return
  deleting.value = true
  try {
    await pb.collection('badge_users').delete(b.id)
    toast.success('Visitor login deleted')
    await reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete visitor login')
  } finally {
    deleting.value = false
  }
}

onMounted(reload)
</script>

<template>
  <ListLayout
    title="Visitors"
    subtitle="Time-bound passes issued to guests and contractors."
    :loading="loading"
    :error="error"
    :is-empty="badges.length === 0"
    empty-icon="👋"
    empty-title="No visitor passes"
    empty-message="Issue a time-bound pass to a guest or contractor. Their access expires on its own."
    error-title="Failed to load visitors"
    @retry="reload"
  >
    <template #actions>
      <router-link v-if="canManage" to="/visitors/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Visitor Pass</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link v-if="canManage" to="/visitors/new" class="btn btn-primary">New Visitor Pass</router-link>
    </template>

    <BaseCard :no-padding="true">
      <ResponsiveList :items="badges" :columns="columns" :loading="loading">
        <template #cell-name="{ item }">
          <span class="font-medium">{{ item.expand?.cardholder?.name || '—' }}</span>
        </template>
        <template #card-name="{ item }">
          <span class="text-sm font-bold text-primary truncate">{{ item.expand?.cardholder?.name || '—' }}</span>
        </template>

        <template #cell-validUntil="{ item }">
          <span class="text-sm">{{ untilLabel(item) }}</span>
        </template>
        <template #card-validUntil="{ item }">
          <span class="text-sm">{{ untilLabel(item) }}</span>
        </template>

        <template #cell-state="{ item }">
          <SoftBadge :tone="state(item).tone" dot>{{ state(item).label }}</SoftBadge>
        </template>
        <template #card-state="{ item }">
          <SoftBadge :tone="state(item).tone" dot>{{ state(item).label }}</SoftBadge>
        </template>

        <template #cell-actions="{ item }">
          <button
            v-if="canManage"
            class="btn btn-ghost btn-xs text-error"
            :disabled="deleting"
            @click.stop="remove(item)"
          >
            Delete
          </button>
        </template>
      </ResponsiveList>

      <ListPagination
        :page="page"
        :total-pages="totalPages"
        :loading="loading"
        @prev="prevPage(queryOpts())"
        @next="nextPage(queryOpts())"
      >
        {{ badges.length }} of {{ totalItems }} visitor pass(es)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { usePagination } from '@/composables/usePagination'
import { useAuthStore } from '@/stores/auth'
import type { Cardholder, Credential } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

/**
 * Visitor passes: the cardholders with `kind = "visitor"`, each with its current
 * credential window.
 *
 * A visitor is not a different kind of THING from a cardholder — it is a cardholder with
 * a time-bound pass — so this page is a filtered view of the same collection the
 * Cardholders page lists, and `kind` is the only thing separating them. It used to join
 * a separate `badge_users` login to its cardholder through a relation and an expand,
 * which is why every row needed two records to render.
 *
 * Validity lives on the CREDENTIAL, not on the person, so it is fetched separately —
 * one source of truth for expiry, and the same field the edge enforces.
 */

const toast = useToast()
const { confirm } = useConfirm()
const auth = useAuthStore()

const { items: badges, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Cardholder>('cardholders', 50)

// cardholder id → its most relevant credential window.
const windows = ref<Record<string, { until: string; status: string }>>({})
const working = ref(false)

const canManage = computed(() => auth.can('enroll'))

const columns: Column<Cardholder>[] = [
  { key: 'name', label: 'Visitor' },
  { key: 'email', label: 'Email' },
  { key: 'validUntil', label: 'Valid until' },
  { key: 'state', label: 'State' },
  { key: 'actions', label: '' },
]

function queryOpts() {
  return { filter: 'kind = "visitor"', sort: '-created' }
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
  const ids = badges.value.map((b) => b.id).filter(Boolean)
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

function untilLabel(b: Cardholder): string {
  const w = windows.value[b.id]
  if (!w?.until) return '—'
  const d = new Date(w.until)
  return isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

/** Expired / revoked / active, derived from the credential, never from the login. */
function state(b: Cardholder): { label: string; tone: 'success' | 'warning' | 'neutral' } {
  const w = windows.value[b.id]
  if (!w) return { label: 'no pass', tone: 'neutral' }
  if (w.status === 'revoked') return { label: 'revoked', tone: 'neutral' }
  if (w.status === 'suspended') return { label: 'suspended', tone: 'warning' }
  if (w.until && new Date(w.until).getTime() < Date.now()) return { label: 'expired', tone: 'warning' }
  return { label: 'active', tone: 'success' }
}

/**
 * End the visit early: revokes the credential, keeps the person.
 *
 * This is the primary action on this page. Keeping the record keeps the fact that they
 * were here, and lets a repeat visit refresh the same person rather than duplicating
 * them — which is why revoke exists alongside delete rather than instead of it.
 */
async function revoke(b: Cardholder) {
  const ok = await confirm({
    title: 'Revoke this pass?',
    message: `End the visit for ${b.name || b.email}?`,
    details:
      'Their pass stops opening doors immediately, including their QR code. They keep their sign-in, and their badge will say the pass is no longer valid.',
    confirmText: 'Revoke pass',
    variant: 'warning',
  })
  if (!ok) return
  working.value = true
  try {
    await pb.send(`/api/badge/visitors/${b.id}/revoke`, { method: 'POST' })
    toast.success('Visitor pass revoked')
    await reload()
  } catch (err: any) {
    toast.error(err?.response?.message || err?.message || 'Failed to revoke the pass')
  } finally {
    working.value = false
  }
}

/**
 * Remove the person entirely, pass included: `credentials.user` cascades (migration
 * 1750000036), so there is no way for this to leave a working credential behind — which
 * is what a delete used to do when the login was a separate record it could remove on
 * its own.
 *
 * Deliberately the secondary action. Deleting a visitor destroys the record that they
 * visited at all, and an operator reaching for a button on this page almost always means
 * "stop them getting in", not "erase the visit".
 */
async function remove(b: Cardholder) {
  const ok = await confirm({
    title: 'Delete this visitor?',
    message: `Delete ${b.name || b.email}?`,
    details:
      'Their pass and their sign-in are deleted with them, and the record that they visited is gone. Revoke instead if they may return.',
    confirmText: 'Delete visitor',
    variant: 'danger',
  })
  if (!ok) return
  working.value = true
  try {
    await pb.collection('cardholders').delete(b.id)
    toast.success('Visitor deleted')
    await reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete the visitor')
  } finally {
    working.value = false
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
          <span class="font-medium">{{ item.name || '—' }}</span>
        </template>
        <template #card-name="{ item }">
          <span class="text-sm font-bold text-primary truncate">{{ item.name || '—' }}</span>
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
          <div v-if="canManage" class="flex justify-end gap-1">
            <!-- Revoke is the common case, so it leads and Delete stays quieter. -->
            <button
              v-if="state(item).label === 'active'"
              class="btn btn-ghost btn-xs"
              :disabled="working"
              @click.stop="revoke(item)"
            >
              Revoke
            </button>
            <button class="btn btn-ghost btn-xs text-error" :disabled="working" @click.stop="remove(item)">
              Delete
            </button>
          </div>
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

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
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
import { visitorPassState, VISITOR_STATES, type VisitorStateFilter } from './visitorState'

/**
 * Visitor passes: the cardholders with `kind = "visitor"`, each with its current
 * credential window.
 *
 * A visitor is not a different kind of THING from a cardholder — it is a cardholder with
 * a time-bound pass — so this page is a filtered view of the same collection the
 * Cardholders page lists, and `kind` is the only thing separating them.
 *
 * Validity lives on the CREDENTIAL, not on the person, so it is fetched separately —
 * one source of truth for expiry, and the same field the edge enforces. The state itself is
 * derived by the shared `visitorPassState`, so this page and the detail page cannot describe
 * the same visit differently.
 */

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()
const auth = useAuthStore()

const { items: badges, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Cardholder>('cardholders', 50)

// cardholder id → every credential of theirs, so the shared state helper sees what the
// detail page sees.
const creds = ref<Record<string, Credential[]>>({})
const working = ref(false)
const searchQuery = ref('')
const stateFilter = ref<VisitorStateFilter | ''>('')

const canManage = computed(() => auth.can('enroll'))

const columns: Column<Cardholder>[] = [
  { key: 'name', label: 'Visitor' },
  { key: 'email', label: 'Email' },
  { key: 'validUntil', label: 'Valid until' },
  { key: 'state', label: 'State' },
  { key: 'actions', label: '' },
]

/**
 * Name/email search is server-side; the state filter is not, and cannot sensibly be: a
 * visitor's state comes from their NEWEST credential's window, which is a row in another
 * collection, so expressing it as a PocketBase filter would either need a back-relation
 * whose conditions can each match a different credential, or a second round trip that
 * breaks pagination.
 *
 * Filtering the loaded page instead is honest here because the sort is `-created`: a
 * page of the 50 most recent visits is where every live and recently-lapsed pass is. The
 * footer says how many are shown against how many exist, so a narrowed list never reads as
 * the whole truth.
 */
function queryOpts() {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  const search = q ? ` && (name ~ "${q}" || email ~ "${q}")` : ''
  return { filter: `kind = "visitor"${search}`, sort: '-created' }
}

async function reload() {
  page.value = 1
  await load(queryOpts())
  await loadWindows()
}

async function turnPage(next: boolean) {
  await (next ? nextPage(queryOpts()) : prevPage(queryOpts()))
  await loadWindows()
}

/**
 * Fetch every visitor's credentials. Batched into one filtered query rather than one
 * request per row.
 */
async function loadWindows() {
  const ids = badges.value.map((b) => b.id).filter(Boolean)
  if (!ids.length) {
    creds.value = {}
    return
  }
  try {
    const filter = ids.map((id) => `user = "${id}"`).join(' || ')
    const rows = await pb.collection('credentials').getFullList<Credential>({
      filter,
      sort: '-created',
    })
    const next: Record<string, Credential[]> = {}
    for (const c of rows) {
      const bucket = next[c.user]
      if (bucket) bucket.push(c)
      else next[c.user] = [c]
    }
    creds.value = next
  } catch {
    creds.value = {} // the list is still useful without the windows
  }
}

/**
 * State per row, computed once. The template asks for it several times per row (the badge,
 * its tone, the Revoke gate), and each derivation sorts that visitor's credentials — so
 * calling straight through would re-sort every row's cards on every render.
 */
const passes = computed<Record<string, ReturnType<typeof visitorPassState>>>(() => {
  const out: Record<string, ReturnType<typeof visitorPassState>> = {}
  for (const b of badges.value) out[b.id] = visitorPassState(b, creds.value[b.id] || [])
  return out
})

function pass(b: Cardholder) {
  return passes.value[b.id] ?? visitorPassState(b, creds.value[b.id] || [])
}

function untilLabel(b: Cardholder): string {
  const until = pass(b).credential?.valid_until
  if (!until) return '—'
  const d = new Date(until)
  return isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

const visible = computed<Cardholder[]>(() => {
  if (!stateFilter.value) return badges.value
  return badges.value.filter((b) => pass(b).label === stateFilter.value)
})

/** Counts per state on this page, so the filter chips say what they will show. */
const stateCounts = computed<Record<string, number>>(() => {
  const out: Record<string, number> = {}
  for (const b of badges.value) {
    const label = pass(b).label
    out[label] = (out[label] || 0) + 1
  }
  return out
})

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
 * 1750000036), so there is no way for this to leave a working credential behind.
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

watchDebounced(searchQuery, reload, { debounce: 300 })
onMounted(reload)
</script>

<template>
  <ListLayout
    v-model:search="searchQuery"
    title="Visitors"
    subtitle="Time-bound passes issued to guests and contractors."
    search-placeholder="Search by name or email..."
    :loading="loading"
    :error="error"
    :is-empty="badges.length === 0"
    :has-query="!!searchQuery"
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

    <!-- State chips. Each carries its count on this page, so choosing one is never a
         guess at whether it will show anything. -->
    <template #toolbar>
      <div class="flex flex-wrap items-center gap-1">
        <button
          type="button"
          class="btn btn-sm"
          :class="stateFilter === '' ? 'btn-primary' : 'btn-ghost'"
          @click="stateFilter = ''"
        >
          All <span class="opacity-60">{{ badges.length }}</span>
        </button>
        <button
          v-for="s in VISITOR_STATES"
          :key="s"
          type="button"
          class="btn btn-sm"
          :class="stateFilter === s ? 'btn-primary' : 'btn-ghost'"
          :disabled="!stateCounts[s]"
          @click="stateFilter = s"
        >
          {{ s }} <span class="opacity-60">{{ stateCounts[s] || 0 }}</span>
        </button>
      </div>
    </template>

    <BaseCard :no-padding="true">
      <ResponsiveList
        :items="visible"
        :columns="columns"
        :loading="loading"
        @row-click="(b) => router.push(`/visitors/${b.id}`)"
      >
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
          <SoftBadge :tone="pass(item).tone" dot>{{ pass(item).label }}</SoftBadge>
        </template>
        <template #card-state="{ item }">
          <SoftBadge :tone="pass(item).tone" dot>{{ pass(item).label }}</SoftBadge>
        </template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 py-2 text-center opacity-60">
            <span class="text-4xl">🔍</span>
            <span class="text-sm">
              No visitor passes<template v-if="stateFilter"> are “{{ stateFilter }}”</template
              ><template v-if="searchQuery"> matching “{{ searchQuery }}”</template>.
            </span>
          </div>
        </template>

        <template #cell-actions="{ item }">
          <div v-if="canManage" class="flex justify-end gap-1">
            <!-- Revoke is the common case, so it leads and Delete stays quieter. -->
            <button
              v-if="pass(item).label === 'active'"
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
        @prev="turnPage(false)"
        @next="turnPage(true)"
      >
        <!-- Both numbers, always: a state filter narrows the page, and a footer showing only
             the narrowed count would read as the whole picture. -->
        {{ visible.length }} shown of {{ totalItems }} visitor pass(es)
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>

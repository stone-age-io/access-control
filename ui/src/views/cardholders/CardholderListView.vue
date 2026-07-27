<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { watchDebounced } from '@vueuse/core'
import { usePagination } from '@/composables/usePagination'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { useFileUrl } from '@/composables/useFileUrl'
import { useAuthStore } from '@/stores/auth'
import { pb } from '@/utils/pb'
import { visitorPassState, VISITOR_STATES, type VisitorStateFilter } from '@/utils/visitorPass'
import type { Cardholder, Credential, Role } from '@/types/pocketbase'
import type { Column } from '@/components/ui/ResponsiveList.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import ResponsiveList from '@/components/ui/ResponsiveList.vue'
import ListLayout from '@/components/ui/ListLayout.vue'
import ListPagination from '@/components/ui/ListPagination.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import Avatar from '@/components/ui/Avatar.vue'

/**
 * Every person in the system — staff, contractors, and visitors — on one page.
 *
 * # Why one page with a filter, and not two pages
 *
 * A visitor is not a different kind of THING: it is a cardholder with `kind = "visitor"` and
 * a time-bound pass. Two pages over one collection meant an operator searching here for
 * someone who happens to be a visitor got nothing, with no hint that another list existed.
 * So `kind` is a filter, and search deliberately spans both (see `otherKindMatches`).
 *
 * # Why the default is Permanent, not All
 *
 * A lobby's worth of guests interleaved alphabetically into the staff roster is exactly what
 * separate pages were protecting against, and that concern was right even though the split
 * was not. The filter defaults to Permanent and is remembered per browser, so the roster
 * stays a roster and a front desk that lives in Visitors stays there.
 *
 * # Why the pass-state chips only exist under Visitors
 *
 * A pass state is derived from the newest of a person's credentials — a row in another
 * collection — so it cannot be a PocketBase filter and is applied to the loaded page instead.
 * That is honest under Visitors, where the sort is `-created` and 50 rows covers every live
 * and recently-lapsed visit. It would be a lie on a name-sorted roster of thousands, where
 * "expired · 3" means "3 among the A's". So the chips, and the credentials query that feeds
 * them, exist only in the mode where they tell the truth.
 */

type KindFilter = 'permanent' | 'visitor' | 'all'

const KIND_FILTERS: { value: KindFilter; label: string }[] = [
  // "Permanent" rather than "Staff": the population includes contractors, residents, and
  // students, and the axis is really how long the pass lasts.
  { value: 'permanent', label: 'Permanent' },
  { value: 'visitor', label: 'Visitors' },
  { value: 'all', label: 'All' },
]

const FILTER_STORAGE_KEY = 'sa.cardholders.kind'

const router = useRouter()
const toast = useToast()
const { confirm } = useConfirm()
const auth = useAuthStore()
// cardholders.photo is a protected file — its URL needs a session file token.
const { url: fileUrl } = useFileUrl()

const { items: cardholders, page, totalPages, totalItems, loading, error, load, nextPage, prevPage } =
  usePagination<Cardholder>('cardholders', 50)

const searchQuery = ref('')
const working = ref(false)
/** cardholder id → their credentials. Populated only in Visitors mode. */
const creds = ref<Record<string, Credential[]>>({})
const stateFilter = ref<VisitorStateFilter | ''>('')
/** How many rows match the search under the OTHER kinds, so a filter never hides a hit. */
const otherKindMatches = ref(0)

function storedKind(): KindFilter {
  const raw = localStorage.getItem(FILTER_STORAGE_KEY)
  return KIND_FILTERS.some((k) => k.value === raw) ? (raw as KindFilter) : 'permanent'
}
const kindFilter = ref<KindFilter>(storedKind())

const canManage = computed(() => auth.can('enroll'))
const isVisitorMode = computed(() => kindFilter.value === 'visitor')

/**
 * The `kind` clause. Written as `kind != "visitor"` rather than a list of wanted kinds,
 * because blank `kind` means an ordinary cardholder (migration 1750000000) — so the only
 * value that has to be set correctly is `visitor`.
 */
function kindClause(k: KindFilter): string {
  if (k === 'visitor') return 'kind = "visitor"'
  if (k === 'permanent') return 'kind != "visitor"'
  return ''
}

function searchClause(): string {
  const q = searchQuery.value.trim().replace(/["\\]/g, '')
  if (!q) return ''
  return `(name ~ "${q}" || email ~ "${q}" || external_id ~ "${q}")`
}

function joined(...clauses: string[]): string {
  return clauses.filter(Boolean).join(' && ')
}

function queryOpts() {
  return {
    // Visitors sort newest-first: the question there is "who is here now", and a page of
    // the 50 most recent visits answers it. A roster sorts by name.
    sort: isVisitorMode.value ? '-created' : 'name',
    expand: 'roles',
    filter: joined(kindClause(kindFilter.value), searchClause()),
  }
}

const columns = computed<Column<Cardholder>[]>(() =>
  isVisitorMode.value
    ? [
        { key: 'name', label: 'Visitor' },
        { key: 'email', label: 'Email' },
        { key: 'validUntil', label: 'Valid until' },
        { key: 'state', label: 'Pass' },
      ]
    : [
        { key: 'name', label: 'Name' },
        { key: 'email', label: 'Email' },
        { key: 'status', label: 'Status' },
        { key: 'roles', label: 'Roles' },
        { key: 'external_id', label: 'External ID' },
      ],
)

async function reload() {
  page.value = 1
  await load(queryOpts())
  await Promise.all([loadWindows(), countOtherKinds()])
}

async function turnPage(next: boolean) {
  await (next ? nextPage(queryOpts()) : prevPage(queryOpts()))
  await loadWindows()
}

function selectKind(k: KindFilter) {
  if (kindFilter.value === k) return
  kindFilter.value = k
  localStorage.setItem(FILTER_STORAGE_KEY, k)
  // A pass state only exists under Visitors; leaving one set would silently narrow the
  // roster by a filter whose chips are no longer on screen.
  stateFilter.value = ''
  reload()
}

/**
 * Fetch the loaded visitors' credentials, batched into one filtered query rather than one
 * request per row. Skipped entirely outside Visitors mode — a staff roster does not show
 * pass state, so it must not pay for it.
 */
async function loadWindows() {
  if (!isVisitorMode.value) {
    creds.value = {}
    return
  }
  const ids = cardholders.value.map((b) => b.id).filter(Boolean)
  if (!ids.length) {
    creds.value = {}
    return
  }
  try {
    const filter = ids.map((id) => `user = "${id}"`).join(' || ')
    const rows = await pb.collection('credentials').getFullList<Credential>({ filter, sort: '-created' })
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
 * How many people the current search matches under the kinds the filter is hiding.
 *
 * This is the reason the merge is worth doing rather than merely tidier: before it, an
 * operator searching for a name that belongs to a visitor got an empty roster and no
 * indication that the person existed. One `perPage: 1` request, only while searching.
 */
async function countOtherKinds() {
  const search = searchClause()
  if (!search || kindFilter.value === 'all') {
    otherKindMatches.value = 0
    return
  }
  const complement = kindFilter.value === 'visitor' ? 'kind != "visitor"' : 'kind = "visitor"'
  try {
    const res = await pb.collection('cardholders').getList(1, 1, { filter: joined(complement, search) })
    otherKindMatches.value = res.totalItems
  } catch {
    otherKindMatches.value = 0 // a missing hint is better than a wrong one
  }
}

/**
 * Pass state per row, computed once. The template asks for it several times per row (the
 * badge, its tone, the Revoke gate), and each derivation sorts that person's credentials — so
 * calling straight through would re-sort every row's cards on every render.
 */
const passes = computed<Record<string, ReturnType<typeof visitorPassState>>>(() => {
  const out: Record<string, ReturnType<typeof visitorPassState>> = {}
  if (!isVisitorMode.value) return out
  for (const b of cardholders.value) out[b.id] = visitorPassState(b, creds.value[b.id] || [])
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
  if (!isVisitorMode.value || !stateFilter.value) return cardholders.value
  return cardholders.value.filter((b) => pass(b).label === stateFilter.value)
})

/** Counts per state on this page, so the filter chips say what they will show. */
const stateCounts = computed<Record<string, number>>(() => {
  const out: Record<string, number> = {}
  if (!isVisitorMode.value) return out
  for (const b of cardholders.value) {
    const label = pass(b).label
    out[label] = (out[label] || 0) + 1
  }
  return out
})

function rolesOf(c: Cardholder): Role[] {
  return c.expand?.roles || []
}

function isVisitor(c: Cardholder): boolean {
  return c.kind === 'visitor'
}

/**
 * End the visit early: revokes the credential, keeps the person.
 *
 * Keeping the record keeps the fact that they were here, and lets a repeat visit refresh the
 * same person rather than duplicating them — which is why revoke exists alongside delete
 * rather than instead of it.
 */
async function revoke(c: Cardholder) {
  const ok = await confirm({
    title: 'Revoke this pass?',
    message: `End the visit for ${c.name || c.email}?`,
    details:
      'Their pass stops opening doors immediately, including their QR code. They keep their sign-in, and their badge will say the pass is no longer valid.',
    confirmText: 'Revoke pass',
    variant: 'warning',
  })
  if (!ok) return
  working.value = true
  try {
    await pb.send(`/api/badge/visitors/${c.id}/revoke`, { method: 'POST' })
    toast.success('Visitor pass revoked')
    await reload()
  } catch (err: any) {
    toast.error(err?.response?.message || err?.message || 'Failed to revoke the pass')
  } finally {
    working.value = false
  }
}

/**
 * Remove the person entirely, credentials included: `credentials.user` cascades (migration
 * 1750000036), so this cannot leave a working credential behind.
 */
async function handleDelete(c: Cardholder) {
  const visitor = isVisitor(c)
  const confirmed = await confirm({
    title: visitor ? 'Delete this visitor?' : 'Delete Cardholder',
    message: visitor ? `Delete ${c.name || c.email}?` : `Delete cardholder "${c.name || c.email}"?`,
    details: visitor
      ? 'Their pass and their sign-in are deleted with them, and the record that they visited is gone. Revoke instead if they may return.'
      : 'Their credentials are deleted with them and will stop opening doors. This cannot be undone.',
    confirmText: visitor ? 'Delete visitor' : 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  working.value = true
  try {
    await pb.collection('cardholders').delete(c.id)
    toast.success(visitor ? 'Visitor deleted' : 'Cardholder deleted')
    await reload()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete')
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
    title="Cardholders"
    :subtitle="
      isVisitorMode
        ? 'Time-bound passes issued to guests and contractors.'
        : 'People who hold credentials — staff, contractors, and visitors.'
    "
    search-placeholder="Search by name, email, or external ID..."
    :loading="loading"
    :error="error"
    :is-empty="cardholders.length === 0"
    :has-query="!!searchQuery"
    :empty-icon="isVisitorMode ? '👋' : '🪪'"
    :empty-title="isVisitorMode ? 'No visitor passes' : 'No cardholders yet'"
    :empty-message="
      isVisitorMode
        ? 'Issue a time-bound pass to a guest or contractor. Their access expires on its own.'
        : 'Add the people who hold credentials, then assign them roles.'
    "
    error-title="Failed to load cardholders"
    @retry="reload"
  >
    <template #actions>
      <router-link
        v-if="canManage"
        to="/visitors/new"
        class="btn btn-outline w-full sm:w-auto"
      >
        <span class="text-lg">+</span><span>Visitor Pass</span>
      </router-link>
      <router-link to="/cardholders/new" class="btn btn-primary w-full sm:w-auto">
        <span class="text-lg">+</span><span>New Cardholder</span>
      </router-link>
    </template>
    <template #empty-action>
      <router-link v-if="isVisitorMode && canManage" to="/visitors/new" class="btn btn-primary">
        New Visitor Pass
      </router-link>
      <router-link v-else-if="!isVisitorMode" to="/cardholders/new" class="btn btn-primary">
        Create Cardholder
      </router-link>
    </template>

    <template #toolbar>
      <div class="flex flex-wrap items-center gap-3">
        <!-- Kind: a server-side filter on one column, so its counts are the real totals. -->
        <div class="join">
          <button
            v-for="k in KIND_FILTERS"
            :key="k.value"
            type="button"
            class="btn btn-sm join-item"
            :class="kindFilter === k.value ? 'btn-primary' : 'btn-ghost'"
            @click="selectKind(k.value)"
          >
            {{ k.label }}
          </button>
        </div>

        <!-- Pass state: derived from another collection, so it narrows the LOADED page only.
             Offered under Visitors alone, where `-created` makes that honest. Each chip
             carries its count, so choosing one is never a guess at whether it will show
             anything. -->
        <div v-if="isVisitorMode" class="flex flex-wrap items-center gap-1">
          <button
            type="button"
            class="btn btn-sm"
            :class="stateFilter === '' ? 'btn-primary' : 'btn-ghost'"
            @click="stateFilter = ''"
          >
            Any <span class="opacity-60">{{ cardholders.length }}</span>
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
      </div>
    </template>

    <!-- The hit the filter is hiding. Without this, searching for someone who turns out to
         be a visitor returns an empty roster and no reason to think they exist. -->
    <div v-if="otherKindMatches" class="alert py-2 text-sm">
      <span>
        {{ otherKindMatches }}
        {{ otherKindMatches === 1 ? 'more person matches' : 'more people match' }}
        “{{ searchQuery }}”
        {{ kindFilter === 'visitor' ? 'outside Visitors' : 'under Visitors' }}.
      </span>
      <button type="button" class="btn btn-sm btn-ghost" @click="selectKind('all')">Show all</button>
    </div>

    <BaseCard :no-padding="true">
      <ResponsiveList
        :items="visible"
        :columns="columns"
        :loading="loading"
        @row-click="(c) => router.push(`/cardholders/${c.id}`)"
      >
        <template #cell-name="{ item }">
          <div class="flex items-center gap-2.5">
            <Avatar :name="item.name" :seed="item.id" :src="fileUrl(item, item.photo, '100x100')" />
            <span class="font-medium">{{ item.name || 'Unnamed' }}</span>
            <!-- Only under All, where the two kinds are interleaved and the row would
                 otherwise not say which it is. -->
            <SoftBadge v-if="kindFilter === 'all' && isVisitor(item)" tone="info">visitor</SoftBadge>
          </div>
        </template>
        <template #card-name="{ item }">
          <div class="flex items-center gap-2 min-w-0">
            <Avatar :name="item.name" :seed="item.id" size="xs" :src="fileUrl(item, item.photo, '100x100')" />
            <span class="text-sm font-bold text-primary truncate">{{ item.name || 'Unnamed' }}</span>
            <SoftBadge v-if="kindFilter === 'all' && isVisitor(item)" tone="info">visitor</SoftBadge>
          </div>
        </template>

        <template #cell-status="{ item }">
          <SoftBadge :tone="item.status === 'active' ? 'success' : 'warning'" dot>{{ item.status || 'active' }}</SoftBadge>
        </template>
        <template #card-status="{ item }">
          <SoftBadge :tone="item.status === 'active' ? 'success' : 'warning'" dot>{{ item.status || 'active' }}</SoftBadge>
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

        <template #cell-roles="{ item }">
          <div v-if="rolesOf(item).length" class="flex flex-wrap gap-1">
            <SoftBadge v-for="r in rolesOf(item).slice(0, 3)" :key="r.id" class="font-mono">{{ r.code }}</SoftBadge>
            <SoftBadge v-if="rolesOf(item).length > 3">+{{ rolesOf(item).length - 3 }}</SoftBadge>
          </div>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-roles="{ item }">
          <div v-if="rolesOf(item).length" class="flex flex-wrap gap-1 justify-end">
            <SoftBadge v-for="r in rolesOf(item).slice(0, 2)" :key="r.id" class="font-mono">{{ r.code }}</SoftBadge>
            <SoftBadge v-if="rolesOf(item).length > 2">+{{ rolesOf(item).length - 2 }}</SoftBadge>
          </div>
          <span v-else>-</span>
        </template>

        <template #cell-external_id="{ item }">
          <code v-if="item.external_id" class="text-xs">{{ item.external_id }}</code>
          <span v-else class="text-base-content/40">-</span>
        </template>
        <template #card-external_id="{ item }">
          <code v-if="item.external_id" class="text-xs">{{ item.external_id }}</code>
          <span v-else>-</span>
        </template>

        <template #empty>
          <div class="flex flex-col items-center gap-2 py-2 text-center opacity-60">
            <span class="text-4xl">🔍</span>
            <span class="text-sm">
              No {{ isVisitorMode ? 'visitor passes' : 'people' }}<template v-if="stateFilter">
                are “{{ stateFilter }}”</template
              ><template v-if="searchQuery"> matching “{{ searchQuery }}”</template>.
            </span>
          </div>
        </template>

        <template #actions="{ item }">
          <!-- Revoke leads for a live visit — it is the common case — and Delete stays
               quieter for everyone. -->
          <button
            v-if="canManage && isVisitor(item) && pass(item).label === 'active'"
            class="btn btn-xs"
            :disabled="working"
            @click.stop="revoke(item)"
          >
            Revoke
          </button>
          <router-link :to="`/cardholders/${item.id}/edit`" class="btn btn-xs">Edit</router-link>
          <button class="btn btn-xs text-error" :disabled="working" @click.stop="handleDelete(item)">Delete</button>
        </template>
      </ResponsiveList>

      <ListPagination :page="page" :total-pages="totalPages" :loading="loading" @prev="turnPage(false)" @next="turnPage(true)">
        <!-- Both numbers when a state filter is narrowing the page: a footer showing only
             the narrowed count would read as the whole picture. -->
        <template v-if="isVisitorMode && stateFilter">
          {{ visible.length }} shown of {{ totalItems }} visitor pass(es)
        </template>
        <template v-else>
          {{ visible.length }} of {{ totalItems }}
          {{ isVisitorMode ? 'visitor pass(es)' : 'cardholder(s)' }}
        </template>
      </ListPagination>
    </BaseCard>
  </ListLayout>
</template>

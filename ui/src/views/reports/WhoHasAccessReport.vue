<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { pb } from '@/utils/pb'
import { toCsv, downloadCsv, fileStamp, type CsvColumn } from '@/utils/csv'
import { useClientPagination } from '@/composables/useClientPagination'
import type { Cardholder, Role, AccessGroup, Portal, Schedule, Credential } from '@/types/pocketbase'
import BaseCard from '@/components/ui/BaseCard.vue'
import ListPagination from '@/components/ui/ListPagination.vue'

// The access matrix, walked over the policy graph exactly as policy.Decide reads
// it: cardholder → roles → access groups → (portals + one schedule). Each grant
// edge records the path (which access group, under which schedule). This is the
// *configured* grant, not a moment-in-time decision — it does not account for the
// schedule window being open right now, holidays, posture, or credential validity
// dates. The credential count flags the practical gap: access granted but no badge.

interface Edge {
  chId: string
  chName: string
  chStatus: string
  credCount: number
  portalId: string
  portalCode: string
  portalName: string
  location: string
  groupCode: string
  schedule: string
}

const loading = ref(false)
const error = ref('')
const edges = ref<Edge[]>([])
const cardholderCount = ref(0)
const portalCount = ref(0)

const pivot = ref<'cardholder' | 'portal'>('cardholder')
const search = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [cardholders, roles, groups, portals, schedules, creds] = await Promise.all([
      pb.collection('cardholders').getFullList<Cardholder>({ batch: 500, sort: 'name' }),
      pb.collection('roles').getFullList<Role>({ batch: 500 }),
      pb.collection('access_groups').getFullList<AccessGroup>({ batch: 500 }),
      pb.collection('portals').getFullList<Portal>({ batch: 500 }),
      pb.collection('schedules').getFullList<Schedule>({ batch: 500 }),
      pb.collection('credentials').getFullList<Credential>({ batch: 500 }),
    ])

    const roleById = new Map(roles.map((r) => [r.id, r]))
    const groupById = new Map(groups.map((g) => [g.id, g]))
    const portalById = new Map(portals.map((p) => [p.id, p]))
    const schedById = new Map(schedules.map((s) => [s.id, s]))

    const activeCreds = new Map<string, number>()
    for (const c of creds) {
      if (c.status === 'active') activeCreds.set(c.user, (activeCreds.get(c.user) || 0) + 1)
    }

    const out: Edge[] = []
    for (const ch of cardholders) {
      // Dedup portals reached via the same access group through different roles.
      const seen = new Set<string>()
      for (const roleId of ch.roles || []) {
        const role = roleById.get(roleId)
        if (!role) continue
        for (const groupId of role.access_groups || []) {
          const group = groupById.get(groupId)
          if (!group) continue
          const sched = schedById.get(group.schedule)
          for (const portalId of group.portals || []) {
            const key = `${groupId}|${portalId}`
            if (seen.has(key)) continue
            seen.add(key)
            const portal = portalById.get(portalId)
            if (!portal) continue
            out.push({
              chId: ch.id,
              chName: ch.name || ch.external_id || ch.id,
              chStatus: ch.status || '',
              credCount: activeCreds.get(ch.id) || 0,
              portalId,
              portalCode: portal.code,
              portalName: portal.name || portal.code,
              location: portal.location,
              groupCode: group.code,
              schedule: sched ? sched.name || sched.code : group.schedule || '—',
            })
          }
        }
      }
    }
    edges.value = out
    cardholderCount.value = cardholders.length
    portalCount.value = portals.length
  } catch (err: any) {
    error.value = err?.message || 'Failed to build the access matrix'
    edges.value = []
  } finally {
    loading.value = false
  }
}

const filtered = computed(() => {
  const q = search.value.toLowerCase().trim()
  if (!q) return edges.value
  return edges.value.filter((e) =>
    e.chName.toLowerCase().includes(q) ||
    e.portalCode.toLowerCase().includes(q) ||
    e.portalName.toLowerCase().includes(q) ||
    e.location.toLowerCase().includes(q) ||
    e.groupCode.toLowerCase().includes(q),
  )
})

interface Group {
  id: string
  title: string
  meta: string
  status?: string
  rows: { a: string; b: string; c: string }[]
}

// Group the filtered edges by the chosen pivot. By cardholder: each person and the
// portals they can reach. By portal: each door and who can reach it.
const grouped = computed<Group[]>(() => {
  const map = new Map<string, Group>()
  for (const e of filtered.value) {
    if (pivot.value === 'cardholder') {
      let g = map.get(e.chId)
      if (!g) {
        g = { id: e.chId, title: e.chName, meta: `${e.credCount} active credential${e.credCount === 1 ? '' : 's'}`, status: e.chStatus, rows: [] }
        map.set(e.chId, g)
      }
      g.rows.push({ a: `${e.portalName}`, b: e.location, c: `${e.groupCode} · ${e.schedule}` })
    } else {
      let g = map.get(e.portalId)
      if (!g) {
        g = { id: e.portalId, title: e.portalName, meta: `${e.portalCode} · ${e.location}`, rows: [] }
        map.set(e.portalId, g)
      }
      g.rows.push({ a: e.chName, b: e.chStatus || 'active', c: `${e.groupCode} · ${e.schedule}` })
    }
  }
  const groups = [...map.values()]
  groups.sort((x, y) => x.title.localeCompare(y.title))
  return groups
})

// Paginate the grouped rows (cardholders or portals); the coverage-gap counts
// above are still computed over the whole edge set, not the page.
const { page, totalPages, pageItems, next, prev } = useClientPagination(grouped, 25)

const rowHeads = computed(() =>
  pivot.value === 'cardholder'
    ? ['Portal', 'Location', 'Via (group · schedule)']
    : ['Cardholder', 'Status', 'Via (group · schedule)'],
)

// Coverage gaps worth surfacing: people with access to nothing, doors nobody can reach.
const noAccess = computed(() => cardholderCount.value - new Set(edges.value.map((e) => e.chId)).size)
const noGrant = computed(() => portalCount.value - new Set(edges.value.map((e) => e.portalId)).size)

interface MatrixRow {
  cardholder: string
  status: string
  active_credentials: number
  portal_code: string
  portal_name: string
  location: string
  access_group: string
  schedule: string
}

const EXPORT_COLUMNS: CsvColumn<MatrixRow>[] = [
  { key: 'cardholder', label: 'Cardholder' },
  { key: 'status', label: 'Status' },
  { key: 'active_credentials', label: 'Active Credentials' },
  { key: 'portal_code', label: 'Portal Code' },
  { key: 'portal_name', label: 'Portal Name' },
  { key: 'location', label: 'Location' },
  { key: 'access_group', label: 'Access Group' },
  { key: 'schedule', label: 'Schedule' },
]

function exportCsv() {
  const rows: MatrixRow[] = filtered.value.map((e) => ({
    cardholder: e.chName,
    status: e.chStatus || 'active',
    active_credentials: e.credCount,
    portal_code: e.portalCode,
    portal_name: e.portalName,
    location: e.location,
    access_group: e.groupCode,
    schedule: e.schedule,
  }))
  downloadCsv(`who-has-access-${fileStamp()}.csv`, toCsv(rows, EXPORT_COLUMNS))
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <!-- Controls -->
    <div class="flex flex-col sm:flex-row sm:flex-wrap gap-3">
      <div role="tablist" class="tabs tabs-boxed">
        <button role="tab" class="tab" :class="{ 'tab-active': pivot === 'cardholder' }" @click="pivot = 'cardholder'">By cardholder</button>
        <button role="tab" class="tab" :class="{ 'tab-active': pivot === 'portal' }" @click="pivot = 'portal'">By portal</button>
      </div>
      <label class="input input-bordered flex items-center gap-2 flex-1 min-h-[3rem]">
        <span class="text-xs opacity-60 shrink-0">🔎</span>
        <input v-model="search" type="text" class="grow" placeholder="Filter by cardholder, portal, location, access group..." />
      </label>
      <button class="btn btn-outline w-full sm:w-auto" :disabled="loading || filtered.length === 0" @click="exportCsv">Export CSV</button>
    </div>

    <div v-if="error" class="alert alert-error">
      <span>{{ error }}</span>
      <button class="btn btn-ghost btn-xs" @click="load">Retry</button>
    </div>

    <div v-if="loading" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <template v-else>
      <!-- Coverage gaps -->
      <div class="flex flex-wrap gap-2 text-sm">
        <span class="badge badge-ghost gap-1">{{ new Set(edges.map((e) => e.chId)).size }} cardholders with access</span>
        <span v-if="noAccess > 0" class="badge badge-warning gap-1">{{ noAccess }} with no access</span>
        <span v-if="noGrant > 0" class="badge badge-warning gap-1">{{ noGrant }} portals nobody can reach</span>
      </div>

      <div v-if="grouped.length === 0" class="text-center py-12 opacity-50">
        <span class="text-3xl">🔑</span>
        <p class="text-sm mt-2">No access grants{{ search ? ' match this filter' : ' configured yet' }}.</p>
      </div>

      <BaseCard v-for="g in pageItems" :key="g.id" :no-padding="true">
        <div class="flex items-center justify-between gap-3 px-4 py-3 border-b border-base-200">
          <div class="min-w-0">
            <div class="font-semibold truncate">{{ g.title }}</div>
            <div class="text-xs opacity-50 truncate">{{ g.meta }}</div>
          </div>
          <span v-if="g.status && g.status !== 'active'" class="badge badge-sm badge-warning">{{ g.status }}</span>
          <span class="badge badge-sm tabular-nums shrink-0">{{ g.rows.length }}</span>
        </div>
        <div class="overflow-x-auto">
          <table class="table table-sm">
            <thead>
              <tr><th v-for="h in rowHeads" :key="h">{{ h }}</th></tr>
            </thead>
            <tbody>
              <tr v-for="(r, i) in g.rows" :key="i">
                <td>{{ r.a }}</td>
                <td>{{ r.b }}</td>
                <td class="text-xs opacity-70">{{ r.c }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </BaseCard>

      <ListPagination v-if="grouped.length > 0" :page="page" :total-pages="totalPages" @prev="prev" @next="next">
        {{ grouped.length }} {{ pivot === 'cardholder' ? 'cardholders' : 'portals' }}
      </ListPagination>
    </template>
  </div>
</template>

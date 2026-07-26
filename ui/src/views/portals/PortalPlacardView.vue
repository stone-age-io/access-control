<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import type { Portal, Location } from '@/types/pocketbase'
import QrCode from '@/components/ui/QrCode.vue'
import { PORTAL_QR_PREFIX } from '@/utils/portalQr'

/**
 * Printable door placards: a QR code per portal for the mobile grant page.
 *
 * The code encodes `portal:<code>` — the portal's identity, NOT a credential and NOT
 * a token. That distinction is the whole reason this is safe to stick on a wall:
 * photographing a placard gains nothing, because authorization comes from the
 * scanning operator's session and their `command` capability. Anyone without it gets
 * a 403.
 */
const toast = useToast()

const portals = ref<Portal[]>([])
const locations = ref<Location[]>([])
const loading = ref(true)
const locationFilter = ref('')
const selected = ref<Set<string>>(new Set())

const filtered = computed(() =>
  locationFilter.value ? portals.value.filter((p) => p.location === locationFilter.value) : portals.value,
)
const toPrint = computed(() =>
  selected.value.size ? filtered.value.filter((p) => selected.value.has(p.id)) : filtered.value,
)

function locationName(id: string): string {
  const l = locations.value.find((x) => x.id === id)
  return l?.name || l?.code || ''
}

function toggle(id: string) {
  const next = new Set(selected.value)
  next.has(id) ? next.delete(id) : next.add(id)
  selected.value = next
}

function selectAll() {
  selected.value = new Set(filtered.value.map((p) => p.id))
}
function selectNone() {
  selected.value = new Set()
}

// Templates resolve names against component scope only, so `window` needs a wrapper.
function printPage() {
  window.print()
}

async function load() {
  loading.value = true
  try {
    const [p, l] = await Promise.all([
      pb.collection('portals').getFullList<Portal>({ sort: 'code' }),
      pb.collection('locations').getFullList<Location>({ sort: 'code' }),
    ])
    portals.value = p
    locations.value = l
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load portals')
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-4">
    <!-- Controls: hidden when printing -->
    <div class="print:hidden space-y-4">
      <div>
        <h1 class="text-xl font-bold">Door Placards</h1>
        <p class="text-sm text-base-content/60">
          Printable QR codes for the mobile grant page. Each code identifies a door — it is not a credential, so
          a placard is safe in public view.
        </p>
      </div>

      <div class="card bg-base-100 shadow-sm">
        <div class="card-body gap-3">
          <div class="flex flex-wrap items-end gap-3">
            <label class="form-control">
              <span class="label-text mb-1">Location</span>
              <select v-model="locationFilter" class="select select-bordered select-sm">
                <option value="">All locations</option>
                <option v-for="l in locations" :key="l.id" :value="l.id">{{ l.name || l.code }}</option>
              </select>
            </label>
            <div class="flex gap-2">
              <button class="btn btn-sm btn-ghost" @click="selectAll">Select all</button>
              <button class="btn btn-sm btn-ghost" @click="selectNone">Clear</button>
              <button class="btn btn-sm btn-primary" :disabled="!toPrint.length" @click="printPage">
                Print {{ toPrint.length }}
              </button>
            </div>
          </div>

          <div v-if="loading" class="flex justify-center p-6">
            <span class="loading loading-spinner"></span>
          </div>
          <div v-else class="flex flex-wrap gap-2">
            <button
              v-for="p in filtered"
              :key="p.id"
              class="btn btn-xs"
              :class="selected.has(p.id) ? 'btn-primary' : 'btn-outline'"
              @click="toggle(p.id)"
            >
              {{ p.code }}
            </button>
          </div>
          <p v-if="!loading && !selected.size" class="text-xs text-base-content/50">
            Nothing selected — all {{ filtered.length }} shown door(s) will print.
          </p>
        </div>
      </div>
    </div>

    <!-- Placards: one per page when printing -->
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 print:block">
      <div
        v-for="p in toPrint"
        :key="p.id"
        class="card bg-base-100 border border-base-300 print:break-after-page print:border-0"
      >
        <div class="card-body items-center text-center gap-2">
          <QrCode :value="`${PORTAL_QR_PREFIX}${p.code}`" :size="240" />
          <div class="font-bold text-lg">{{ p.name || p.code }}</div>
          <div class="text-sm text-base-content/60">{{ locationName(p.location) }}</div>
          <div class="text-xs font-mono text-base-content/40">{{ p.code }}</div>
          <p class="text-xs text-base-content/50 mt-1">Scan with the operator app to unlock</p>
        </div>
      </div>
    </div>
  </div>
</template>

<style>
/* Placards should print as clean pages: drop the app chrome and page background. */
@media print {
  @page {
    margin: 12mm;
  }
  body {
    background: white;
  }
}
</style>

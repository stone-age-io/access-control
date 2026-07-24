<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'
import { policyKey } from '@/utils/policyKey'
import type { Location, Portal, AuxInput, AuxOutput } from '@/types/pocketbase'
import { PLACE_KIND_COLLECTION, type PlaceKind } from '@/utils/placeable'
import DetailLayout from '@/components/ui/DetailLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import DataField from '@/components/ui/DataField.vue'
import RecordMeta from '@/components/ui/RecordMeta.vue'
import RelationList from '@/components/ui/RelationList.vue'
import FloorPlanMap from '@/components/map/FloorPlanMap.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const { confirm } = useConfirm()

const recordId = route.params.id as string
const record = ref<Location | null>(null)
const portals = ref<Portal[]>([])
const auxInputs = ref<AuxInput[]>([])
const auxOutputs = ref<AuxOutput[]>([])
const loading = ref(true)
const deleting = ref(false)

const title = computed(() => record.value?.name || record.value?.code || 'Location')
const kvKey = computed(() => (record.value ? policyKey('locations', record.value) : ''))
const hasCoords = computed(() => {
  const c = record.value?.coordinates
  return !!c && (c.lat !== 0 || c.lon !== 0)
})

// Persist a point's floor-plan position (or null to remove it), routed to the
// right collection by kind. Optimistic: update the local record so the map
// re-renders immediately, revert on failure.
async function handleUpdatePosition({ kind, id, position }: { kind: PlaceKind; id: string; position: { x: number; y: number } | null }) {
  const list =
    kind === 'portal' ? portals.value : kind === 'aux_input' ? auxInputs.value : auxOutputs.value
  const rec = (list as { id: string; floorplan_position?: { x: number; y: number } | null }[]).find((r) => r.id === id)
  if (!rec) return
  const prev = rec.floorplan_position
  rec.floorplan_position = position
  try {
    await pb.collection(PLACE_KIND_COLLECTION[kind]).update(id, { floorplan_position: position })
  } catch (err: any) {
    rec.floorplan_position = prev
    toast.error(err?.message || 'Failed to update position')
  }
}

async function load() {
  loading.value = true
  try {
    const [l, pts, ins, outs] = await Promise.all([
      pb.collection('locations').getOne<Location>(recordId, { expand: 'holiday_calendars' }),
      pb.collection('portals').getFullList<Portal>({ filter: `location = "${recordId}"`, sort: 'code' }),
      pb.collection('aux_input').getFullList<AuxInput>({ filter: `location = "${recordId}"`, sort: 'code' }),
      pb.collection('aux_output').getFullList<AuxOutput>({ filter: `location = "${recordId}"`, sort: 'code' }),
    ])
    record.value = l
    portals.value = pts
    auxInputs.value = ins
    auxOutputs.value = outs
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load location')
    router.push('/locations')
  } finally {
    loading.value = false
  }
}

async function handleDelete() {
  if (!record.value) return
  const confirmed = await confirm({
    title: 'Delete Location',
    message: `Delete location "${record.value.code}"?`,
    details: 'Portals referencing this location will be left without one. This cannot be undone.',
    confirmText: 'Delete',
    variant: 'danger',
  })
  if (!confirmed) return
  deleting.value = true
  try {
    await pb.collection('locations').delete(recordId)
    toast.success('Location deleted')
    router.push('/locations')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to delete location')
  } finally {
    deleting.value = false
  }
}

onMounted(load)
</script>

<template>
  <div v-if="loading" class="flex justify-center p-12">
    <span class="loading loading-spinner loading-lg"></span>
  </div>

  <DetailLayout
    v-else-if="record"
    :title="title"
    :breadcrumbs="[{ label: 'Locations', to: '/locations' }, { label: title }]"
  >
    <template #actions>
      <router-link :to="`/locations/${record.id}/edit`" class="btn btn-sm btn-primary">Edit</router-link>
      <button class="btn btn-sm btn-ghost text-error" :disabled="deleting" @click="handleDelete">Delete</button>
    </template>

    <BaseCard>
      <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-x-6 gap-y-4">
        <DataField label="Code">
          <code class="text-sm">{{ record.code }}</code>
        </DataField>
        <DataField label="Name">{{ record.name || '—' }}</DataField>
        <DataField label="Timezone">
          <code class="text-sm">{{ record.timezone }}</code>
        </DataField>
        <DataField label="FAI Suppress">
          <SoftBadge :tone="record.fai_suppress ? 'success' : 'neutral'" dot>
            {{ record.fai_suppress ? 'suppressed' : 'off' }}
          </SoftBadge>
        </DataField>
        <DataField label="Email on fire">
          <SoftBadge :tone="record.notify_fire ? 'warning' : 'neutral'" dot>
            {{ record.notify_fire ? 'emails opted-in operators' : 'off' }}
          </SoftBadge>
        </DataField>
        <DataField label="Coordinates">
          <span v-if="hasCoords" class="font-mono text-sm">
            {{ record.coordinates.lat.toFixed(5) }}, {{ record.coordinates.lon.toFixed(5) }}
          </span>
          <span v-else class="text-base-content/50">—</span>
        </DataField>
        <DataField label="Holiday calendars">
          <div v-if="record.expand?.holiday_calendars?.length" class="flex flex-wrap gap-1">
            <router-link
              v-for="c in record.expand.holiday_calendars"
              :key="c.id"
              :to="`/holiday-calendars/${c.id}`"
              class="badge-soft badge-soft-neutral gap-1"
            ><code class="text-xs">{{ c.code }}</code></router-link>
          </div>
          <span v-else class="opacity-40">none</span>
        </DataField>
      </div>
      <div v-if="record.description" class="mt-4 pt-4 border-t border-base-200">
        <DataField label="Description">{{ record.description }}</DataField>
      </div>
    </BaseCard>

    <RelationList
      title="Portals"
      icon="🚪"
      :items="portals"
      :to="(p) => `/portals/${p.id}`"
      :primary="(p) => p.code"
      :secondary="(p) => p.name"
      empty="No portals in this location."
    >
      <template #actions>
        <router-link :to="`/portals/new?location=${record.id}`" class="btn btn-sm btn-outline">+ Add portal</router-link>
      </template>
    </RelationList>

    <BaseCard title="Floor plan">
      <FloorPlanMap
        v-if="record.floorplan"
        :location="record"
        :portals="portals"
        :aux-inputs="auxInputs"
        :aux-outputs="auxOutputs"
        @update-position="handleUpdatePosition"
      />
      <div v-else class="text-center py-8 text-base-content/60">
        <span class="text-3xl">🗺️</span>
        <p class="text-sm mt-2">No floor plan uploaded for this location.</p>
        <router-link :to="`/locations/${record.id}/edit`" class="btn btn-sm btn-outline mt-3">
          Upload a floor plan
        </router-link>
      </div>
    </BaseCard>

    <RecordMeta :record="record" :kv-key="kvKey" />
  </DetailLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import { policyKey } from '@/utils/policyKey'
import type { AccessGroup, Area, AuxOutput, AreaRight, Portal, Schedule } from '@/types/pocketbase'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'
import RelationPicker from '@/components/ui/RelationPicker.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const form = ref({
  code: '',
  name: '',
  schedule: '',
  portals: [] as string[],
  areas: [] as string[],
  aux_outputs: [] as string[],
  area_rights: [] as AreaRight[],
})

const portals = ref<Portal[]>([])
const schedules = ref<Schedule[]>([])
const areas = ref<Area[]>([])
const auxOutputs = ref<AuxOutput[]>([])
const loading = ref(false)
const loadingRecord = ref(false)
const errors = ref<Record<string, string>>({})
const { markClean } = useUnsavedChanges(() => form.value)

const kvKey = computed(() => policyKey('access_groups', { code: form.value.code.trim() }))

const portalLocation = (p: Portal) => p.expand?.location?.code || '—'
const portalSearch = (p: Portal) => [p.code, p.name, p.expand?.location?.code].filter(Boolean).join(' ')
// Areas and aux outputs group and search on exactly the same shape as portals.
const targetLocation = (t: Area | AuxOutput) => t.expand?.location?.code || '—'
const targetSearch = (t: Area | AuxOutput) => [t.code, t.name, t.expand?.location?.code].filter(Boolean).join(' ')

/**
 * Both rights, pre-selected the moment the first area is added.
 *
 * The server treats empty `area_rights` as granting NEITHER right (fail closed), so a
 * group with areas and no rights is inert. That is the correct default on the wire and
 * the wrong default in a form: an operator who ticks an area plainly means to grant
 * something. So the form fills in the common case and leaves narrowing to them —
 * un-ticking `disarm` is a deliberate act, where noticing an empty list is not.
 */
function onAreasChanged(next: string[]) {
  form.value.areas = next
  if (next.length > 0 && form.value.area_rights.length === 0) {
    form.value.area_rights = ['arm', 'disarm']
  }
}

function toggleRight(right: AreaRight) {
  const held = form.value.area_rights.includes(right)
  form.value.area_rights = held
    ? form.value.area_rights.filter((r) => r !== right)
    : [...form.value.area_rights, right]
}

/** Areas chosen but no right held: the one combination that silently does nothing. */
const rightsMissing = computed(() => form.value.areas.length > 0 && form.value.area_rights.length === 0)

async function loadOptions() {
  try {
    const [pts, scheds, ars, outs] = await Promise.all([
      pb.collection('portals').getFullList<Portal>({ sort: 'code', expand: 'location' }),
      pb.collection('schedules').getFullList<Schedule>({ sort: 'code' }),
      pb.collection('areas').getFullList<Area>({ sort: 'code', expand: 'location' }),
      pb.collection('aux_output').getFullList<AuxOutput>({ sort: 'code', expand: 'location' }),
    ])
    portals.value = pts
    schedules.value = scheds
    areas.value = ars
    auxOutputs.value = outs
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load options')
  }
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const g = await pb.collection('access_groups').getOne<AccessGroup>(recordId)
    form.value = {
      code: g.code || '',
      name: g.name || '',
      schedule: g.schedule || '',
      portals: [...(g.portals || [])],
      areas: [...(g.areas || [])],
      aux_outputs: [...(g.aux_outputs || [])],
      area_rights: [...(g.area_rights || [])],
    }
    markClean()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load access group')
    router.push('/access-groups')
  } finally {
    loadingRecord.value = false
  }
}

function validate(): boolean {
  const e: Record<string, string> = {}
  if (!form.value.code.trim()) e.code = 'Code is required'
  if (!form.value.schedule) e.schedule = 'Schedule is required'
  errors.value = e
  const first = Object.values(e)[0]
  if (first) toast.error(first)
  return !first
}

async function handleSubmit() {
  if (!validate()) return

  loading.value = true
  try {
    const data = {
      code: form.value.code.trim(),
      name: form.value.name.trim(),
      schedule: form.value.schedule,
      portals: form.value.portals,
      areas: form.value.areas,
      aux_outputs: form.value.aux_outputs,
      // Sent even when empty, so clearing every right actually clears it rather than
      // leaving the previous value in place.
      area_rights: form.value.area_rights,
    }
    if (isEdit.value) {
      await pb.collection('access_groups').update(recordId!, data)
      toast.success('Access group updated')
      markClean()
      router.push(`/access-groups/${recordId}`)
    } else {
      const created = await pb.collection('access_groups').create<AccessGroup>(data)
      toast.success('Access group created')
      markClean()
      router.push(`/access-groups/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save access group')
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await loadOptions()
  if (isEdit.value) await loadRecord()
})
</script>

<template>
  <div v-if="loadingRecord" class="flex justify-center p-12">
    <span class="loading loading-spinner loading-lg"></span>
  </div>

  <form v-else @submit.prevent="handleSubmit">
    <FormLayout
      :title="isEdit ? 'Edit Access Group' : 'New Access Group'"
      :breadcrumbs="[{ label: 'Access Groups', to: '/access-groups' }, { label: isEdit ? 'Edit' : 'New' }]"
      :kv-key="kvKey"
      :kv-placeholder="'group.<code>'"
    >
      <BaseCard title="Access Group">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Code" required :error="errors.code">
              <input v-model="form.code" type="text" placeholder="lobby-group" class="input input-bordered font-mono" required />
            </FormField>
            <FormField label="Name">
              <input v-model="form.name" type="text" placeholder="Lobby Access" class="input input-bordered" />
            </FormField>
          </div>

          <FormField label="Schedule" required :error="errors.schedule">
            <select v-model="form.schedule" class="select select-bordered" required>
              <option value="">Select a schedule...</option>
              <option v-for="s in schedules" :key="s.id" :value="s.id">{{ s.code }} — {{ s.name || s.code }}</option>
            </select>
            <p v-if="schedules.length === 0" class="text-xs text-warning">No schedules exist yet — create one first.</p>
          </FormField>
        </div>
      </BaseCard>

      <BaseCard title="Portals">
        <div class="space-y-2">
          <p class="text-sm text-base-content/60">The portals this group grants (during the schedule's windows), grouped by location.</p>
          <RelationPicker
            v-model="form.portals"
            :options="portals"
            :group="portalLocation"
            :search-text="portalSearch"
            :primary="(p) => p.code"
            :secondary="(p) => p.name"
            empty="No portals available. Create some first."
          />
        </div>
      </BaseCard>

      <!-- Areas + the two arm rights. One schedule covers all three target kinds:
           "warehouse staff, Mon–Fri 06:00–18:00" is one window whether it authorizes a
           door, a disarm, or a gate relay. -->
      <BaseCard title="Areas">
        <div class="space-y-3">
          <p class="text-sm text-base-content/60">
            The areas this group can arm or disarm during the schedule's windows.
          </p>
          <!-- Not v-model: the handler both assigns and pre-selects the rights, so it
               needs the incoming value rather than a possibly-not-yet-assigned form
               field. Loading an existing record deliberately does NOT go through here,
               or editing a group with areas and no rights would silently grant both. -->
          <RelationPicker
            :model-value="form.areas"
            :options="areas"
            :group="targetLocation"
            :search-text="targetSearch"
            :primary="(a) => a.code"
            :secondary="(a) => a.name"
            empty="No areas exist yet — create one first."
            @update:model-value="onAreasChanged"
          />

          <div v-if="form.areas.length" class="space-y-2 pt-1">
            <span class="label-text font-medium">Rights</span>
            <div class="flex flex-wrap gap-4">
              <label class="label cursor-pointer gap-2 justify-start p-0">
                <input
                  type="checkbox"
                  class="checkbox checkbox-sm"
                  :checked="form.area_rights.includes('arm')"
                  @change="toggleRight('arm')"
                />
                <span class="label-text">Arm</span>
              </label>
              <label class="label cursor-pointer gap-2 justify-start p-0">
                <input
                  type="checkbox"
                  class="checkbox checkbox-sm"
                  :checked="form.area_rights.includes('disarm')"
                  @change="toggleRight('disarm')"
                />
                <span class="label-text">Disarm</span>
              </label>
            </div>
            <p class="text-xs text-base-content/60">
              Separate on purpose: disarming turns intrusion detection off. Closing staff who
              lock up can be given <span class="font-medium">Arm</span> alone.
            </p>
            <div v-if="rightsMissing" class="alert alert-warning py-2 text-sm">
              <span>
                No rights selected — these areas are on the badge but nothing can be armed or
                disarmed. Tick at least one.
              </span>
            </div>
          </div>
        </div>
      </BaseCard>

      <BaseCard title="Aux outputs">
        <div class="space-y-2">
          <p class="text-sm text-base-content/60">
            Auxiliary relays this group can drive — a vehicle gate, a light, a bell. One right:
            driving it. On, off, and pulse are the same relay, so they are not granted separately.
          </p>
          <RelationPicker
            v-model="form.aux_outputs"
            :options="auxOutputs"
            :group="targetLocation"
            :search-text="targetSearch"
            :primary="(o) => o.code"
            :secondary="(o) => o.name"
            empty="No aux outputs exist yet."
          />
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Access Group</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>

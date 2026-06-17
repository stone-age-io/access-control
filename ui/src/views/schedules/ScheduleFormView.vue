<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import type { Schedule, ScheduleWindow } from '@/types/pocketbase'
import BaseCard from '@/components/ui/BaseCard.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

// ISO weekdays: 1=Mon .. 7=Sun.
const DAYS = [
  { num: 1, label: 'Mon' },
  { num: 2, label: 'Tue' },
  { num: 3, label: 'Wed' },
  { num: 4, label: 'Thu' },
  { num: 5, label: 'Fri' },
  { num: 6, label: 'Sat' },
  { num: 7, label: 'Sun' },
]

const code = ref('')
const name = ref('')
const windows = ref<ScheduleWindow[]>([{ days: [1, 2, 3, 4, 5], start: '08:00', end: '17:00' }])

const loading = ref(false)
const loadingRecord = ref(false)

function addWindow() {
  windows.value.push({ days: [1, 2, 3, 4, 5], start: '08:00', end: '17:00' })
}

function removeWindow(idx: number) {
  windows.value.splice(idx, 1)
}

function toggleDay(w: ScheduleWindow, day: number) {
  const i = w.days.indexOf(day)
  if (i === -1) w.days.push(day)
  else w.days.splice(i, 1)
  w.days.sort((a, b) => a - b)
}

function crossesMidnight(w: ScheduleWindow): boolean {
  return !!w.start && !!w.end && w.end <= w.start
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const sched = await pb.collection('schedules').getOne<Schedule>(recordId)
    code.value = sched.code || ''
    name.value = sched.name || ''
    windows.value = Array.isArray(sched.windows) && sched.windows.length
      ? sched.windows.map(w => ({ days: [...(w.days || [])], start: w.start || '', end: w.end || '' }))
      : [{ days: [1, 2, 3, 4, 5], start: '08:00', end: '17:00' }]
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load schedule')
    router.push('/schedules')
  } finally {
    loadingRecord.value = false
  }
}

function validate(): boolean {
  if (!code.value.trim()) { toast.error('Code is required'); return false }
  for (const [i, w] of windows.value.entries()) {
    if (!w.days.length) { toast.error(`Window ${i + 1}: pick at least one day`); return false }
    if (!w.start || !w.end) { toast.error(`Window ${i + 1}: set start and end times`); return false }
  }
  return true
}

async function handleSubmit() {
  if (!validate()) return
  loading.value = true
  try {
    const data = {
      code: code.value.trim(),
      name: name.value.trim(),
      windows: windows.value.map(w => ({ days: w.days, start: w.start, end: w.end })),
    }
    if (isEdit.value) {
      await pb.collection('schedules').update(recordId!, data)
      toast.success('Schedule updated')
    } else {
      await pb.collection('schedules').create(data)
      toast.success('Schedule created')
    }
    router.push('/schedules')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save schedule')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (isEdit.value) loadRecord()
})
</script>

<template>
  <div class="space-y-6 max-w-3xl">
    <div>
      <div class="breadcrumbs text-sm">
        <ul>
          <li><router-link to="/schedules">Schedules</router-link></li>
          <li>{{ isEdit ? 'Edit' : 'New' }}</li>
        </ul>
      </div>
      <h1 class="text-3xl font-bold">{{ isEdit ? 'Edit Schedule' : 'New Schedule' }}</h1>
    </div>

    <div v-if="loadingRecord" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <form v-else @submit.prevent="handleSubmit" class="space-y-6">
      <BaseCard title="Schedule">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div class="form-control">
              <label class="label"><span class="label-text">Code *</span></label>
              <input v-model="code" type="text" placeholder="business-hours" class="input input-bordered font-mono" required />
            </div>
            <div class="form-control">
              <label class="label"><span class="label-text">Name</span></label>
              <input v-model="name" type="text" placeholder="Business Hours" class="input input-bordered" />
            </div>
          </div>
        </div>
      </BaseCard>

      <BaseCard title="Time Windows">
        <template #actions>
          <button type="button" class="btn btn-sm btn-outline" @click="addWindow">+ Add Window</button>
        </template>

        <div class="space-y-4">
          <p class="text-sm text-base-content/60">
            Access is open during any window. An end time at or before the start crosses midnight
            (e.g. <code class="font-mono">22:00 → 06:00</code>).
          </p>

          <div v-for="(w, idx) in windows" :key="idx" class="rounded-box border border-base-300 p-4 space-y-3">
            <div class="flex items-center justify-between">
              <span class="text-xs font-bold uppercase tracking-wider opacity-60">Window {{ idx + 1 }}</span>
              <button
                type="button"
                class="btn btn-xs btn-ghost text-error"
                @click="removeWindow(idx)"
                :disabled="windows.length === 1"
                title="Remove window"
              >
                Remove
              </button>
            </div>

            <!-- Days -->
            <div class="flex flex-wrap gap-1.5">
              <button
                v-for="d in DAYS"
                :key="d.num"
                type="button"
                class="btn btn-sm"
                :class="w.days.includes(d.num) ? 'btn-primary' : 'btn-outline'"
                @click="toggleDay(w, d.num)"
              >
                {{ d.label }}
              </button>
            </div>

            <!-- Times -->
            <div class="flex flex-wrap items-end gap-4">
              <div class="form-control">
                <label class="label py-1"><span class="label-text text-xs">Start</span></label>
                <input v-model="w.start" type="time" class="input input-bordered input-sm font-mono" />
              </div>
              <span class="pb-2 opacity-50">→</span>
              <div class="form-control">
                <label class="label py-1"><span class="label-text text-xs">End</span></label>
                <input v-model="w.end" type="time" class="input input-bordered input-sm font-mono" />
              </div>
              <span v-if="crossesMidnight(w)" class="badge badge-warning badge-sm mb-2">crosses midnight</span>
            </div>
          </div>
        </div>
      </BaseCard>

      <div class="flex flex-col sm:flex-row justify-end gap-2 sm:gap-4">
        <button type="button" @click="router.back()" class="btn btn-ghost order-2 sm:order-1" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary order-1 sm:order-2" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Schedule</span>
        </button>
      </div>
    </form>
  </div>
</template>

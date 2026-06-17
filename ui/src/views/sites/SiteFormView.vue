<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import type { Site } from '@/types/pocketbase'
import BaseCard from '@/components/ui/BaseCard.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const form = ref({
  code: '',
  name: '',
  timezone: 'America/New_York',
  fai_suppress: true,
})

const loading = ref(false)
const loadingRecord = ref(false)

// A short list of common IANA zones for the datalist; any valid IANA name works.
const commonTimezones = [
  'UTC',
  'America/New_York',
  'America/Chicago',
  'America/Denver',
  'America/Los_Angeles',
  'America/Phoenix',
  'America/Toronto',
  'America/Sao_Paulo',
  'Europe/London',
  'Europe/Paris',
  'Europe/Berlin',
  'Europe/Madrid',
  'Asia/Tokyo',
  'Asia/Shanghai',
  'Asia/Singapore',
  'Asia/Kolkata',
  'Australia/Sydney',
]

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const site = await pb.collection('sites').getOne<Site>(recordId)
    form.value = {
      code: site.code || '',
      name: site.name || '',
      timezone: site.timezone || 'UTC',
      fai_suppress: site.fai_suppress ?? true,
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load site')
    router.push('/sites')
  } finally {
    loadingRecord.value = false
  }
}

async function handleSubmit() {
  if (!form.value.code.trim()) { toast.error('Code is required'); return }
  if (!form.value.timezone.trim()) { toast.error('Timezone is required'); return }

  loading.value = true
  try {
    const data = {
      code: form.value.code.trim(),
      name: form.value.name.trim(),
      timezone: form.value.timezone.trim(),
      fai_suppress: form.value.fai_suppress,
    }
    if (isEdit.value) {
      await pb.collection('sites').update(recordId!, data)
      toast.success('Site updated')
    } else {
      await pb.collection('sites').create(data)
      toast.success('Site created')
    }
    router.push('/sites')
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save site')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  if (isEdit.value) loadRecord()
})
</script>

<template>
  <div class="space-y-6 max-w-2xl">
    <div>
      <div class="breadcrumbs text-sm">
        <ul>
          <li><router-link to="/sites">Sites</router-link></li>
          <li>{{ isEdit ? 'Edit' : 'New' }}</li>
        </ul>
      </div>
      <h1 class="text-3xl font-bold">{{ isEdit ? 'Edit Site' : 'New Site' }}</h1>
    </div>

    <div v-if="loadingRecord" class="flex justify-center p-12">
      <span class="loading loading-spinner loading-lg"></span>
    </div>

    <form v-else @submit.prevent="handleSubmit" class="space-y-6">
      <BaseCard title="Site">
        <div class="space-y-4">
          <div class="form-control">
            <label class="label"><span class="label-text">Code *</span></label>
            <input v-model="form.code" type="text" placeholder="hq" class="input input-bordered font-mono" required />
            <label class="label"><span class="label-text-alt">Stable slug used in NATS subjects and as the KV key. Avoid spaces.</span></label>
          </div>

          <div class="form-control">
            <label class="label"><span class="label-text">Name</span></label>
            <input v-model="form.name" type="text" placeholder="Headquarters" class="input input-bordered" />
          </div>

          <div class="form-control">
            <label class="label"><span class="label-text">Timezone *</span></label>
            <input v-model="form.timezone" list="tz-list" type="text" placeholder="America/New_York" class="input input-bordered font-mono" required />
            <datalist id="tz-list">
              <option v-for="tz in commonTimezones" :key="tz" :value="tz" />
            </datalist>
            <label class="label"><span class="label-text-alt">IANA timezone name. Used to evaluate schedule windows in local time (handles DST).</span></label>
          </div>

          <div class="form-control">
            <label class="label cursor-pointer justify-start gap-3">
              <input v-model="form.fai_suppress" type="checkbox" class="toggle toggle-primary" />
              <span class="label-text">Suppress alarms while fire input is active (FAI)</span>
            </label>
            <label class="label"><span class="label-text-alt">Hardware owns egress; software only suppresses false forced/held-open alarms during fire.</span></label>
          </div>
        </div>
      </BaseCard>

      <div class="flex flex-col sm:flex-row justify-end gap-2 sm:gap-4">
        <button type="button" @click="router.back()" class="btn btn-ghost order-2 sm:order-1" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary order-1 sm:order-2" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Site</span>
        </button>
      </div>
    </form>
  </div>
</template>

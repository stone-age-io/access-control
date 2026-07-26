<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import { useFileUrl } from '@/composables/useFileUrl'
import type { Cardholder, CardholderStatus, Role } from '@/types/pocketbase'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'
import RelationPicker from '@/components/ui/RelationPicker.vue'
import Avatar from '@/components/ui/Avatar.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()

const recordId = route.params.id as string | undefined
const isEdit = computed(() => !!recordId)

const STATUSES: CardholderStatus[] = ['active', 'suspended']

const form = ref({
  external_id: '',
  name: '',
  email: '',
  status: 'active' as CardholderStatus,
  roles: [] as string[],
})

const roles = ref<Role[]>([])
const loading = ref(false)
const loadingRecord = ref(false)
const errors = ref<Record<string, string>>({})

// --- photo ---------------------------------------------------------------
// The photo lives outside `form` because it is a File, not a JSON value. The
// loaded record is kept so the stored photo's (protected, token-bearing) URL can
// be built for the preview.
const PHOTO_MAX_BYTES = 5 << 20 // must match the field's MaxSize (migration 1750000029)
const PHOTO_TYPES = ['image/jpeg', 'image/png', 'image/webp']

const existing = ref<Cardholder | null>(null)
const photoFile = ref<File | null>(null)
const photoRemoved = ref(false)
const localPreview = ref('')
const fileInput = ref<HTMLInputElement | null>(null)
const { url: fileUrl } = useFileUrl()

// A newly picked file wins over the stored one; an explicit remove clears both.
const photoPreview = computed(() => {
  if (localPreview.value) return localPreview.value
  if (photoRemoved.value || !existing.value?.photo) return ''
  return fileUrl(existing.value, existing.value.photo, '400x400')
})
const hasPhoto = computed(() => !!photoPreview.value)

watch(photoFile, (f) => {
  if (localPreview.value) URL.revokeObjectURL(localPreview.value)
  localPreview.value = f ? URL.createObjectURL(f) : ''
})
onBeforeUnmount(() => {
  if (localPreview.value) URL.revokeObjectURL(localPreview.value)
})

function onPhotoChange(e: Event) {
  const picked = (e.target as HTMLInputElement).files?.[0] || null
  if (!picked) return
  // Reject client-side so the operator gets an immediate, specific message rather
  // than a generic 400 from the field validator.
  if (!PHOTO_TYPES.includes(picked.type)) {
    toast.error('Photo must be a JPEG, PNG, or WebP image')
    if (fileInput.value) fileInput.value.value = ''
    return
  }
  if (picked.size > PHOTO_MAX_BYTES) {
    toast.error('Photo must be 5 MB or smaller')
    if (fileInput.value) fileInput.value.value = ''
    return
  }
  photoFile.value = picked
  photoRemoved.value = false
}

function removePhoto() {
  photoFile.value = null
  photoRemoved.value = true
  if (fileInput.value) fileInput.value.value = ''
}

// The dirty check is JSON-based, so the File itself is invisible to it — surface
// the photo intent explicitly or a photo-only edit would leave without a prompt.
const { markClean } = useUnsavedChanges(() => ({
  ...form.value,
  photoName: photoFile.value?.name || '',
  photoRemoved: photoRemoved.value,
}))

const kvKey = computed(() => (recordId ? `user.${recordId}` : ''))

async function loadOptions() {
  try {
    roles.value = await pb.collection('roles').getFullList<Role>({ sort: 'code' })
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load roles')
  }
}

async function loadRecord() {
  if (!recordId) return
  loadingRecord.value = true
  try {
    const c = await pb.collection('cardholders').getOne<Cardholder>(recordId)
    existing.value = c
    form.value = {
      external_id: c.external_id || '',
      name: c.name || '',
      email: c.email || '',
      status: (c.status || 'active') as CardholderStatus,
      roles: [...(c.roles || [])],
    }
    photoFile.value = null
    photoRemoved.value = false
    markClean()
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load cardholder')
    router.push('/cardholders')
  } finally {
    loadingRecord.value = false
  }
}

function validate(): boolean {
  const e: Record<string, string> = {}
  if (!form.value.name.trim() && !form.value.email.trim()) {
    e.name = 'Name or email is required'
    e.email = 'Name or email is required'
  }
  errors.value = e
  const first = Object.values(e)[0]
  if (first) toast.error(first)
  return !first
}

async function handleSubmit() {
  if (!validate()) return

  loading.value = true
  try {
    const data: Record<string, unknown> = {
      external_id: form.value.external_id.trim(),
      name: form.value.name.trim(),
      email: form.value.email.trim(),
      status: form.value.status,
      roles: form.value.roles,
    }
    // Mutually exclusive by construction: picking a file clears photoRemoved, and
    // removing clears the file. A File makes the SDK send multipart; null clears
    // the stored file over plain JSON.
    if (photoFile.value) data.photo = photoFile.value
    else if (photoRemoved.value) data.photo = null

    if (isEdit.value) {
      await pb.collection('cardholders').update(recordId!, data)
      toast.success('Cardholder updated')
      markClean()
      router.push(`/cardholders/${recordId}`)
    } else {
      const created = await pb.collection('cardholders').create<Cardholder>(data)
      toast.success('Cardholder created')
      markClean()
      router.push(`/cardholders/${created.id}`)
    }
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save cardholder')
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
      :title="isEdit ? 'Edit Cardholder' : 'New Cardholder'"
      :breadcrumbs="[{ label: 'Cardholders', to: '/cardholders' }, { label: isEdit ? 'Edit' : 'New' }]"
      :kv-key="kvKey"
      :kv-placeholder="'user.<assigned on save>'"
    >
      <BaseCard title="Cardholder">
        <div class="space-y-4">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="Name" :error="errors.name">
              <input v-model="form.name" type="text" placeholder="Alice Smith" class="input input-bordered" />
            </FormField>
            <FormField label="Email" :error="errors.email">
              <input v-model="form.email" type="email" placeholder="alice@example.com" class="input input-bordered" />
            </FormField>
          </div>

          <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
            <FormField label="External ID" hint="Optional IdP/LDAP/CSV key.">
              <input v-model="form.external_id" type="text" placeholder="ldap-12345" class="input input-bordered font-mono" />
            </FormField>
            <FormField label="Status">
              <select v-model="form.status" class="select select-bordered">
                <option v-for="s in STATUSES" :key="s" :value="s">{{ s }}</option>
              </select>
            </FormField>
          </div>
        </div>
      </BaseCard>

      <BaseCard title="Photo">
        <div class="flex items-start gap-5">
          <Avatar
            :name="form.name || form.email"
            :seed="recordId || form.email || form.name"
            size="lg"
            :src="photoPreview"
          />
          <div class="space-y-2 min-w-0">
            <FormField
              label="Cardholder photo"
              hint="JPEG, PNG, or WebP · 5 MB max. Used for visual identification and on the cardholder's badge."
            >
              <input
                ref="fileInput"
                type="file"
                accept="image/jpeg,image/png,image/webp"
                class="file-input file-input-bordered"
                @change="onPhotoChange"
              />
            </FormField>
            <button v-if="hasPhoto" type="button" class="btn btn-xs btn-ghost text-error" @click="removePhoto">
              Remove photo
            </button>
            <p class="text-xs text-base-content/50">
              Stored as a protected file — the image URL requires a signed-in session and is never public.
            </p>
          </div>
        </div>
      </BaseCard>

      <BaseCard title="Roles">
        <div class="space-y-2">
          <p class="text-sm text-base-content/60">The roles assigned to this cardholder.</p>
          <RelationPicker
            v-model="form.roles"
            :options="roles"
            :primary="(r) => r.code"
            :secondary="(r) => r.name"
            empty="No roles available. Create some first."
          />
        </div>
      </BaseCard>

      <template #actions>
        <button type="button" @click="router.back()" class="btn btn-ghost" :disabled="loading">Cancel</button>
        <button type="submit" class="btn btn-primary" :disabled="loading">
          <span v-if="loading" class="loading loading-spinner"></span>
          <span v-else>{{ isEdit ? 'Update' : 'Create' }} Cardholder</span>
        </button>
      </template>
    </FormLayout>
  </form>
</template>

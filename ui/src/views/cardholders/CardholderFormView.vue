<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import { useFileUrl } from '@/composables/useFileUrl'
import { useAuthStore } from '@/stores/auth'
import type { Cardholder, CardholderStatus, Role } from '@/types/pocketbase'
import type { BadgeUser, IssueHolderResponse } from '@/types/badge'
import FormLayout from '@/components/ui/FormLayout.vue'
import BaseCard from '@/components/ui/BaseCard.vue'
import FormField from '@/components/ui/FormField.vue'
import RelationPicker from '@/components/ui/RelationPicker.vue'
import Avatar from '@/components/ui/Avatar.vue'

const router = useRouter()
const route = useRoute()
const toast = useToast()
const auth = useAuthStore()

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

// --- badge login -----------------------------------------------------------
// A badge login is 1:1 with a cardholder, so it belongs on this form as a property of
// the person: one checkbox for "can view their badge on a phone". It used to be a card
// with its own inline form on the DETAIL page, which made a boolean look like a related
// entity and put a write surface on a read-only page.
//
// It lives outside `form` because it is not a cardholder field — it is a separate
// record, reconciled after the save through POST /api/badge/holders (which owns the
// `kind`, throwaway-password and `password_set` semantics) or a plain delete.
//
// Both directions need `enroll` (migration 1750000035 made them symmetric): whoever may
// hand out a badge login may take it back.
const canEnroll = computed(() => auth.can('enroll'))

const badge = ref<BadgeUser | null>(null)
const wantsBadge = ref(false)
const badgePassword = ref('')
const badgeSendInvite = ref(false)

// A login signs in by email, so there must be one. Stated as a computed rather than a
// validation error because the honest UI is a disabled checkbox with a reason.
const canHaveBadge = computed(() => !!form.value.email.trim())
const badgeIsVisitor = computed(() => badge.value?.kind === 'visitor')

async function loadBadge() {
  if (!recordId) return
  try {
    badge.value = await pb
      .collection('badge_users')
      .getFirstListItem<BadgeUser>(`cardholder = "${recordId}"`)
  } catch {
    badge.value = null // absence is the normal case, not an error
  }
  wantsBadge.value = !!badge.value
}

/**
 * Apply the badge-login intent after the cardholder itself is saved.
 *
 * Runs AFTER, and reports its own failure without failing the save: the cardholder edit
 * is the operator's main intent and has already committed, so a mail or permission
 * problem on the login must not be reported as "failed to save cardholder". Returns a
 * message to show, or ''.
 */
async function reconcileBadge(cardholderId: string): Promise<string> {
  if (!canEnroll.value) return ''

  // Removing: a visitor's pass is revoked with their login by the server hook, which is
  // why this needs no revoke call of its own.
  if (!wantsBadge.value) {
    if (!badge.value) return ''
    await pb.collection('badge_users').delete(badge.value.id)
    badge.value = null
    return ''
  }

  // Nothing to do: an existing login, no new password, no invite requested.
  if (badge.value && !badgePassword.value && !badgeSendInvite.value) return ''

  const res = await pb.send<IssueHolderResponse>('/api/badge/holders', {
    method: 'POST',
    body: {
      cardholder: cardholderId,
      email: form.value.email.trim(),
      password: badgePassword.value,
      sendInvite: badgeSendInvite.value,
    },
  })
  badgePassword.value = ''
  if (badgeSendInvite.value && !res.inviteSent) {
    // Not a failure — the login works regardless. Say it, because the operator now has
    // to hand the sign-in details over themselves.
    return 'The badge login was saved, but the invite email could not be sent. Give them the sign-in details in person.'
  }
  return ''
}

// The dirty check is JSON-based, so the File itself is invisible to it — surface
// the photo intent explicitly or a photo-only edit would leave without a prompt.
// Same for the badge intent, which is a separate record rather than a form field.
const { markClean } = useUnsavedChanges(() => ({
  ...form.value,
  photoName: photoFile.value?.name || '',
  photoRemoved: photoRemoved.value,
  wantsBadge: wantsBadge.value,
  badgePassword: badgePassword.value,
  badgeSendInvite: badgeSendInvite.value,
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

    let id = recordId
    if (isEdit.value) {
      await pb.collection('cardholders').update(recordId!, data)
      toast.success('Cardholder updated')
    } else {
      const created = await pb.collection('cardholders').create<Cardholder>(data)
      id = created.id
      toast.success('Cardholder created')
    }

    // The cardholder is saved; the login is a second record, so its failure is reported
    // separately rather than as a failed save.
    try {
      const warning = await reconcileBadge(id!)
      if (warning) toast.error(warning)
    } catch (err: any) {
      toast.error(
        `The cardholder was saved, but their badge login was not: ${err?.response?.message || err?.message || 'unknown error'}`,
      )
    }

    markClean()
    router.push(`/cardholders/${id}`)
  } catch (err: any) {
    toast.error(err?.message || 'Failed to save cardholder')
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await loadOptions()
  if (isEdit.value) {
    await loadRecord()
    await loadBadge()
    markClean() // loadBadge sets wantsBadge, which the dirty check watches
  }
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

      <!-- Badge login: one property of this person, not a related entity. -->
      <BaseCard v-if="canEnroll" title="Badge login">
        <div class="space-y-4">
          <label class="label cursor-pointer justify-start gap-3 p-0" :class="{ 'opacity-50': !canHaveBadge }">
            <input
              v-model="wantsBadge"
              type="checkbox"
              class="checkbox"
              :disabled="!canHaveBadge || badgeIsVisitor"
            />
            <span class="label-text">Can view their badge on a phone</span>
          </label>
          <p class="text-xs text-base-content/60 -mt-2">
            Lets this person sign in to see their badge and use remote unlock where a door allows it.
            It grants no access of its own — their credentials decide that, and they keep working either way.
          </p>

          <p v-if="!canHaveBadge" class="text-xs text-warning">
            Add an email address above first — it is the sign-in identity for the badge.
          </p>
          <p v-else-if="badgeIsVisitor" class="text-xs text-warning">
            This person holds a <strong>visitor</strong> pass. Manage it from the Visitors page, so their
            pass and their login stay in step.
          </p>

          <template v-if="wantsBadge && canHaveBadge && !badgeIsVisitor">
            <FormField
              :label="badge?.password_set ? 'Replace their password' : 'Password'"
              hint="Optional. Hand it over in person — it is never emailed. Set one if this install has no mail server, since an emailed code would otherwise be the only way in. They can change it from their badge."
            >
              <input
                v-model="badgePassword"
                type="text"
                autocomplete="off"
                placeholder="Leave blank for emailed-code sign-in only"
                class="input input-bordered font-mono"
              />
            </FormField>

            <label class="label cursor-pointer justify-start gap-3 p-0">
              <input v-model="badgeSendInvite" type="checkbox" class="checkbox checkbox-sm" />
              <span class="label-text">Email them where to sign in (never includes the password)</span>
            </label>

            <p v-if="badge" class="text-xs text-base-content/50">
              Signs in as <code>{{ badge.email }}</code
              >{{ badge.password_set ? ' with a password or an emailed code.' : ' with an emailed code.' }}
            </p>
          </template>

          <!-- Unreachable for a visitor: the checkbox is disabled for them. -->
          <p v-else-if="!wantsBadge && badge" class="text-xs text-warning">
            Saving will remove their badge login. Their credentials keep working — to revoke
            access, revoke the credential.
          </p>
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

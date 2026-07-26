<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'
import { useFileUrl } from '@/composables/useFileUrl'
import { withCardholderPassword } from '@/utils/cardholderPassword'
import { useAuthStore } from '@/stores/auth'
import type { Cardholder, CardholderStatus, Role, User } from '@/types/pocketbase'
import type { BadgeInviteResponse, BadgeKind } from '@/types/badge'
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
  // Badge-tier fields. They live in `form` with everything else because that is what
  // they are — columns on this row, not a related record. `kind` is read-only here
  // (a visitor is minted on the Visitors page) but must round-trip, or saving a
  // visitor from this form would silently demote them to an ordinary cardholder.
  badge_login: false,
  kind: '' as BadgeKind | '',
  // The operator account of the same human, if any (1750000040). A pointer, not a
  // merge: the two records stay two authorities. It lives on THIS side because the
  // `users` collection is self-writable, so a field there could be repointed by its
  // own owner to inherit someone else's badge.
  operator: '',
})

const roles = ref<Role[]>([])
const operators = ref<User[]>([])
/** Operator ids already claimed by some OTHER cardholder (one account, one badge). */
const linkedOperators = ref<Set<string>>(new Set())
const operatorTaken = computed(
  () => !!form.value.operator && linkedOperators.value.has(form.value.operator),
)
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
// One checkbox: "can view their badge on a phone". A badge login is 1:1 with a person
// because it IS the person — `badge_login` is a field on this record, gating the
// collection's AuthRule — so it belongs on this form as a property rather than as a
// related entity with a lifecycle of its own.
//
// It used to be a second record in a `badge_users` collection, reconciled after the save
// through POST /api/badge/holders. That route existed to centralise three things a caller
// must not get wrong (the `kind` discriminator, the throwaway password PocketBase demands
// on an auth record, and `password_set`); all three moved into the schema and a server
// hook, so the checkbox is now part of the ordinary update the collection rules govern.
//
// Both directions need `enroll`: whoever may hand out a badge login may take it back.
const canEnroll = computed(() => auth.can('enroll'))

// A login signs in by email — it is the sole identity field, and the only route an OTP
// or a reset can arrive by. Stated as a computed rather than a validation error because
// the honest UI is a disabled checkbox with a reason next to it. The server enforces the
// same rule (badgeapi.bindLoginRequiresEmail).
const canHaveBadge = computed(() => !!form.value.email.trim())
const isVisitor = computed(() => form.value.kind === 'visitor')

// An optional initial password, handed over in person. This is what makes the badge tier
// usable on an install with NO SMTP at all, since OTP and password-reset are both emails.
// Never mailed — see the invite text.
const badgePassword = ref('')
const badgeSendInvite = ref(false)
const hasPassword = ref(false)

// Turning the checkbox off must not leave a stale password to send along with it.
watch(
  () => form.value.badge_login,
  (on) => {
    if (!on) {
      badgePassword.value = ''
      badgeSendInvite.value = false
    }
  },
)

/**
 * Email the holder where their badge lives. A separate call, after the save, because
 * enabling a login and telling someone about it are genuinely separate acts: a rollout
 * to 500 people flips the flag by import and mails later, and an operator handing a
 * password over at a desk wants no mail at all.
 *
 * Reports its own failure without failing the save — the login works either way, and the
 * operator can always say it in person. Returns a message to show, or ''.
 */
async function sendInvite(cardholderId: string): Promise<string> {
  const res = await pb.send<BadgeInviteResponse>(`/api/badge/invite/${cardholderId}`, {
    method: 'POST',
  })
  if (!res.sent) {
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
  badgePassword: badgePassword.value,
  badgeSendInvite: badgeSendInvite.value,
}))

const kvKey = computed(() => (recordId ? `user.${recordId}` : ''))

async function loadOptions() {
  try {
    const [rs, ops, linked] = await Promise.all([
      pb.collection('roles').getFullList<Role>({ sort: 'code' }),
      pb.collection('users').getFullList<User>({ sort: 'email' }),
      // Which operators are already spoken for, so the form can say so before the
      // unique index refuses the save. Not a permission check — just a better message.
      pb.collection('cardholders').getFullList<Cardholder>({
        filter: 'operator != ""',
        fields: 'id,operator',
      }),
    ])
    roles.value = rs
    operators.value = ops
    linkedOperators.value = new Set(
      linked.filter((c) => c.id !== recordId).map((c) => c.operator || ''),
    )
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load options')
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
      badge_login: !!c.badge_login,
      kind: (c.kind || '') as BadgeKind | '',
      operator: c.operator || '',
    }
    hasPassword.value = !!c.password_set
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
    // Only touched by an operator who may: without `enroll` the checkbox is not
    // rendered, and sending these would be refused by the collection rule anyway.
    if (canEnroll.value) {
      data.badge_login = form.value.badge_login
      data.kind = form.value.kind
      data.operator = form.value.operator
      if (form.value.badge_login && badgePassword.value) {
        // PocketBase accepts a password on an auth record from anyone matching the
        // collection's ManageRule (`enroll`), with no old-password proof — which is
        // exactly the "operator sets it in person" case. passwordConfirm is required.
        data.password = badgePassword.value
        data.passwordConfirm = badgePassword.value
        // The holder now knows their password, so a later self-service change must
        // demand the current one. Getting this backwards would let a stolen session
        // change it unchallenged.
        data.password_set = true
      }
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
      // cardholders is an auth collection, so the create REQUEST needs a password
      // even for someone who will never sign in. See utils/cardholderPassword.
      const created = await pb.collection('cardholders').create<Cardholder>(withCardholderPassword(data))
      id = created.id
      toast.success('Cardholder created')
    }

    // The save is done — badge login included, since it is the same record. Only the
    // invite is a separate act, and its failure must not read as a failed save.
    if (canEnroll.value && form.value.badge_login && badgeSendInvite.value) {
      try {
        const warning = await sendInvite(id!)
        if (warning) toast.error(warning)
      } catch (err: any) {
        toast.error(
          `Saved, but the invite email could not be sent: ${err?.response?.message || err?.message || 'unknown error'}`,
        )
      }
    }
    badgePassword.value = ''

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

      <!-- Badge login: one field on this person, not a related entity. -->
      <BaseCard v-if="canEnroll" title="Badge login">
        <div class="space-y-4">
          <label class="label cursor-pointer justify-start gap-3 p-0" :class="{ 'opacity-50': !canHaveBadge }">
            <input
              v-model="form.badge_login"
              type="checkbox"
              class="checkbox"
              :disabled="!canHaveBadge"
            />
            <span class="label-text">Can view their badge on a phone</span>
          </label>
          <p class="text-xs text-base-content/60 -mt-2">
            Lets this person sign in to see their badge and use remote unlock where a door allows it.
            It grants no access of its own — their credentials decide that, and they keep working
            whether this is on or off.
          </p>

          <p v-if="!canHaveBadge" class="text-xs text-warning">
            Add an email address above first — it is the sign-in identity, and how a one-time code
            would reach them.
          </p>
          <p v-else-if="isVisitor" class="text-xs text-base-content/60">
            This person is a <strong>visitor</strong>. Their pass is managed on the Visitors page;
            turning this off here only removes their sign-in, it does not end the visit.
          </p>

          <template v-if="form.badge_login && canHaveBadge">
            <FormField
              :label="hasPassword ? 'Replace their password' : 'Password'"
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

            <p class="text-xs text-base-content/50">
              Signs in as <code>{{ form.email.trim() }}</code
              >{{ hasPassword || badgePassword ? ' with a password or an emailed code.' : ' with an emailed code.' }}
            </p>
          </template>

          <p v-else-if="!form.badge_login && hasPassword" class="text-xs text-warning">
            Saving will stop them signing in. Their credentials keep working — to take away
            access at the door, revoke the credential.
          </p>

          <!-- The two tiers stay two records; this only says they are the same human, so
               an operator can see their own badge from the console without a second
               sign-in. It grants nothing in either direction. -->
          <FormField
            label="Operator account"
            hint="If this person also signs in to the console, link their operator account here. It lets them view this badge from their profile menu. It grants no console access and no door access — both are decided where they already are."
          >
            <select v-model="form.operator" class="select select-bordered">
              <option value="">Not an operator</option>
              <option v-for="o in operators" :key="o.id" :value="o.id">
                {{ o.email }}{{ o.name ? ` — ${o.name}` : '' }}
              </option>
            </select>
            <p v-if="operatorTaken" class="text-xs text-warning mt-1">
              That operator is already linked to another cardholder. One account, one badge —
              saving will be refused.
            </p>
          </FormField>
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

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { pb } from '@/utils/pb'
import QrCode from '@/components/ui/QrCode.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import type { BadgeMe } from '@/types/badge'

/**
 * An operator's OWN badge, read-only.
 *
 * One human can hold an account in both tiers — the guard who badges through the door
 * and also runs the console. This shows that person their badge without making them sign
 * out and back in as themselves: GET /api/badge/me accepts an operator token and resolves
 * through `cardholders.operator` (migration 1750000040).
 *
 * # Why there are no buttons here
 *
 * Deliberately identity only — no unlock, no arm, no controls. An operator looking at
 * this is already inside a console with full command authority over every door, so
 * duplicating a subset of it behind a badge lens would add a second path to the same
 * act and make the audit trail ambiguous about which authority was used. The server
 * agrees rather than trusting this: every badge route that ACTUATES is bound to
 * `cardholders` alone, so an operator token cannot drive them even if asked to.
 *
 * A modal rather than a page because it is a thing you glance at and dismiss, not a
 * place you navigate to and lose your position on the console for.
 */
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()

const me = ref<BadgeMe | null>(null)
const loading = ref(false)
/** Set when the operator has no cardholder linked — an explanation, not an error. */
const noBadge = ref(false)
const loadError = ref('')
const photoUrl = ref('')

const grantCount = computed(() => {
  const m = me.value
  return m ? m.portals.length + m.areas.length + m.outputs.length : 0
})

const passTone = computed(() => (me.value?.passState === 'valid' ? 'success' : 'warning'))
const passLabel = computed(() => {
  switch (me.value?.passState) {
    case 'valid':
      return 'Pass valid'
    case 'expired':
      return 'Pass expired'
    case 'not_yet_valid':
      return 'Pass not started'
    case 'suspended':
      return 'Badge suspended'
    default:
      return 'No pass issued'
  }
})

async function load() {
  loading.value = true
  noBadge.value = false
  loadError.value = ''
  me.value = null
  photoUrl.value = ''
  try {
    me.value = await pb.send<BadgeMe>('/api/badge/me', { method: 'GET' })
    await loadPhoto()
  } catch (err: any) {
    // 404 is the honest answer to "show me my badge" when no cardholder is linked.
    if (err?.status === 404) noBadge.value = true
    else loadError.value = 'Could not load your badge.'
  } finally {
    loading.value = false
  }
}

/**
 * `cardholders.photo` is a PROTECTED file, so the URL needs a short-lived file token.
 * An operator passes the collection's view rule (migration 1750000027), which is what
 * makes the download succeed for this tier.
 */
async function loadPhoto() {
  const m = me.value
  if (!m?.photoFile || !m.photoRecord) return
  try {
    const token = await pb.files.getToken()
    photoUrl.value = pb.files.getURL(
      { id: m.photoRecord, collectionId: 'cardholders', collectionName: 'cardholders' },
      m.photoFile,
      { token, thumb: '400x400' },
    )
  } catch {
    photoUrl.value = '' // no photo beats a broken image
  }
}

// Fetch on open rather than on mount: most sessions never open this, and an install
// where no operator carries a card should pay nothing for the menu entry.
watch(() => props.open, (open) => { if (open) load() })

function close() {
  emit('update:open', false)
}
</script>

<template>
  <div v-if="open" class="modal modal-open" role="dialog">
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-lg mb-3">My badge</h3>

      <div v-if="loading" class="flex justify-center p-8">
        <span class="loading loading-spinner loading-lg"></span>
      </div>

      <div v-else-if="noBadge" class="space-y-2 text-sm">
        <p>No badge is linked to your operator account.</p>
        <p class="text-base-content/60">
          Your console sign-in and your badge are separate records, on purpose. Ask someone
          with the <span class="font-medium">enroll</span> capability to set the
          <span class="font-mono text-xs">operator</span> field on your cardholder record.
        </p>
      </div>

      <div v-else-if="loadError" class="alert alert-error text-sm">
        <span>{{ loadError }}</span>
        <button class="btn btn-xs" @click="load">Retry</button>
      </div>

      <div v-else-if="me" class="space-y-4">
        <div class="flex flex-col items-center text-center gap-3">
          <img
            v-if="photoUrl"
            :src="photoUrl"
            :alt="me.name"
            class="w-20 h-20 rounded-full object-cover bg-base-300"
          />
          <div
            v-else
            class="w-20 h-20 rounded-full bg-base-300 flex items-center justify-center text-xl font-semibold"
          >
            {{ (me.name || '?').slice(0, 1).toUpperCase() }}
          </div>

          <div>
            <div class="font-bold">{{ me.name || 'Cardholder' }}</div>
            <div class="text-xs text-base-content/60">{{ me.email }}</div>
          </div>

          <SoftBadge :tone="passTone" dot>{{ passLabel }}</SoftBadge>

          <!-- Whatever the server sent: an inert identifier for a staff badge, nothing
               at all for a suspended one. Never re-derived here. -->
          <template v-if="me.qr">
            <QrCode :value="me.qr" :size="176" />
            <p class="text-xs text-base-content/60">
              {{ me.qrSecret
                ? 'This code opens doors — treat it like a key.'
                : 'For identification. This code does not open doors.' }}
            </p>
          </template>
        </div>

        <!-- What the badge covers, as a count plus names. Read-only: acting on any of it
             belongs on the console, where it is audited as an operator action. -->
        <div v-if="grantCount" class="text-sm">
          <div class="font-medium mb-1">On this badge</div>
          <ul class="space-y-1 text-base-content/70">
            <li v-for="p in me.portals" :key="p.id" class="flex justify-between gap-2">
              <span class="truncate">{{ p.name }}</span>
              <span class="text-xs opacity-60 shrink-0">{{ p.location }}</span>
            </li>
            <li v-for="a in me.areas" :key="a.id" class="flex justify-between gap-2">
              <span class="truncate">{{ a.name }} <span class="opacity-50 text-xs">· area</span></span>
              <span class="text-xs opacity-60 shrink-0">{{ a.location }}</span>
            </li>
            <li v-for="o in me.outputs" :key="o.id" class="flex justify-between gap-2">
              <span class="truncate">{{ o.name }} <span class="opacity-50 text-xs">· control</span></span>
              <span class="text-xs opacity-60 shrink-0">{{ o.location }}</span>
            </li>
          </ul>
        </div>
        <p v-else class="text-sm text-base-content/50">Nothing is assigned to this badge yet.</p>
      </div>

      <div class="modal-action">
        <button class="btn btn-sm" @click="close">Close</button>
      </div>
    </div>
    <div class="modal-backdrop bg-black/40" @click="close"></div>
  </div>
</template>

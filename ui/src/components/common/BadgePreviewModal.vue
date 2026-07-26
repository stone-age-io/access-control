<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { pb } from '@/utils/pb'
import SoftBadge from '@/components/ui/SoftBadge.vue'
import BadgePassPanel from '@/views/badge/BadgePassPanel.vue'
import BadgeAccessPanel from '@/views/badge/BadgeAccessPanel.vue'
import type { BadgePreview } from '@/types/badge'

/**
 * What a cardholder's OWN badge says, shown to an operator for troubleshooting.
 *
 * # Why this exists
 *
 * "My pass doesn't work" is the support call, and almost every cause of it is invisible
 * from the operator side without cross-referencing four collections: no credential issued,
 * a window that has not opened, a suspended person, a role whose group grants nothing, a
 * door that grants in person but not remotely, a `badge_login` never ticked. The badge
 * already reduces all of that to one sentence and one list, so the cheapest honest answer
 * is to render the holder's actual payload.
 *
 * # Why it renders the badge's own components
 *
 * BadgePassPanel and BadgeAccessPanel, from GET /api/badge/preview/{id} — which returns
 * byte-for-byte what /me and /live return to the holder. A lookalike built from collection
 * reads would send an operator hunting for a discrepancy that is in the preview rather than
 * in the policy graph. Here, if it looks wrong, it IS wrong.
 *
 * # Why it is read-only, and what that costs
 *
 * The server mints no session, so `readonly` on the Access panel is not a restriction being
 * enforced here — there is simply nothing to act with. That was the deliberate choice over
 * a real impersonation token: badge actions stamp the CARDHOLDER as actor, so an operator
 * driving a borrowed badge session would write audit rows indistinguishable from the
 * holder's own, and "did this visitor open the loading bay, or someone checking on them?"
 * would stop being answerable from the log.
 *
 * The cost, stated plainly: this cannot prove a holder's unlock button works end-to-end. It
 * proves what the server would decide. An operator who needs the door opened uses the
 * command routes, where it is audited under their own `command` capability.
 */
const props = defineProps<{ open: boolean; cardholderId: string; name?: string }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()

const preview = ref<BadgePreview | null>(null)
const loading = ref(false)
const loadError = ref('')
const tab = ref<'badge' | 'access'>('badge')

const actionCount = computed(() => {
  const m = preview.value?.me
  return m ? m.portals.length + m.areas.length + m.outputs.length : 0
})

/**
 * The operator-only diagnosis: the reasons a badge can be healthy and still unusable, or
 * unreachable, stated as chips rather than left to be inferred from a form.
 */
const diagnostics = computed(() => {
  const p = preview.value
  if (!p) return []
  const out: { label: string; tone: 'success' | 'warning' | 'error' | 'neutral' }[] = []

  // The commonest cause of "I can't get in" that the badge payload cannot show: without a
  // login they never reach a badge at all, so `me` looks perfectly healthy.
  out.push(
    p.badgeLogin
      ? { label: 'Can sign in', tone: 'success' }
      : { label: 'No badge login', tone: 'error' },
  )
  // Which matters on an install with no SMTP: OTP and password reset are both emails, so a
  // holder with no password has no route in at all.
  if (p.badgeLogin) {
    out.push(
      p.passwordSet
        ? { label: 'Has a password', tone: 'success' }
        : { label: 'Emailed code only', tone: 'neutral' },
    )
  }
  if (p.status && p.status !== 'active') {
    out.push({ label: `Cardholder ${p.status}`, tone: 'warning' })
  }
  return out
})

async function load() {
  loading.value = true
  loadError.value = ''
  preview.value = null
  tab.value = 'badge'
  try {
    preview.value = await pb.send<BadgePreview>(`/api/badge/preview/${props.cardholderId}`, {
      method: 'GET',
    })
  } catch (err: any) {
    loadError.value =
      err?.status === 403
        ? 'Viewing a badge needs the enroll capability.'
        : err?.response?.message || 'Could not load this badge.'
  } finally {
    loading.value = false
  }
}

// Fetch on open, not on mount: this is audited as a look at a person, so it must happen
// only when an operator actually asks.
watch(
  () => props.open,
  (open) => {
    if (open && props.cardholderId) load()
  },
)

function close() {
  emit('update:open', false)
}
</script>

<template>
  <div v-if="open" class="modal modal-open" role="dialog">
    <div class="modal-box max-w-md p-0 flex flex-col max-h-[90vh]">
      <div class="shrink-0 px-5 pt-5 pb-3 border-b border-base-200">
        <h3 class="font-bold text-lg">{{ name || 'Badge' }}</h3>
        <p class="text-xs text-base-content/60">
          Exactly what this person's own badge shows them. Read-only — nothing here can be
          used from the console.
        </p>
      </div>

      <div class="flex-1 min-h-0 overflow-y-auto p-4 bg-base-200">
        <div v-if="loading" class="flex justify-center p-10">
          <span class="loading loading-spinner loading-lg"></span>
        </div>

        <div v-else-if="loadError" class="alert alert-error text-sm">
          <span>{{ loadError }}</span>
          <button class="btn btn-xs" @click="load">Retry</button>
        </div>

        <div v-else-if="preview" class="space-y-3">
          <!-- The operator-only half: why a badge that renders correctly may still not work. -->
          <div v-if="diagnostics.length" class="flex flex-wrap gap-1">
            <SoftBadge v-for="d in diagnostics" :key="d.label" :tone="d.tone" dot>
              {{ d.label }}
            </SoftBadge>
          </div>

          <div role="tablist" class="tabs tabs-boxed tabs-sm">
            <button
              role="tab"
              class="tab flex-1"
              :class="tab === 'badge' ? 'tab-active' : ''"
              @click="tab = 'badge'"
            >
              Badge
            </button>
            <button
              role="tab"
              class="tab flex-1 gap-1"
              :class="tab === 'access' ? 'tab-active' : ''"
              @click="tab = 'access'"
            >
              Access
              <span v-if="actionCount" class="badge badge-sm">{{ actionCount }}</span>
            </button>
          </div>

          <!-- The badge's own components, on the badge's own payload. The operator client is
               passed so the PROTECTED photo resolves against this session's file token. -->
          <BadgePassPanel v-if="tab === 'badge'" :me="preview.me" :client="pb" compact />
          <BadgeAccessPanel
            v-else
            :me="preview.me"
            :plans="preview.live.locations"
            readonly
          />
        </div>
      </div>

      <div class="shrink-0 px-5 py-3 border-t border-base-200 flex justify-end">
        <button class="btn btn-sm" @click="close">Close</button>
      </div>
    </div>
    <div class="modal-backdrop bg-black/40" @click="close"></div>
  </div>
</template>

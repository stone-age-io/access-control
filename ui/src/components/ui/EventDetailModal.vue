<script setup lang="ts">
/**
 * The one event-detail dialog, shared by the Events page, the Overview's recent
 * activity, and the Alarm Console. Renders nothing when `event` is null; the
 * caller owns the selected event and clears it on `close`.
 */
import { formatDate, formatConstant } from '@/utils/format'
import { eventKindBadge } from '@/utils/events'
import type { AccessEvent } from '@/types/pocketbase'

defineProps<{ event: AccessEvent | null }>()
const emit = defineEmits<{ close: [] }>()
</script>

<template>
  <Teleport to="body">
    <dialog class="modal" :class="{ 'modal-open': !!event }">
      <div class="modal-box max-w-2xl" v-if="event">
        <div class="flex justify-between items-center mb-4">
          <h3 class="font-bold text-lg flex items-center gap-2">
            <span class="badge" :class="eventKindBadge(event)">{{ event.kind }}</span>
            Event Detail
          </h3>
          <button @click="emit('close')" class="btn btn-sm btn-circle btn-ghost">✕</button>
        </div>

        <div class="grid grid-cols-2 gap-x-4 gap-y-2 text-sm mb-4">
          <div><span class="opacity-50 text-xs uppercase block">Time</span>{{ formatDate(event.ts || event.created, 'PPpp') }}</div>
          <div><span class="opacity-50 text-xs uppercase block">Location</span><code>{{ event.location || '-' }}</code></div>
          <div><span class="opacity-50 text-xs uppercase block">Portal</span><code>{{ event.portal || '-' }}</code></div>
          <div><span class="opacity-50 text-xs uppercase block">Allow</span>
            <span v-if="event.kind === 'tap'" class="badge badge-sm" :class="event.allow ? 'badge-success' : 'badge-error'">{{ event.allow ? 'allow' : 'deny' }}</span>
            <span v-else class="opacity-40">n/a</span>
          </div>
          <div><span class="opacity-50 text-xs uppercase block">Credential</span><code>{{ event.credential || '-' }}</code></div>
          <div><span class="opacity-50 text-xs uppercase block">User</span><code>{{ event.user || '-' }}</code></div>
          <div><span class="opacity-50 text-xs uppercase block">Source</span>
            <span v-if="event.source" class="badge badge-sm badge-ghost">{{ event.source }}</span>
            <span v-else class="opacity-40">n/a</span>
          </div>
          <div class="col-span-2"><span class="opacity-50 text-xs uppercase block">Reason</span>{{ event.reason ? formatConstant(event.reason) : '-' }}</div>
        </div>

        <div class="bg-base-200 rounded-box overflow-hidden">
          <div class="px-3 py-2 text-xs font-medium opacity-60 border-b border-base-300">Payload</div>
          <pre class="p-3 text-xs overflow-x-auto"><code>{{ JSON.stringify(event.payload ?? {}, null, 2) }}</code></pre>
        </div>

        <div class="modal-action">
          <button class="btn" @click="emit('close')">Close</button>
        </div>
      </div>
      <div class="modal-backdrop" @click="emit('close')"></div>
    </dialog>
  </Teleport>
</template>

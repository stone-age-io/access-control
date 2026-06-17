<script setup lang="ts">
/** Standard rail card: the policy KV key plus record timestamps and id. */
import { formatDate, formatRelativeTime } from '@/utils/format'

defineProps<{
  record: { id: string; created: string; updated: string }
  kvKey?: string
}>()
</script>

<template>
  <div class="card bg-base-100 shadow-md border border-base-300/60">
    <div class="card-body p-4 gap-3">
      <h3 class="text-xs font-bold uppercase tracking-wider opacity-60 flex items-center gap-2">
        <span>🔑</span>Record
      </h3>

      <div v-if="kvKey">
        <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide mb-1">Policy KV key</div>
        <code class="text-xs font-mono break-all bg-base-200 px-2 py-1 rounded block">{{ kvKey }}</code>
      </div>

      <div class="grid grid-cols-1 gap-2.5">
        <div>
          <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide">Created</div>
          <div class="text-sm" :title="formatDate(record.created)">{{ formatRelativeTime(record.created) }}</div>
        </div>
        <div>
          <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide">Updated</div>
          <div class="text-sm" :title="formatDate(record.updated)">{{ formatRelativeTime(record.updated) }}</div>
        </div>
        <div>
          <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide">ID</div>
          <code class="text-xs opacity-70 break-all">{{ record.id }}</code>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * Compact record-meta strip for the bottom of a single-column detail view:
 * the policy KV key plus timestamps and id, low-emphasis and inline. Mirrors
 * FormLayout's inline KV-key styling.
 */
import { formatDate, formatRelativeTime } from '@/utils/format'

defineProps<{
  record: { id: string; created: string; updated: string }
  kvKey?: string
}>()
</script>

<template>
  <div class="flex flex-wrap items-center gap-x-6 gap-y-2 px-1 pt-2 text-xs text-base-content/50">
    <div v-if="kvKey" class="flex items-center gap-2 min-w-0">
      <span class="font-semibold uppercase tracking-wide">Policy KV key</span>
      <code class="font-mono bg-base-200 px-2 py-0.5 rounded break-all text-base-content/70">{{ kvKey }}</code>
    </div>
    <div>
      <span class="font-semibold uppercase tracking-wide">Created</span>
      <span class="ml-1" :title="formatDate(record.created)">{{ formatRelativeTime(record.created) }}</span>
    </div>
    <div>
      <span class="font-semibold uppercase tracking-wide">Updated</span>
      <span class="ml-1" :title="formatDate(record.updated)">{{ formatRelativeTime(record.updated) }}</span>
    </div>
    <div>
      <span class="font-semibold uppercase tracking-wide">ID</span>
      <code class="ml-1 opacity-80">{{ record.id }}</code>
    </div>
  </div>
</template>

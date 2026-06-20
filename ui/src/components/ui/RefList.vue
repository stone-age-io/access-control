<script setup lang="ts" generic="T extends { id: string }">
/**
 * A compact list of related records for the rail — forward relations
 * ("Access points in this group") or reverse references ("Used by these roles").
 * Each row links to that record's detail view.
 */
defineProps<{
  title: string
  icon?: string
  items: T[]
  to: (item: T) => string
  primary: (item: T) => string
  secondary?: (item: T) => string
  empty?: string
  loading?: boolean
}>()
</script>

<template>
  <div class="card bg-base-100 border border-base-300 shadow-sm">
    <div class="card-body p-4 gap-2">
      <div class="flex items-center justify-between">
        <h3 class="text-xs font-bold uppercase tracking-wider opacity-60 flex items-center gap-2">
          <span v-if="icon">{{ icon }}</span>{{ title }}
        </h3>
        <span v-if="!loading" class="badge badge-ghost badge-sm">{{ items.length }}</span>
      </div>

      <div v-if="loading" class="py-2">
        <span class="loading loading-dots loading-sm opacity-40"></span>
      </div>
      <p v-else-if="items.length === 0" class="text-sm opacity-50 py-1">{{ empty || 'None' }}</p>
      <ul v-else class="divide-y divide-base-200/70 -mx-1">
        <li v-for="item in items" :key="item.id">
          <router-link
            :to="to(item)"
            class="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-base-200 transition-colors"
          >
            <code class="text-xs font-medium text-primary truncate">{{ primary(item) }}</code>
            <span v-if="secondary && secondary(item)" class="text-xs opacity-50 truncate">{{ secondary(item) }}</span>
          </router-link>
        </li>
      </ul>
    </div>
  </div>
</template>

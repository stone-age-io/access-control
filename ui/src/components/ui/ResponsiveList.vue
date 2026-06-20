<script setup lang="ts" generic="T extends { id: string }">

export interface Column<T = any> {
  key: string
  label: string
  format?: (value: any, item: T) => string
  class?: string
  mobileLabel?: string
}

interface Props {
  items: T[]
  columns: Column<T>[]
  loading?: boolean
  clickable?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  clickable: true,
})

const emit = defineEmits<{
  'row-click': [item: T]
}>()

function get(obj: any, path: string): any {
  return path.split('.').reduce((acc, part) => acc?.[part], obj)
}

function handleClick(item: T) {
  if (props.clickable) emit('row-click', item)
}

function handleKey(e: KeyboardEvent, item: T) {
  if (!props.clickable) return
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    emit('row-click', item)
  }
}
</script>

<template>
  <div class="w-full">
    <!-- DESKTOP: table -->
    <div class="hidden lg:block overflow-x-auto">
      <table class="table table-sm w-full">
        <thead>
          <tr class="border-b border-base-300">
            <th v-for="col in columns" :key="col.key" :class="col.class" class="text-[11px] uppercase tracking-wider opacity-60">
              {{ col.label }}
            </th>
            <th v-if="$slots.actions" class="text-right text-[11px] uppercase tracking-wider opacity-60">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in items"
            :key="item.id"
            :class="{ 'hover cursor-pointer': clickable }"
            class="border-b border-base-200/50 last:border-0 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary/60"
            :tabindex="clickable ? 0 : undefined"
            :role="clickable ? 'button' : undefined"
            @click="handleClick(item)"
            @keydown="handleKey($event, item)"
          >
            <td v-for="col in columns" :key="col.key" :class="col.class" class="py-3">
              <slot :name="`cell-${col.key}`" :item="item" :value="get(item, col.key)">
                <span class="text-sm">
                  {{ col.format ? col.format(get(item, col.key), item) : get(item, col.key) || '-' }}
                </span>
              </slot>
            </td>
            <td v-if="$slots.actions" @click.stop class="py-3">
              <div class="flex justify-end gap-2">
                <slot name="actions" :item="item" />
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- MOBILE: high-density cards -->
    <div class="lg:hidden space-y-2">
      <div
        v-for="item in items"
        :key="item.id"
        :class="[
          'card bg-base-100 border border-base-300 shadow-sm transition-all duration-200',
          { 'cursor-pointer active:scale-[0.98] hover:border-primary/40 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary/60': clickable },
        ]"
        :tabindex="clickable ? 0 : undefined"
        :role="clickable ? 'button' : undefined"
        @click="handleClick(item)"
        @keydown="handleKey($event, item)"
      >
        <div class="card-body p-3">
          <div class="mb-1.5">
            <slot :name="`card-${columns[0].key}`" :item="item" :value="get(item, columns[0].key)">
              <div class="text-sm font-bold text-primary truncate">
                {{ columns[0].format ? columns[0].format(get(item, columns[0].key), item) : get(item, columns[0].key) || 'Unnamed' }}
              </div>
            </slot>
          </div>

          <div class="grid grid-cols-2 gap-x-3 gap-y-1.5 border-t border-base-200/60 pt-2">
            <div
              v-for="col in columns.slice(1)"
              :key="col.key"
              :class="col.class"
              class="flex items-center gap-1.5 overflow-hidden"
            >
              <span class="text-[10px] uppercase font-bold opacity-50 tracking-tight shrink-0">
                {{ col.mobileLabel || col.label }}:
              </span>
              <div class="flex-1 truncate">
                <slot :name="`card-${col.key}`" :item="item" :value="get(item, col.key)">
                  <span class="text-xs font-medium text-base-content/80">
                    {{ col.format ? col.format(get(item, col.key), item) : get(item, col.key) || '-' }}
                  </span>
                </slot>
              </div>
            </div>
          </div>

          <div v-if="$slots.actions" class="flex justify-end items-center gap-1 mt-2 pt-2 border-t border-base-200/60" @click.stop>
            <slot name="actions" :item="item" />
          </div>
        </div>
      </div>
    </div>

    <!-- EMPTY & LOADING -->
    <div v-if="items.length === 0 && !loading" class="text-center py-12 bg-base-200/30 rounded-xl border-2 border-dashed border-base-300">
      <slot name="empty">
        <div class="flex flex-col items-center gap-2 opacity-40">
          <span class="text-4xl">📭</span>
          <span class="text-sm font-bold uppercase tracking-widest">No items found</span>
        </div>
      </slot>
    </div>

    <div v-if="loading" class="flex justify-center p-4">
      <span class="loading loading-dots loading-md opacity-30"></span>
    </div>
  </div>
</template>

<style scoped>
.truncate {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.card-body {
  min-height: unset;
}
</style>

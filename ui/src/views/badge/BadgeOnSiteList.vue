<script setup lang="ts">
import { computed, ref, watch } from 'vue'

/**
 * Everything on a badge that can only be used IN PERSON — doors without remote unlock,
 * areas without remote arming, controls without remote driving.
 *
 * # Why it is listed at all
 *
 * The grant is real. A holder who could not see it would believe their badge does less
 * than it does, and would stand at a door they are entitled to open wondering why it is
 * not on their phone. So it is shown, and labelled as in-person.
 *
 * # Why it is shaped like this
 *
 * It was a flat `<ul>` of every item, which is fine for the common badge (a couple of
 * doors at one site) and wrong for the badge that needs it most: a contractor or a
 * facilities manager with thirty doors across four buildings got one unbroken list, taller
 * than the screen, in no order they cared about.
 *
 * So: grouped by location, because "which of my doors are in THIS building" is the only
 * question a list of thirty answers; collapsible, because a holder standing at one site
 * has no use for the other three; a filter, because past a certain size you know the name
 * of what you are looking for; and bounded in height with its own scroll, so it cannot
 * push the rest of the badge off the screen.
 *
 * Deliberately NOT actionable. Nothing in here can be driven remotely — that is what
 * makes it this list rather than the ones above it — so it stays a reference, and there
 * are no buttons to mis-tap.
 */

/** One in-person-only thing. `kind` is the label, not a type discriminator with behaviour. */
export interface OnSiteItem {
  id: string
  name: string
  location: string
  kind: 'door' | 'area' | 'control'
}

const props = defineProps<{ items: OnSiteItem[]; passUsable: boolean }>()

/** Offered only once a list is big enough to be worth searching. */
const FILTER_THRESHOLD = 6

const query = ref('')
/** Location names the holder has collapsed. */
const collapsed = ref<Set<string>>(new Set())

const KIND_LABEL: Record<OnSiteItem['kind'], string> = {
  door: 'door',
  area: 'area',
  control: 'control',
}

/** '' is a real possibility — a portal whose location record has no name resolved. */
function locationOf(item: OnSiteItem): string {
  return item.location || 'Other'
}

const filtered = computed<OnSiteItem[]>(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return props.items
  return props.items.filter((i) =>
    `${i.name} ${i.location} ${KIND_LABEL[i.kind]}`.toLowerCase().includes(q),
  )
})

interface Group {
  location: string
  items: OnSiteItem[]
}

const groups = computed<Group[]>(() => {
  const byLocation = new Map<string, OnSiteItem[]>()
  for (const item of filtered.value) {
    const key = locationOf(item)
    const bucket = byLocation.get(key)
    if (bucket) bucket.push(item)
    else byLocation.set(key, [item])
  }
  return [...byLocation.entries()]
    .map(([location, items]) => ({
      location,
      items: [...items].sort((a, b) => a.name.localeCompare(b.name)),
    }))
    .sort((a, b) => a.location.localeCompare(b.location))
})

/** Every location present, ignoring the filter — what "collapse all" applies to. */
const allLocations = computed(() => [...new Set(props.items.map(locationOf))])

const showFilter = computed(() => props.items.length >= FILTER_THRESHOLD)

/**
 * A single site starts open (collapsing it would hide everything for no gain); several
 * start closed, so the holder sees the shape of their access — which buildings, how many
 * in each — before any of the detail.
 *
 * Keyed on the location LIST rather than its length, so a badge that gains a site
 * re-evaluates rather than staying however the last one was left. Joined on a character no
 * location name can contain, so ["A B", "C"] and ["A", "B C"] are not the same key.
 */
watch(
  () => allLocations.value.join('\u0000'),
  () => {
    collapsed.value = allLocations.value.length > 1 ? new Set(allLocations.value) : new Set()
  },
  { immediate: true },
)

/** While filtering, everything is open: a hit inside a collapsed group is a hit nobody sees. */
function isOpen(location: string): boolean {
  return !!query.value.trim() || !collapsed.value.has(location)
}

function toggle(location: string) {
  const next = new Set(collapsed.value)
  if (next.has(location)) next.delete(location)
  else next.add(location)
  collapsed.value = next
}
</script>

<template>
  <div class="card bg-base-100 shadow-sm">
    <div class="card-body gap-2 p-4">
      <div class="flex items-baseline justify-between gap-2">
        <h2 class="card-title text-base">On site only</h2>
        <span class="text-xs text-base-content/50">{{ items.length }}</span>
      </div>
      <p class="text-xs text-base-content/60">
        {{
          passUsable
            ? 'Present your badge, or use the keypad, at these.'
            : 'These are on your badge but need a valid pass.'
        }}
      </p>

      <label v-if="showFilter" class="input input-bordered input-sm flex items-center gap-2">
        <span class="opacity-40 text-xs">🔍</span>
        <input
          v-model="query"
          type="search"
          class="grow min-w-0 bg-transparent outline-none text-sm"
          placeholder="Filter by name or building"
          aria-label="Filter your in-person access"
        />
        <button
          v-if="query"
          type="button"
          class="btn btn-ghost btn-xs btn-circle"
          aria-label="Clear filter"
          @click="query = ''"
        >
          ✕
        </button>
      </label>

      <!-- Bounded with its own scroll so the filter above stays reachable: on a badge with
           thirty doors, a filter you have to scroll back up to find is a filter nobody uses.
           A viewport fraction rather than a fixed 14rem, because this is now one view of a
           switcher rather than one card among four — the old bound left most of the screen
           empty while the list inside it scrolled. -->
      <div class="max-h-[55vh] overflow-y-auto overscroll-contain -mx-1 px-1">
        <p v-if="!groups.length" class="text-sm text-base-content/50 py-4 text-center">
          Nothing matches “{{ query }}”.
        </p>

        <div v-for="group in groups" :key="group.location" class="border-b border-base-200 last:border-0">
          <button
            type="button"
            class="w-full flex items-center justify-between gap-2 py-2 text-left"
            :aria-expanded="isOpen(group.location)"
            @click="toggle(group.location)"
          >
            <span class="text-sm font-medium truncate">{{ group.location }}</span>
            <span class="flex items-center gap-2 shrink-0">
              <span class="badge badge-sm badge-ghost">{{ group.items.length }}</span>
              <span
                class="text-xs opacity-50 transition-transform"
                :class="isOpen(group.location) ? 'rotate-90' : ''"
                >▶</span
              >
            </span>
          </button>

          <ul v-if="isOpen(group.location)" class="pb-2 space-y-1">
            <li
              v-for="item in group.items"
              :key="item.id"
              class="flex items-baseline justify-between gap-2 text-sm pl-2"
            >
              <span class="truncate">{{ item.name }}</span>
              <span class="text-[10px] uppercase tracking-wide text-base-content/40 shrink-0">
                {{ KIND_LABEL[item.kind] }}
              </span>
            </li>
          </ul>
        </div>
      </div>
    </div>
  </div>
</template>

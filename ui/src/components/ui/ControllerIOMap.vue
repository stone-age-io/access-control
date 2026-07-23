<script setup lang="ts">
/**
 * Occupancy map for a controller: every logical relay and input the model defines,
 * with what currently drives/monitors it (portal lock/DPS/REX or an aux point), the
 * physical line, and a conflict flag when more than one record claims a line.
 *
 * Answers "what's wired, what's free, what collides" for one box at a glance.
 */
import { computed } from 'vue'
import type { ModelProfile } from '@/utils/models'
import type { ControllerIO, Occupant } from '@/utils/io'
import BaseCard from '@/components/ui/BaseCard.vue'
import SoftBadge from '@/components/ui/SoftBadge.vue'

const props = defineProps<{ profile: ModelProfile | null; io: ControllerIO }>()

interface Row {
  index: number
  label: string
  occ: Occupant[]
}

function rows(lines: ModelProfile['relays'], usage: Map<number, Occupant[]>): Row[] {
  return lines.map((l) => ({ index: l.index, label: l.label, occ: usage.get(l.index) || [] }))
}

const relayRows = computed(() => (props.profile ? rows(props.profile.relays, props.io.relays) : []))
const inputRows = computed(() => (props.profile ? rows(props.profile.inputs, props.io.inputs) : []))
</script>

<template>
  <BaseCard title="I/O map">
    <p v-if="!profile" class="text-sm opacity-60">
      Set this controller's model to see its relay and input map.
    </p>

    <div v-else class="grid grid-cols-1 lg:grid-cols-2 gap-x-10 gap-y-6">
      <!-- Relays -->
      <div>
        <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide mb-1 px-1">
          Relays · {{ relayRows.length }}
        </div>
        <ul class="divide-y divide-base-200">
          <li
            v-for="r in relayRows"
            :key="r.index"
            class="flex items-center gap-2 py-1.5 px-1"
            :class="r.occ.length > 1 ? 'text-error' : ''"
          >
            <SoftBadge class="font-mono w-7 justify-center shrink-0">{{ r.index }}</SoftBadge>
            <span v-if="r.occ.length > 1" class="shrink-0" title="More than one record claims this line">⚠</span>
            <div class="flex-1 min-w-0 flex flex-wrap gap-x-2">
              <template v-if="r.occ.length">
                <router-link
                  v-for="(o, i) in r.occ"
                  :key="i"
                  :to="o.to"
                  class="link link-hover text-sm truncate"
                >{{ o.label }}</router-link>
              </template>
              <span v-else class="text-sm opacity-40">free</span>
            </div>
            <code class="text-xs opacity-50 shrink-0">{{ r.label }}</code>
          </li>
        </ul>
      </div>

      <!-- Inputs -->
      <div>
        <div class="text-[10px] uppercase font-bold opacity-50 tracking-wide mb-1 px-1">
          Inputs · {{ inputRows.length }}
        </div>
        <ul class="divide-y divide-base-200">
          <li
            v-for="r in inputRows"
            :key="r.index"
            class="flex items-center gap-2 py-1.5 px-1"
            :class="r.occ.length > 1 ? 'text-error' : ''"
          >
            <SoftBadge class="font-mono w-7 justify-center shrink-0">{{ r.index }}</SoftBadge>
            <span v-if="r.occ.length > 1" class="shrink-0" title="More than one record claims this line">⚠</span>
            <div class="flex-1 min-w-0 flex flex-wrap gap-x-2">
              <template v-if="r.occ.length">
                <router-link
                  v-for="(o, i) in r.occ"
                  :key="i"
                  :to="o.to"
                  class="link link-hover text-sm truncate"
                >{{ o.label }}</router-link>
              </template>
              <span v-else class="text-sm opacity-40">free</span>
            </div>
            <code class="text-xs opacity-50 shrink-0">{{ r.label }}</code>
          </li>
        </ul>
      </div>
    </div>
  </BaseCard>
</template>

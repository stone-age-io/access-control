<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import ListLayout from '@/components/ui/ListLayout.vue'
import DeniedAccessReport from './DeniedAccessReport.vue'
import WhoHasAccessReport from './WhoHasAccessReport.vue'
import WiringReport from './WiringReport.vue'
import AccessSimulatorReport from './AccessSimulatorReport.vue'

// One Reports page; each report is a sub-tab backed by a route param so it
// deep-links and survives reload (/reports/:tab). The parent /reports redirects
// to the first tab (see the router).
const TABS = [
  { key: 'denied-access', label: 'Denied Access', icon: '🚫' },
  { key: 'who-has-access', label: 'Who Has Access', icon: '🔑' },
  { key: 'wiring', label: 'Wiring / As-Built', icon: '🔧' },
  { key: 'simulator', label: 'Access Simulator', icon: '🧪' },
] as const

const route = useRoute()
const router = useRouter()

const active = computed(() => {
  const t = route.params.tab as string
  return TABS.some((x) => x.key === t) ? t : 'denied-access'
})

function select(key: string) {
  if (key !== route.params.tab) router.replace(`/reports/${key}`)
}
</script>

<template>
  <ListLayout
    title="Reports"
    subtitle="Read-only views over the policy graph and event stream — exportable for audits and handoff."
  >
    <!-- Wrapping segmented control: full labels, no horizontal scroll. Plain
         buttons rather than DaisyUI tabs-boxed, whose fixed-height tabs clipped
         the labels (emoji-only) and double-scrolled once four wide tabs overflowed
         a narrow viewport. -->
    <div role="tablist" class="flex flex-wrap gap-2">
      <button
        v-for="t in TABS"
        :key="t.key"
        type="button"
        role="tab"
        :aria-selected="active === t.key"
        class="inline-flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium whitespace-nowrap transition-colors"
        :class="active === t.key
          ? 'bg-primary text-primary-content shadow-sm'
          : 'bg-base-200 text-base-content/70 hover:bg-base-300'"
        @click="select(t.key)"
      >
        <span aria-hidden="true">{{ t.icon }}</span>
        <span>{{ t.label }}</span>
      </button>
    </div>

    <DeniedAccessReport v-if="active === 'denied-access'" />
    <WhoHasAccessReport v-else-if="active === 'who-has-access'" />
    <WiringReport v-else-if="active === 'wiring'" />
    <AccessSimulatorReport v-else-if="active === 'simulator'" />
  </ListLayout>
</template>

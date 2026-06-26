<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import HelpButton from '@/components/ui/HelpButton.vue'
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
  <div class="space-y-6">
    <div class="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
      <div>
        <h1 class="text-3xl font-bold">Reports</h1>
        <p class="text-base-content/70 mt-1">Read-only views over the policy graph and event stream — exportable for audits and handoff.</p>
      </div>
      <HelpButton />
    </div>

    <div role="tablist" class="tabs tabs-boxed w-fit max-w-full overflow-x-auto">
      <button
        v-for="t in TABS"
        :key="t.key"
        role="tab"
        class="tab gap-2 whitespace-nowrap"
        :class="{ 'tab-active': active === t.key }"
        @click="select(t.key)"
      >
        <span>{{ t.icon }}</span>
        <span>{{ t.label }}</span>
      </button>
    </div>

    <DeniedAccessReport v-if="active === 'denied-access'" />
    <WhoHasAccessReport v-else-if="active === 'who-has-access'" />
    <WiringReport v-else-if="active === 'wiring'" />
    <AccessSimulatorReport v-else-if="active === 'simulator'" />
  </div>
</template>

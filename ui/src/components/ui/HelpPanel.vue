<script setup lang="ts">
/**
 * Right-side slide-over that renders the current route's help. Mounted once in
 * MainLayout; opened by HelpButton (desktop) or the AppHeader help icon (mobile). Content is
 * resolved from the active route, so it tracks navigation automatically.
 */
import { computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useEventListener } from '@vueuse/core'
import { useHelp } from '@/composables/useHelp'
import { helpForPath } from '@/help/registry'

const route = useRoute()
const { isOpen, close } = useHelp()
const topic = computed(() => helpForPath(route.path))

// Close on route change (the help may not exist on the destination).
watch(() => route.path, close)
// Close on Escape.
useEventListener(document, 'keydown', (e: KeyboardEvent) => {
  if (e.key === 'Escape' && isOpen.value) close()
})
</script>

<template>
  <Teleport to="body">
    <Transition name="help-fade">
      <div v-if="isOpen && topic" class="fixed inset-0 z-[80]" role="dialog" aria-modal="true" aria-label="Help">
        <!-- Backdrop -->
        <div class="absolute inset-0 bg-black/40 backdrop-blur-[1px]" @click="close"></div>

        <!-- Panel -->
        <Transition name="help-slide" appear>
          <aside
            class="absolute right-0 top-0 h-full w-full max-w-md bg-base-100 shadow-2xl border-l border-base-300 flex flex-col"
          >
            <header class="flex items-center justify-between gap-3 px-5 py-4 border-b border-base-300">
              <h2 class="text-lg font-bold flex items-center gap-2 min-w-0">
                <span class="text-xl flex-shrink-0">{{ topic.icon }}</span>
                <span class="truncate">{{ topic.title }}</span>
              </h2>
              <button class="btn btn-sm btn-circle btn-ghost flex-shrink-0" aria-label="Close help" @click="close">
                <svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </header>

            <div class="flex-1 overflow-y-auto px-5 py-4 space-y-5">
              <section v-for="(s, i) in topic.sections" :key="i" class="space-y-2">
                <h3 v-if="s.heading" class="text-xs font-bold uppercase tracking-wider opacity-60">{{ s.heading }}</h3>
                <p v-if="s.body" class="text-sm leading-relaxed text-base-content/80">{{ s.body }}</p>
                <dl v-if="s.items?.length" class="space-y-1.5">
                  <div v-for="(it, j) in s.items" :key="j" class="grid grid-cols-[7rem_1fr] gap-2 items-baseline">
                    <dt class="text-xs font-semibold text-base-content/90">{{ it.term }}</dt>
                    <dd class="text-xs leading-relaxed text-base-content/70">{{ it.def }}</dd>
                  </div>
                </dl>
              </section>
            </div>
          </aside>
        </Transition>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.help-fade-enter-active,
.help-fade-leave-active {
  transition: opacity 0.2s ease;
}
.help-fade-enter-from,
.help-fade-leave-to {
  opacity: 0;
}
.help-slide-enter-active,
.help-slide-leave-active {
  transition: transform 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}
.help-slide-enter-from,
.help-slide-leave-to {
  transform: translateX(100%);
}
</style>

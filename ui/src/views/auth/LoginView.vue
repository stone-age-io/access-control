<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useBrandingStore } from '@/stores/branding'
import BrandLogo from '@/components/common/BrandLogo.vue'
import OperatorSignIn from './OperatorSignIn.vue'
import BadgeSignIn from './BadgeSignIn.vue'

/**
 * The one sign-in page, for both tiers.
 *
 * # Why a selector rather than one form
 *
 * PocketBase authenticates per collection, so something has to choose between `users`
 * (the operator console) and `cardholders` (a badge). Two candidates were rejected:
 *
 *   - Auto-detect from the address. One human can legitimately hold an account in BOTH
 *     tiers — the security guard who badges in and also runs the console is the obvious
 *     case — so a guess would silently sign them into the wrong privilege domain. It
 *     would also split every failed attempt across two rate-limit buckets, and leak
 *     which tier an address belongs to.
 *   - Try one, fall back to the other. Same leak, same double-counting, plus a confusing
 *     error when both fail.
 *
 * So the choice is explicit. It is two entries and not three: a visitor and a staff
 * cardholder are the same collection, so a visitor never has to know they are a
 * "visitor" in order to sign in.
 *
 * # Deep link and memory
 *
 * `?as=badge` / `?as=operator` selects a tier directly, which is what invite emails link
 * to. Otherwise the last choice is remembered per browser: whichever tier a device is
 * used for, it is almost always used for that one again, so a lobby tablet and an
 * operator's laptop each land on the right form without a click.
 */
const route = useRoute()
const router = useRouter()
const branding = useBrandingStore()

type Tier = 'operator' | 'badge'

/** Where the remembered choice lives. Namespaced so it cannot collide with a token. */
const TIER_KEY = 'stone-access:login-tier'

function isTier(v: unknown): v is Tier {
  return v === 'operator' || v === 'badge'
}

/**
 * Precedence: the URL, then the remembered choice, then operator. Operator is the
 * default for a first visit because a fresh install is set up by an operator before
 * anyone holds a badge.
 */
function initialTier(): Tier {
  const fromUrl = route.query.as
  if (isTier(fromUrl)) return fromUrl
  try {
    const remembered = localStorage.getItem(TIER_KEY)
    if (isTier(remembered)) return remembered
  } catch {
    // Private browsing or a storage-blocked context; the default is fine.
  }
  return 'operator'
}

const tier = ref<Tier>(initialTier())

// Remember the choice, and keep the URL honest so a reload or a shared link lands in the
// same place. `replace` so the selector does not fill up the back button.
watch(tier, (next) => {
  try {
    localStorage.setItem(TIER_KEY, next)
  } catch {
    // Not being able to remember is not worth surfacing.
  }
  if (route.query.as !== next) {
    router.replace({ query: { ...route.query, as: next } })
  }
})

// A link may change `?as=` without remounting this component.
watch(
  () => route.query.as,
  (v) => {
    if (isTier(v) && v !== tier.value) tier.value = v
  },
)

onMounted(() => {
  if (route.query.as !== tier.value) {
    router.replace({ query: { ...route.query, as: tier.value } })
  }
})

const subtitle = computed(() =>
  tier.value === 'badge' ? 'Sign in to view your badge' : 'Sign in to the control plane',
)
</script>

<template>
  <div class="min-h-screen flex items-center justify-center p-4 bg-base-200">
    <div class="w-full max-w-sm">
      <div class="flex flex-col items-center gap-3 mb-6">
        <BrandLogo :size="56" />
        <h1 class="text-lg font-semibold">{{ branding.appName }}</h1>
        <p class="text-sm text-base-content/60 text-center">{{ subtitle }}</p>
      </div>

      <div class="card bg-base-100 shadow-sm">
        <div class="card-body gap-4">
          <!-- The tier selector. role=tablist so it reads as a choice between two
               destinations rather than two unrelated buttons. -->
          <div role="tablist" class="tabs tabs-boxed">
            <button
              role="tab"
              type="button"
              class="tab flex-1"
              :class="{ 'tab-active': tier === 'badge' }"
              :aria-selected="tier === 'badge'"
              @click="tier = 'badge'"
            >
              My badge
            </button>
            <button
              role="tab"
              type="button"
              class="tab flex-1"
              :class="{ 'tab-active': tier === 'operator' }"
              :aria-selected="tier === 'operator'"
              @click="tier = 'operator'"
            >
              Operator
            </button>
          </div>

          <!-- Keyed so switching tiers discards the other form's state: a half-typed
               password must not survive into the other tier's field. -->
          <BadgeSignIn v-if="tier === 'badge'" key="badge" />
          <OperatorSignIn v-else key="operator" />
        </div>
      </div>

      <p class="text-xs text-base-content/50 text-center mt-6">
        <template v-if="tier === 'badge'">
          Managing this system? Choose <span class="font-medium">Operator</span> above.
        </template>
        <template v-else>
          Here to see your own badge? Choose <span class="font-medium">My badge</span> above.
        </template>
      </p>
    </div>
  </div>
</template>

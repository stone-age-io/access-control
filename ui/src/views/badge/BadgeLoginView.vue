<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useBadgeAuthStore } from '@/stores/badgeAuth'
import { useBrandingStore } from '@/stores/branding'
import BrandLogo from '@/components/common/BrandLogo.vue'

/**
 * Badge sign-in for cardholders and visitors — NOT operators (they use /login).
 *
 * Two steps, because that is how OTP works: enter an email, receive a code, enter
 * the code. The email address is typed here rather than carried in the invite link
 * on purpose — a link with an address in its query string leaks that address into
 * browser history, referrer headers, and any proxy log between here and the server.
 *
 * Note there is no "no badge found for that address" message. PocketBase does not
 * distinguish a known from an unknown email at the request-OTP step, and neither
 * should this page: doing so would turn it into a way to test which addresses hold
 * badges to a building.
 */
const router = useRouter()
const badgeAuth = useBadgeAuthStore()
const branding = useBrandingStore()

type Step = 'email' | 'code'
const step = ref<Step>('email')

const email = ref('')
const code = ref('')
const otpId = ref('')
const busy = ref(false)
const error = ref('')
const providers = ref<{ name: string; displayName: string }[]>([])

onMounted(async () => {
  providers.value = await badgeAuth.listOAuth2Providers()
})

async function requestCode() {
  error.value = ''
  const address = email.value.trim()
  if (!address) {
    error.value = 'Enter the email address your badge was issued to.'
    return
  }
  busy.value = true
  try {
    otpId.value = await badgeAuth.requestOtp(address)
    step.value = 'code'
  } catch (err: any) {
    // Deliberately generic: a specific error here would reveal whether the address
    // has a badge. A rate-limit rejection is the one case worth naming, since the
    // person can act on it.
    error.value =
      err?.status === 429
        ? 'Too many requests. Wait a minute and try again.'
        : 'Could not send a code. Check the address and try again.'
  } finally {
    busy.value = false
  }
}

async function verify() {
  error.value = ''
  if (!code.value.trim()) {
    error.value = 'Enter the code from your email.'
    return
  }
  busy.value = true
  try {
    await badgeAuth.verifyOtp(otpId.value, code.value.trim())
    router.push('/badge')
  } catch {
    error.value = 'That code is not valid or has expired. Request a new one.'
  } finally {
    busy.value = false
  }
}

function startOver() {
  step.value = 'email'
  code.value = ''
  otpId.value = ''
  error.value = ''
}

async function signInWith(provider: string) {
  error.value = ''
  busy.value = true
  try {
    await badgeAuth.loginWithOAuth2(provider)
    router.push('/badge')
  } catch {
    error.value = 'Sign-in was cancelled or failed.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center p-4 bg-base-200">
    <div class="w-full max-w-sm">
      <div class="flex flex-col items-center gap-3 mb-6">
        <BrandLogo :size="56" />
        <h1 class="text-lg font-semibold">{{ branding.appName }}</h1>
        <p class="text-sm text-base-content/60 text-center">Sign in to view your badge</p>
      </div>

      <div class="card bg-base-100 shadow-sm">
        <div class="card-body gap-4">
          <div v-if="error" class="alert alert-error py-2 text-sm">{{ error }}</div>

          <!-- Step 1: address -->
          <form v-if="step === 'email'" class="space-y-4" @submit.prevent="requestCode">
            <label class="form-control">
              <span class="label-text mb-1">Email address</span>
              <input
                v-model="email"
                type="email"
                autocomplete="email"
                inputmode="email"
                placeholder="you@example.com"
                class="input input-bordered"
                :disabled="busy"
              />
            </label>
            <button type="submit" class="btn btn-primary w-full" :disabled="busy">
              <span v-if="busy" class="loading loading-spinner"></span>
              <span v-else>Email me a sign-in code</span>
            </button>
          </form>

          <!-- Step 2: code -->
          <form v-else class="space-y-4" @submit.prevent="verify">
            <p class="text-sm text-base-content/70">
              We sent a code to <span class="font-medium">{{ email }}</span>. It expires shortly.
            </p>
            <label class="form-control">
              <span class="label-text mb-1">Sign-in code</span>
              <input
                v-model="code"
                type="text"
                inputmode="numeric"
                autocomplete="one-time-code"
                placeholder="12345678"
                class="input input-bordered font-mono tracking-widest text-center"
                :disabled="busy"
              />
            </label>
            <button type="submit" class="btn btn-primary w-full" :disabled="busy">
              <span v-if="busy" class="loading loading-spinner"></span>
              <span v-else>Sign in</span>
            </button>
            <button type="button" class="btn btn-ghost btn-sm w-full" :disabled="busy" @click="startOver">
              Use a different address
            </button>
          </form>

          <!-- OAuth2, when the install has providers configured -->
          <template v-if="providers.length && step === 'email'">
            <div class="divider text-xs">or</div>
            <button
              v-for="p in providers"
              :key="p.name"
              type="button"
              class="btn btn-outline w-full"
              :disabled="busy"
              @click="signInWith(p.name)"
            >
              Continue with {{ p.displayName }}
            </button>
          </template>
        </div>
      </div>

      <p class="text-xs text-base-content/50 text-center mt-6">
        Staff managing this system sign in at
        <router-link to="/login" class="link">the operator console</router-link>.
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useBadgeAuthStore } from '@/stores/badgeAuth'
import { useBrandingStore } from '@/stores/branding'
import BrandLogo from '@/components/common/BrandLogo.vue'

/**
 * Badge sign-in for cardholders and visitors — NOT operators (they use /login).
 *
 * Three methods, all enabled on the collection: password, emailed one-time code, and
 * OAuth2 where the install has providers. Password leads because a staff holder signs
 * in repeatedly for years and it is the only method that works with no SMTP; the code
 * path is one click away and is what a visitor uses.
 *
 * The address is typed here rather than carried in the invite link on purpose — a link
 * with an address in its query string leaks that address into browser history, referrer
 * headers, and any proxy log between here and the server.
 *
 * Note there is no "no badge found for that address" message anywhere on this page.
 * PocketBase does not distinguish a known from an unknown email at the request-OTP or
 * request-reset step, and neither should this page: doing so would turn it into a way
 * to test which addresses hold badges to a building. The same reasoning is why a failed
 * password sign-in says "email or password" and never which was wrong.
 */
const router = useRouter()
const badgeAuth = useBadgeAuthStore()
const branding = useBrandingStore()

/** 'password' and 'email' are entry points; 'code' and 'sent' are their second steps. */
type Step = 'password' | 'email' | 'code' | 'sent'
const step = ref<Step>('password')

const email = ref('')
const password = ref('')
const code = ref('')
const otpId = ref('')
const busy = ref(false)
const error = ref('')
const notice = ref('')
const providers = ref<{ name: string; displayName: string }[]>([])

onMounted(async () => {
  providers.value = await badgeAuth.listOAuth2Providers()
})

function addressOrFail(): string | null {
  const address = email.value.trim()
  if (!address) {
    error.value = 'Enter the email address your badge was issued to.'
    return null
  }
  return address
}

async function signInWithPassword() {
  error.value = ''
  notice.value = ''
  const address = addressOrFail()
  if (!address) return
  if (!password.value) {
    error.value = 'Enter your password, or use a sign-in code instead.'
    return
  }
  busy.value = true
  try {
    await badgeAuth.loginWithPassword(address, password.value)
    router.push('/badge')
  } catch (err: any) {
    // Deliberately does not say which of the two was wrong, and does not distinguish
    // "no such badge" from "wrong password".
    error.value =
      err?.status === 429
        ? 'Too many attempts. Wait a minute and try again.'
        : 'That email or password is not correct.'
  } finally {
    busy.value = false
  }
}

async function requestCode() {
  error.value = ''
  notice.value = ''
  const address = addressOrFail()
  if (!address) return
  busy.value = true
  try {
    otpId.value = await badgeAuth.requestOtp(address)
    step.value = 'code'
  } catch (err: any) {
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

async function forgotPassword() {
  error.value = ''
  notice.value = ''
  const address = addressOrFail()
  if (!address) return
  busy.value = true
  try {
    await badgeAuth.requestPasswordReset(address)
  } catch (err: any) {
    if (err?.status === 429) {
      error.value = 'Too many requests. Wait a minute and try again.'
      busy.value = false
      return
    }
    // Any other failure is swallowed: reporting it would reveal whether the address
    // has a badge. The confirmation below is deliberately unconditional.
  }
  step.value = 'sent'
  busy.value = false
}

function useCode() {
  error.value = ''
  notice.value = ''
  password.value = ''
  step.value = 'email'
}

function usePasswordInstead() {
  error.value = ''
  notice.value = ''
  code.value = ''
  otpId.value = ''
  step.value = 'password'
}

function startOver() {
  code.value = ''
  otpId.value = ''
  error.value = ''
  notice.value = ''
  step.value = 'email'
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
          <div v-if="notice" class="alert alert-info py-2 text-sm">{{ notice }}</div>

          <!-- Password sign-in (default) -->
          <form v-if="step === 'password'" class="space-y-4" @submit.prevent="signInWithPassword">
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
            <label class="form-control">
              <span class="label-text mb-1">Password</span>
              <input
                v-model="password"
                type="password"
                autocomplete="current-password"
                class="input input-bordered"
                :disabled="busy"
              />
            </label>
            <button type="submit" class="btn btn-primary w-full" :disabled="busy">
              <span v-if="busy" class="loading loading-spinner"></span>
              <span v-else>Sign in</span>
            </button>
            <div class="flex flex-col gap-1">
              <button type="button" class="btn btn-ghost btn-sm w-full" :disabled="busy" @click="useCode">
                Email me a sign-in code instead
              </button>
              <button type="button" class="btn btn-ghost btn-xs w-full" :disabled="busy" @click="forgotPassword">
                Forgot your password?
              </button>
            </div>
          </form>

          <!-- OTP step 1: address -->
          <form v-else-if="step === 'email'" class="space-y-4" @submit.prevent="requestCode">
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
            <button type="button" class="btn btn-ghost btn-sm w-full" :disabled="busy" @click="usePasswordInstead">
              Use a password instead
            </button>
          </form>

          <!-- OTP step 2: code -->
          <form v-else-if="step === 'code'" class="space-y-4" @submit.prevent="verify">
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

          <!-- Password reset requested. Unconditional: saying whether the address has a
               badge would make this an enumeration oracle. -->
          <div v-else class="space-y-4">
            <p class="text-sm text-base-content/70">
              If a badge exists for <span class="font-medium">{{ email }}</span>, a link to set a new
              password is on its way. Check your inbox.
            </p>
            <button type="button" class="btn btn-primary w-full" :disabled="busy" @click="usePasswordInstead">
              Back to sign in
            </button>
          </div>

          <!-- OAuth2, when the install has providers configured -->
          <template v-if="providers.length && (step === 'password' || step === 'email')">
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

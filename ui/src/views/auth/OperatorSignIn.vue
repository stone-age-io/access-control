<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

/**
 * Operator sign-in: email + password against the `users` collection.
 *
 * One method on purpose. This is the tier that edits the policy graph, so it has no
 * emailed-code path — a mailbox compromise should not hand over the control plane, and
 * unlike a visitor an operator is a known, provisioned account that can be given a
 * password at setup.
 */
const router = useRouter()
const authStore = useAuthStore()

const email = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')

async function signIn() {
  error.value = ''
  busy.value = true
  try {
    await authStore.login(email.value.trim(), password.value)
    router.push('/')
  } catch (err: any) {
    // Deliberately does not say which of the two was wrong.
    error.value =
      err?.status === 429
        ? 'Too many attempts. Wait a minute and try again.'
        : 'That email or password is not correct.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="space-y-4" @submit.prevent="signIn">
    <div v-if="error" class="alert alert-error py-2 text-sm">{{ error }}</div>

    <label class="form-control">
      <span class="label-text mb-1">Email</span>
      <input
        v-model="email"
        type="email"
        autocomplete="email"
        inputmode="email"
        placeholder="you@example.com"
        class="input input-bordered"
        :disabled="busy"
        required
      />
    </label>

    <label class="form-control">
      <span class="label-text mb-1">Password</span>
      <input
        v-model="password"
        type="password"
        autocomplete="current-password"
        placeholder="••••••••"
        class="input input-bordered"
        :disabled="busy"
        required
      />
    </label>

    <button type="submit" class="btn btn-primary w-full" :disabled="busy">
      <span v-if="busy" class="loading loading-spinner"></span>
      <span v-else>Sign in</span>
    </button>

    <p class="text-xs text-base-content/50 text-center">
      Superusers use the PocketBase dashboard (<span class="font-mono">/_/</span>).
    </p>
  </form>
</template>

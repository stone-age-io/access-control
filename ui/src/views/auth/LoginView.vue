<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import BrandLogo from '@/components/common/BrandLogo.vue'

const router = useRouter()
const authStore = useAuthStore()
const toast = useToast()

const email = ref('')
const password = ref('')
const loading = ref(false)

async function handleLogin() {
  loading.value = true
  try {
    await authStore.login(email.value, password.value)
    router.push('/')
  } catch (err: any) {
    toast.error(err?.message || 'Login failed')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-base-200 p-4">
    <div class="card w-full max-w-sm bg-base-100 shadow-xl border border-base-300">
      <div class="card-body">
        <div class="flex flex-col items-center mb-6">
          <div class="text-primary mb-2"><BrandLogo :size="48" /></div>
          <h2 class="text-2xl font-bold tracking-tight">Stone Access</h2>
          <p class="text-sm opacity-60">Sign in to the control plane</p>
        </div>

        <form @submit.prevent="handleLogin" class="space-y-4">
          <div class="form-control">
            <label class="label"><span class="label-text">Email</span></label>
            <input v-model="email" type="email" placeholder="admin@example.com" class="input input-bordered" required />
          </div>

          <div class="form-control">
            <label class="label"><span class="label-text">Password</span></label>
            <input v-model="password" type="password" placeholder="••••••••" class="input input-bordered" required />
          </div>

          <div class="form-control mt-6">
            <button type="submit" class="btn btn-primary w-full" :disabled="loading">
              <span v-if="loading" class="loading loading-spinner"></span>
              <span v-else>Sign In</span>
            </button>
          </div>
        </form>

        <p class="text-center text-xs opacity-50 mt-4">
          Authenticates as a PocketBase superuser.
        </p>
      </div>
    </div>
  </div>
</template>

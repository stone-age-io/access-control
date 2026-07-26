<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '@/stores/auth'
import { usePortalCommands } from '@/composables/usePortalCommands'
import { parsePortalQr } from '@/utils/portalQr'
import type { Portal } from '@/types/pocketbase'

/**
 * Scan a door placard, unlock that door. For an operator holding `command` — a guard
 * letting someone in, or a tech on a call.
 *
 * This is an OPERATOR OVERRIDE, not a credential presentation: it issues the same
 * cmd.grant the portal detail page's Grant button issues, attributed to the signed-in
 * operator. It is unrelated to badge remote unlock, which is authorized by the
 * holder's own credential via policy.Decide.
 *
 * The camera is a convenience for picking the door, nothing more — so when
 * BarcodeDetector is unavailable (notably Safari/iOS) the page falls back to a
 * searchable list instead of pretending to be broken. Zero new dependencies: adding a
 * wasm decoder to ship a nicer fallback is not worth the bundle weight for a
 * convenience feature.
 */
const toast = useToast()
const auth = useAuthStore()
const { grant, commanding } = usePortalCommands()

const canCommand = computed(() => auth.can('command'))

const portals = ref<Portal[]>([])
const loading = ref(true)
const search = ref('')
const scanning = ref(false)
const scanSupported = ref(false)
const lastScanned = ref('')

const video = ref<HTMLVideoElement | null>(null)
let stream: MediaStream | null = null
let detector: any = null
let rafId = 0

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  if (!q) return portals.value
  return portals.value.filter(
    (p) => p.code.toLowerCase().includes(q) || (p.name || '').toLowerCase().includes(q),
  )
})

async function load() {
  loading.value = true
  try {
    portals.value = await pb.collection('portals').getFullList<Portal>({ sort: 'code' })
  } catch (err: any) {
    toast.error(err?.message || 'Failed to load portals')
  } finally {
    loading.value = false
  }
}

async function startScan() {
  const BD = (window as any).BarcodeDetector
  if (!BD) {
    toast.error('This browser cannot scan. Pick the door from the list instead.')
    return
  }
  try {
    detector = new BD({ formats: ['qr_code'] })
    // `environment` = rear camera on a phone, which is what a guard will use.
    stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } })
    scanning.value = true
    // Wait a tick so the <video> exists before attaching the stream.
    requestAnimationFrame(() => {
      if (video.value && stream) {
        video.value.srcObject = stream
        video.value.play().catch(() => {})
        tick()
      }
    })
  } catch {
    toast.error('Could not open the camera. Check permissions, or pick from the list.')
    stopScan()
  }
}

function stopScan() {
  scanning.value = false
  if (rafId) cancelAnimationFrame(rafId)
  rafId = 0
  stream?.getTracks().forEach((t) => t.stop())
  stream = null
  detector = null
}

async function tick() {
  if (!scanning.value || !video.value || !detector) return
  try {
    const codes = await detector.detect(video.value)
    for (const c of codes) {
      const code = parsePortalQr(c.rawValue || '')
      if (code) {
        await onScanned(code)
        return // stop after a hit; onScanned closes the camera
      }
    }
  } catch {
    // A transient decode failure is normal between frames — keep going.
  }
  rafId = requestAnimationFrame(() => void tick())
}

async function onScanned(code: string) {
  stopScan()
  lastScanned.value = code
  const portal = portals.value.find((p) => p.code === code)
  if (!portal) {
    toast.error(`No portal named "${code}" — is this placard from another system?`)
    return
  }
  await grant(portal.id)
}

onMounted(() => {
  scanSupported.value = !!(window as any).BarcodeDetector && !!navigator.mediaDevices
  load()
})
onBeforeUnmount(stopScan)
</script>

<template>
  <div class="max-w-md mx-auto space-y-4">
    <div>
      <h1 class="text-xl font-bold">Scan to Unlock</h1>
      <p class="text-sm text-base-content/60">
        Scan a door placard to send a momentary unlock. Recorded against your operator account.
      </p>
    </div>

    <div v-if="!canCommand" class="alert alert-warning text-sm">
      <span>You do not hold the <strong>command</strong> capability, so you cannot unlock doors.</span>
    </div>

    <template v-else>
      <!-- Camera -->
      <div class="card bg-base-100 shadow-sm">
        <div class="card-body gap-3">
          <template v-if="scanning">
            <video ref="video" class="w-full rounded-lg bg-black aspect-square object-cover" muted playsinline />
            <button class="btn btn-ghost btn-sm" @click="stopScan">Cancel</button>
          </template>
          <template v-else>
            <button class="btn btn-primary" :disabled="!scanSupported || commanding" @click="startScan">
              {{ scanSupported ? 'Scan a placard' : 'Scanning not supported here' }}
            </button>
            <p v-if="!scanSupported" class="text-xs text-base-content/60">
              This browser has no built-in QR scanner (Safari and iOS are the usual cases). Pick the door from the
              list below — it sends exactly the same unlock.
            </p>
          </template>
          <p v-if="lastScanned" class="text-xs text-base-content/50">Last scanned: {{ lastScanned }}</p>
        </div>
      </div>

      <!-- Manual pick: the fallback, and often just faster -->
      <div class="card bg-base-100 shadow-sm">
        <div class="card-body gap-3">
          <input v-model="search" type="search" placeholder="Search doors…" class="input input-bordered input-sm" />
          <div v-if="loading" class="flex justify-center p-4">
            <span class="loading loading-spinner"></span>
          </div>
          <ul v-else class="divide-y divide-base-200 -mx-2">
            <li v-for="p in filtered" :key="p.id" class="flex items-center justify-between gap-2 px-2 py-2">
              <div class="min-w-0">
                <div class="text-sm font-medium truncate">{{ p.name || p.code }}</div>
                <div class="text-xs font-mono text-base-content/50">{{ p.code }}</div>
              </div>
              <button class="btn btn-xs btn-primary" :disabled="commanding" @click="grant(p.id)">Unlock</button>
            </li>
          </ul>
          <p v-if="!loading && !filtered.length" class="text-sm text-base-content/50 text-center py-2">
            No doors match.
          </p>
        </div>
      </div>

      <p class="text-xs text-base-content/50">
        Need placards? Print them from
        <router-link to="/portals/placards" class="link">Door Placards</router-link>.
      </p>
    </template>
  </div>
</template>

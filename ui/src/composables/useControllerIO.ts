import { ref, watch, type Ref } from 'vue'
import { pb } from '@/utils/pb'
import { modelProfile, type ModelProfile } from '@/utils/models'
import { buildControllerIO, type ControllerIO } from '@/utils/io'
import type { Controller, Portal, AuxInput, AuxOutput } from '@/types/pocketbase'

/**
 * Reactive view of a controller's hardware capacity (from its model profile) and
 * its current relay/input occupancy (the portals + aux I/O bound to it). Re-loads
 * whenever `controllerId` changes, so the index pickers on the I/O forms track the
 * selected controller. Fail-safe: any load error leaves the profile null, and the
 * pickers fall back to free-text entry.
 */
export function useControllerIO(controllerId: Ref<string>) {
  const profile = ref<ModelProfile | null>(null)
  const io = ref<ControllerIO>({ relays: new Map(), inputs: new Map() })
  const loading = ref(false)

  async function refresh() {
    const cid = controllerId.value
    profile.value = null
    io.value = { relays: new Map(), inputs: new Map() }
    if (!cid) return
    loading.value = true
    try {
      const ctrl = await pb.collection('controllers').getOne<Controller>(cid)
      profile.value = await modelProfile(ctrl.model || '')
      const [portals, auxIn, auxOut] = await Promise.all([
        pb.collection('portals').getFullList<Portal>({ filter: `controller = "${cid}"` }),
        pb.collection('aux_input').getFullList<AuxInput>({ filter: `controller = "${cid}"` }),
        pb.collection('aux_output').getFullList<AuxOutput>({ filter: `controller = "${cid}"` }),
      ])
      io.value = buildControllerIO(portals, auxIn, auxOut)
    } catch {
      // Leave capacity unknown; the picker degrades to a plain number input.
    } finally {
      loading.value = false
    }
  }

  watch(controllerId, refresh, { immediate: true })
  return { profile, io, loading, refresh }
}

import { ref } from 'vue'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'

/**
 * Operator arm/disarm commands for an area, issued via the accessd bridge.
 *
 * Unlike posture (a RAM override published over NATS), arming writes a DURABLE
 * arm_override on the area record — so a reboot can't silently disarm. The route
 * does the record write; the result reconciles back through the per-controller
 * area arm shadows (point_status kind=area). Gated by the `command` capability.
 */
export function useAreaCommands() {
  const toast = useToast()
  const { confirm } = useConfirm()
  const commanding = ref(false)

  async function arm(areaId: string, code?: string) {
    const confirmed = await confirm({
      title: 'Arm area',
      message: `Arm "${code || 'this area'}"?`,
      details: 'Armed intrusion points will raise alarms until disarmed. This persists across controller reboots.',
      confirmText: 'Arm',
      variant: 'warning',
    })
    if (!confirmed) return
    await send(areaId, 'arm', 'Area armed')
  }

  async function disarm(areaId: string, code?: string) {
    const confirmed = await confirm({
      title: 'Disarm area',
      message: `Disarm "${code || 'this area'}"?`,
      details: 'Intrusion points will stop raising alarms.',
      confirmText: 'Disarm',
      variant: 'warning',
    })
    if (!confirmed) return
    await send(areaId, 'disarm', 'Area disarmed')
  }

  // armClear reverts to the effective (scheduled/standing) arm-state.
  async function armClear(areaId: string) {
    await send(areaId, 'arm-clear', 'Override cleared')
  }

  async function send(areaId: string, action: string, ok: string) {
    commanding.value = true
    try {
      await pb.send(`/api/areas/${areaId}/${action}`, { method: 'POST', body: {} })
      toast.success(ok)
    } catch (err: any) {
      toast.error(err?.message || 'Command failed')
    } finally {
      commanding.value = false
    }
  }

  return { commanding, arm, disarm, armClear }
}

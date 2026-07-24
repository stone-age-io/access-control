import { ref } from 'vue'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import type { PointStatus } from '@/types/pocketbase'
import type { SoftTone } from '@/utils/badges'

/**
 * Operator control of an aux output, issued via the accessd command bridge
 * (POST /api/aux-outputs/{id}/output). Mirrors the Aux Output detail view's
 * drive(); gated by the `command` capability. Aux *inputs* are observe-only, so
 * there is no command for them.
 */
export function useAuxCommands() {
  const toast = useToast()
  const commanding = ref(false)

  async function drive(outputId: string, action: 'on' | 'off' | 'pulse') {
    commanding.value = true
    try {
      await pb.send(`/api/aux-outputs/${outputId}/output`, { method: 'POST', body: { action } })
      toast.success(`Output: ${action}`)
    } catch (err: any) {
      toast.error(err?.message || 'Failed to drive output')
    } finally {
      commanding.value = false
    }
  }

  return { commanding, drive }
}

/** Live-state badge for an aux point, from its point_status shadow (or null). */
export function auxStateBadge(
  kind: 'aux_input' | 'aux_output',
  status: PointStatus | null | undefined,
): { tone: SoftTone; text: string } {
  const s = status?.state
  if (kind === 'aux_input') {
    if (s === 'active') return { tone: 'warning', text: 'Active' }
    return status ? { tone: 'neutral', text: 'Inactive' } : { tone: 'neutral', text: 'Unknown' }
  }
  if (s === 'energized') return { tone: 'success', text: 'Energized' }
  return status ? { tone: 'neutral', text: 'Off' } : { tone: 'neutral', text: 'Unknown' }
}

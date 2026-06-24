import { ref } from 'vue'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'

/**
 * Operator acknowledgement of an alarm/fire event, via the accessd bridge
 * (POST /api/events/{id}/ack). Sets acknowledged/ack_by/ack_at on the events row.
 * Gated by the `command` capability.
 */
export function useAlarmAck() {
  const toast = useToast()
  const acking = ref(false)

  async function ack(eventId: string): Promise<boolean> {
    acking.value = true
    try {
      await pb.send(`/api/events/${eventId}/ack`, { method: 'POST', body: {} })
      toast.success('Acknowledged')
      return true
    } catch (err: any) {
      toast.error(err?.message || 'Failed to acknowledge')
      return false
    } finally {
      acking.value = false
    }
  }

  return { acking, ack }
}

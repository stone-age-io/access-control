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

  /**
   * Acknowledge many events at once (the console's "Ack all on page"). Fires the
   * per-event bridge calls concurrently and emits a single summary toast rather
   * than one per row. Returns the number actually acknowledged.
   */
  async function ackMany(eventIds: string[]): Promise<number> {
    if (eventIds.length === 0) return 0
    acking.value = true
    try {
      const results = await Promise.allSettled(
        eventIds.map((id) => pb.send(`/api/events/${id}/ack`, { method: 'POST', body: {} })),
      )
      const ok = results.filter((r) => r.status === 'fulfilled').length
      const failed = eventIds.length - ok
      if (failed === 0) toast.success(`Acknowledged ${ok} alarm${ok === 1 ? '' : 's'}`)
      else if (ok === 0) toast.error('Failed to acknowledge')
      else toast.success(`Acknowledged ${ok} of ${eventIds.length}; ${failed} failed`)
      return ok
    } finally {
      acking.value = false
    }
  }

  return { acking, ack, ackMany }
}

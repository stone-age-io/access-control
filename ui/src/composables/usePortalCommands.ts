import { ref } from 'vue'
import { pb } from '@/utils/pb'
import { useToast } from '@/composables/useToast'
import { useConfirm } from '@/composables/useConfirm'

/**
 * Operator control commands for a portal, issued via the accessd bridge
 * (fire-and-forget; results reconcile back through the point_status shadow).
 * The single source for grant/posture so the portal detail view and the
 * operational map can't drift apart.
 */

export const POSTURES: { value: string; label: string; danger?: boolean }[] = [
  { value: 'secure', label: 'Secure' },
  { value: 'unlocked', label: 'Unlocked' },
  { value: 'free_access', label: 'Free access' },
  { value: 'lockdown', label: 'Lockdown', danger: true },
  { value: 'disabled', label: 'Disabled', danger: true },
]

export function usePortalCommands() {
  const toast = useToast()
  const { confirm } = useConfirm()
  const commanding = ref(false)

  async function grant(portalId: string) {
    commanding.value = true
    try {
      await pb.send(`/api/portals/${portalId}/grant`, { method: 'POST', body: {} })
      toast.success('Grant sent')
    } catch (err: any) {
      toast.error(err?.message || 'Failed to send grant')
    } finally {
      commanding.value = false
    }
  }

  async function setPosture(portalId: string, value: string, opts: { danger?: boolean; code?: string } = {}) {
    if (opts.danger) {
      const confirmed = await confirm({
        title: `Set posture: ${value}`,
        message: `Set "${opts.code || 'this portal'}" to ${value}?`,
        details:
          value === 'lockdown'
            ? 'Lockdown denies all access, beating any valid credential, until cleared.'
            : 'Disabled stops enforcement on this portal until cleared.',
        confirmText: 'Set posture',
        variant: 'warning',
      })
      if (!confirmed) return
    }
    commanding.value = true
    try {
      await pb.send(`/api/portals/${portalId}/posture`, { method: 'POST', body: { posture: value } })
      toast.success(value === 'clear' ? 'Override cleared' : `Posture set: ${value}`)
    } catch (err: any) {
      toast.error(err?.message || 'Failed to set posture')
    } finally {
      commanding.value = false
    }
  }

  /**
   * Set the same posture on many portals at once (the list-view command bar).
   * Commands are per-portal and fire-and-forget, so there is no atomic bulk —
   * we fan out independent requests and report a summary. The caller owns the
   * batch-level confirm (it knows the count); this only issues + toasts the
   * outcome. `value: "clear"` reverts each portal's override.
   */
  async function setPostureBulk(portalIds: string[], value: string): Promise<{ ok: number; failed: number }> {
    commanding.value = true
    try {
      const results = await Promise.allSettled(
        portalIds.map((id) =>
          pb.send(`/api/portals/${id}/posture`, { method: 'POST', body: { posture: value } }),
        ),
      )
      const failed = results.filter((r) => r.status === 'rejected').length
      const ok = results.length - failed
      const verb = value === 'clear' ? 'Cleared override on' : `Set ${value} on`
      if (failed === 0) toast.success(`${verb} ${ok} portal(s)`)
      else if (ok === 0) toast.error(`Failed on all ${failed} portal(s)`)
      else toast.warning(`${verb} ${ok} portal(s); ${failed} failed`)
      return { ok, failed }
    } finally {
      commanding.value = false
    }
  }

  return { commanding, grant, setPosture, setPostureBulk }
}

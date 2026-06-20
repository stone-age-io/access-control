import { pb } from '@/utils/pb'

/** One logical relay or input line on a controller model: its 1-based index and a
 *  physical description (e.g. "BCM 5" or "MCP 0x20 pin 8 (port B)"). */
export interface ModelLine {
  index: number
  label: string
}

/** A controller model's hardware capacity, as served by accessd's GET /api/models.
 *  Mirrors internal/modelsapi. */
export interface ModelProfile {
  model: string
  transport: string
  relays: ModelLine[]
  inputs: ModelLine[]
}

// The catalogue is static (compiled into accessd), so fetch it once per session and
// share the promise. On failure the cache is cleared so a later call can retry.
let cache: Promise<Record<string, ModelProfile>> | null = null

/** Fetch the hardware-model catalogue, keyed by model identifier. Cached. */
export function fetchModels(): Promise<Record<string, ModelProfile>> {
  if (!cache) {
    cache = pb
      .send('/api/models', { method: 'GET' })
      .then((res: { models?: ModelProfile[] }) => {
        const map: Record<string, ModelProfile> = {}
        for (const m of res.models || []) map[m.model] = m
        return map
      })
      .catch((err) => {
        cache = null
        throw err
      })
  }
  return cache
}

/** The profile for one model, or null if the model is empty/unknown (callers then
 *  fall back to free-text index entry). */
export async function modelProfile(model: string): Promise<ModelProfile | null> {
  if (!model) return null
  try {
    const map = await fetchModels()
    return map[model] || null
  } catch {
    return null
  }
}

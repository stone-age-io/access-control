import { ref, computed } from 'vue'
import { pb } from '@/utils/pb'
import type { BaseRecord } from '@/types/pocketbase'

interface QueryOptions {
  filter?: string
  sort?: string
  expand?: string
}

interface PaginationOptions {
  /**
   * Count the matching rows on load() only, not on every page turn.
   *
   * PocketBase's list API runs a SECOND query — a COUNT(*) over the same filter
   * — to fill totalItems/totalPages. On a small collection that's free; on a
   * large one it costs as much as the page query itself, so every click of
   * next/prev pays the price twice. With countOnce the total is taken once, by
   * the load() that opens a query, and carried across the page turns under it.
   *
   * The trade is that the total goes stale if rows arrive mid-paging — it can
   * only ever be an *undercount*, and the next load() (any filter change, or a
   * revisit) corrects it. Worth it only where the collection is big enough for
   * the count to hurt; leave it off elsewhere.
   */
  countOnce?: boolean
}

/**
 * Pagination over a PocketBase collection.
 *
 *   const { items, page, totalPages, loading, load, nextPage, prevPage } =
 *     usePagination<Location>('locations', 20)
 *   onMounted(() => load({ sort: 'code' }))
 */
export function usePagination<T extends BaseRecord>(
  collectionName: string,
  perPage = 20,
  { countOnce = false }: PaginationOptions = {},
) {
  const items = ref<T[]>([])
  const page = ref(1)
  const totalPages = ref(1)
  const totalItems = ref(0)
  const loading = ref(false)
  const error = ref<string | null>(null)

  const hasMore = computed(() => page.value < totalPages.value)
  const hasPrev = computed(() => page.value > 1)

  async function fetchPage(options?: QueryOptions, skipTotal = false) {
    loading.value = true
    error.value = null
    try {
      const queryOptions: Record<string, any> = {}
      if (options?.filter) queryOptions.filter = options.filter
      if (options?.sort) queryOptions.sort = options.sort
      if (options?.expand) queryOptions.expand = options.expand
      if (skipTotal) queryOptions.skipTotal = true

      const result = await pb.collection(collectionName).getList<T>(page.value, perPage, queryOptions)
      items.value = result.items
      // skipTotal makes PocketBase report -1 for both, which would empty the
      // pager and the count line. Keep the figures the opening load() took.
      if (!skipTotal) {
        totalPages.value = result.totalPages
        totalItems.value = result.totalItems
      }
    } catch (err: any) {
      error.value = err.message
      console.error('Pagination error:', err)
    } finally {
      loading.value = false
    }
  }

  /** Run a query from the current page, always taking a fresh total. */
  async function load(options?: QueryOptions) {
    await fetchPage(options)
  }

  async function nextPage(options?: QueryOptions) {
    if (hasMore.value) {
      page.value++
      await fetchPage(options, countOnce)
    }
  }

  async function prevPage(options?: QueryOptions) {
    if (hasPrev.value) {
      page.value--
      await fetchPage(options, countOnce)
    }
  }

  function reset() {
    page.value = 1
    items.value = []
    totalPages.value = 1
    totalItems.value = 0
    error.value = null
  }

  return {
    items, page, totalPages, totalItems, loading, error,
    hasMore, hasPrev, load, nextPage, prevPage, reset,
  }
}

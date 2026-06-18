import { ref, computed } from 'vue'
import { pb } from '@/utils/pb'
import type { BaseRecord } from '@/types/pocketbase'

interface QueryOptions {
  filter?: string
  sort?: string
  expand?: string
}

/**
 * Pagination over a PocketBase collection.
 *
 *   const { items, page, totalPages, loading, load, nextPage, prevPage } =
 *     usePagination<Location>('locations', 20)
 *   onMounted(() => load({ sort: 'code' }))
 */
export function usePagination<T extends BaseRecord>(collectionName: string, perPage = 20) {
  const items = ref<T[]>([])
  const page = ref(1)
  const totalPages = ref(1)
  const totalItems = ref(0)
  const loading = ref(false)
  const error = ref<string | null>(null)

  const hasMore = computed(() => page.value < totalPages.value)
  const hasPrev = computed(() => page.value > 1)

  async function load(options?: QueryOptions) {
    loading.value = true
    error.value = null
    try {
      const queryOptions: Record<string, any> = {}
      if (options?.filter) queryOptions.filter = options.filter
      if (options?.sort) queryOptions.sort = options.sort
      if (options?.expand) queryOptions.expand = options.expand

      const result = await pb.collection(collectionName).getList<T>(page.value, perPage, queryOptions)
      items.value = result.items
      totalPages.value = result.totalPages
      totalItems.value = result.totalItems
    } catch (err: any) {
      error.value = err.message
      console.error('Pagination error:', err)
    } finally {
      loading.value = false
    }
  }

  async function nextPage(options?: QueryOptions) {
    if (hasMore.value) {
      page.value++
      await load(options)
    }
  }

  async function prevPage(options?: QueryOptions) {
    if (hasPrev.value) {
      page.value--
      await load(options)
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

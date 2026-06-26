import { ref, computed, watch, type Ref, type ComputedRef } from 'vue'

/**
 * Client-side pagination over an already-loaded array.
 *
 * The report views fetch the whole filtered set (so their summary tallies and
 * coverage gaps stay accurate) and derive a list/groups from it — server paging
 * would break those whole-set aggregates. This paginates only the *display* of
 * that derived array.
 *
 *   const paged = useClientPagination(filtered, 50)
 *   // template: v-for="row in paged.pageItems" + <ListPagination :page totalPages>
 *
 * Resets to page 1 whenever the source changes (a new filter/search/pivot/sort or
 * fresh data), so the user never lands on an out-of-range page.
 */
export function useClientPagination<T>(source: Ref<T[]> | ComputedRef<T[]>, perPage = 25) {
  const page = ref(1)

  const total = computed(() => source.value.length)
  const totalPages = computed(() => Math.max(1, Math.ceil(total.value / perPage)))
  const pageItems = computed(() => {
    const start = (page.value - 1) * perPage
    return source.value.slice(start, start + perPage)
  })

  // The source is a computed that yields a fresh array only when a dep (filter,
  // search, pivot, data) actually changes — so this fires on real changes, not on
  // mere page navigation (paging touches `page`, not `source`).
  watch(source, () => {
    page.value = 1
  })

  function next() {
    if (page.value < totalPages.value) page.value++
  }
  function prev() {
    if (page.value > 1) page.value--
  }

  return { page, total, totalPages, pageItems, next, prev }
}

import { useCallback, useMemo } from 'react'
import type {
  ColumnFiltersState,
  OnChangeFn,
  PaginationState,
  SortingState,
} from '@tanstack/react-table'

type SearchRecord = Record<string, unknown>

export type NavigateFn = (opts: {
  search:
    | true
    | SearchRecord
    | ((prev: SearchRecord) => Partial<SearchRecord> | SearchRecord)
  replace?: boolean
}) => void

type UseTableUrlStateParams = {
  search: SearchRecord
  navigate: NavigateFn
  pagination?: {
    pageKey?: string
    pageSizeKey?: string
    defaultPage?: number
    defaultPageSize?: number
  }
  globalFilter?: {
    enabled?: boolean
    key?: string
    trim?: boolean
  }
  sorting?: {
    enabled?: boolean
    sortKey?: string
    orderKey?: string
    defaultSort?: string
    defaultOrder?: 'asc' | 'desc'
  }
  columnFilters?: Array<
    | {
        columnId: string
        searchKey: string
        type?: 'string'
        serialize?: (value: unknown) => unknown
        deserialize?: (value: unknown) => unknown
      }
    | {
        columnId: string
        searchKey: string
        type: 'array'
        serialize?: (value: unknown) => unknown
        deserialize?: (value: unknown) => unknown
      }
  >
}

type UseTableUrlStateReturn = {
  globalFilter?: string
  onGlobalFilterChange?: OnChangeFn<string>
  columnFilters: ColumnFiltersState
  onColumnFiltersChange: OnChangeFn<ColumnFiltersState>
  pagination: PaginationState
  onPaginationChange: OnChangeFn<PaginationState>
  sorting: SortingState
  onSortingChange?: OnChangeFn<SortingState>
  ensurePageInRange: (
    pageCount: number,
    opts?: { resetTo?: 'first' | 'last' }
  ) => void
}

export function useTableUrlState(
  params: UseTableUrlStateParams
): UseTableUrlStateReturn {
  const {
    search,
    navigate,
    pagination: paginationCfg,
    globalFilter: globalFilterCfg,
    sorting: sortingCfg,
    columnFilters: columnFiltersCfg = [],
  } = params

  const pageKey = paginationCfg?.pageKey ?? 'page'
  const pageSizeKey = paginationCfg?.pageSizeKey ?? 'pageSize'
  const defaultPage = paginationCfg?.defaultPage ?? 1
  const defaultPageSize = paginationCfg?.defaultPageSize ?? 10

  const globalFilterKey = globalFilterCfg?.key ?? 'filter'
  const globalFilterEnabled = globalFilterCfg?.enabled ?? true
  const trimGlobal = globalFilterCfg?.trim ?? true

  const sortingEnabled = sortingCfg?.enabled ?? false
  const sortKey = sortingCfg?.sortKey ?? 'sort'
  const orderKey = sortingCfg?.orderKey ?? 'order'
  const defaultSort = sortingCfg?.defaultSort
  const defaultOrder = sortingCfg?.defaultOrder ?? 'desc'

  // Derive columnFilters directly from URL search so browser back/forward and Reset stay in sync.
  const columnFilters: ColumnFiltersState = useMemo(() => {
    const collected: ColumnFiltersState = []
    for (const cfg of columnFiltersCfg) {
      const raw = search[cfg.searchKey]
      const deserialize = cfg.deserialize ?? ((v: unknown) => v)
      if (cfg.type === 'string') {
        const value = (deserialize(raw) as string) ?? ''
        if (typeof value === 'string' && value.trim() !== '') {
          collected.push({ id: cfg.columnId, value })
        }
      } else {
        const value = (deserialize(raw) as unknown[]) ?? []
        if (Array.isArray(value) && value.length > 0) {
          collected.push({ id: cfg.columnId, value })
        }
      }
    }
    return collected
  }, [columnFiltersCfg, search])

  const pagination: PaginationState = useMemo(() => {
    const rawPage = search[pageKey]
    const rawPageSize = search[pageSizeKey]
    const pageNum =
      typeof rawPage === 'number' && rawPage >= 1 ? rawPage : defaultPage
    const pageSizeNum =
      typeof rawPageSize === 'number' && rawPageSize >= 1
        ? rawPageSize
        : defaultPageSize
    return { pageIndex: Math.max(0, pageNum - 1), pageSize: pageSizeNum }
  }, [search, pageKey, pageSizeKey, defaultPage, defaultPageSize])

  const onPaginationChange: OnChangeFn<PaginationState> = useCallback(
    (updater) => {
      const next = typeof updater === 'function' ? updater(pagination) : updater
      const nextPage = next.pageIndex + 1
      const nextPageSize = next.pageSize
      navigate({
        search: (prev) => ({
          ...prev,
          [pageKey]: nextPage <= defaultPage ? undefined : nextPage,
          [pageSizeKey]:
            nextPageSize === defaultPageSize ? undefined : nextPageSize,
        }),
      })
    },
    [navigate, pagination, pageKey, pageSizeKey, defaultPage, defaultPageSize]
  )

  // Derive globalFilter from URL search so external navigation updates it immediately.
  const globalFilter: string | undefined = useMemo(() => {
    if (!globalFilterEnabled) return undefined
    const raw = search[globalFilterKey]
    return typeof raw === 'string' ? raw : ''
  }, [globalFilterEnabled, search, globalFilterKey])

  const onGlobalFilterChange: OnChangeFn<string> | undefined = useMemo(() => {
    if (!globalFilterEnabled) return undefined
    return (updater) => {
      const next =
        typeof updater === 'function' ? updater(globalFilter ?? '') : updater
      const value = trimGlobal ? next.trim() : next
      navigate({
        search: (prev) => ({
          ...prev,
          [pageKey]: undefined,
          [globalFilterKey]: value ? value : undefined,
        }),
      })
    }
  }, [
    globalFilterEnabled,
    globalFilter,
    trimGlobal,
    navigate,
    pageKey,
    globalFilterKey,
  ])

  const onColumnFiltersChange: OnChangeFn<ColumnFiltersState> = useCallback(
    (updater) => {
      const next =
        typeof updater === 'function' ? updater(columnFilters) : updater

      const patch: Record<string, unknown> = {}
      for (const cfg of columnFiltersCfg) {
        const found = next.find((f) => f.id === cfg.columnId)
        const serialize = cfg.serialize ?? ((v: unknown) => v)
        if (cfg.type === 'string') {
          const value =
            typeof found?.value === 'string' ? (found.value as string) : ''
          patch[cfg.searchKey] =
            value.trim() !== '' ? serialize(value) : undefined
        } else {
          const value = Array.isArray(found?.value)
            ? (found.value as unknown[])
            : []
          patch[cfg.searchKey] = value.length > 0 ? serialize(value) : undefined
        }
      }

      navigate({
        search: (prev) => ({
          ...prev,
          [pageKey]: undefined,
          ...patch,
        }),
      })
    },
    [columnFilters, columnFiltersCfg, navigate, pageKey]
  )

  const sorting: SortingState = useMemo(() => {
    if (!sortingEnabled) return []
    const rawSort =
      typeof search[sortKey] === 'string'
        ? (search[sortKey] as string)
        : defaultSort
    if (!rawSort) return []
    const rawOrder =
      search[orderKey] === 'asc' || search[orderKey] === 'desc'
        ? (search[orderKey] as 'asc' | 'desc')
        : defaultOrder
    return [{ id: rawSort, desc: rawOrder === 'desc' }]
  }, [sortingEnabled, search, sortKey, orderKey, defaultSort, defaultOrder])

  const onSortingChange: OnChangeFn<SortingState> | undefined = useMemo(() => {
    if (!sortingEnabled) return undefined
    return (updater) => {
      const next = typeof updater === 'function' ? updater(sorting) : updater
      const first = next[0]
      if (!first) {
        navigate({
          search: (prev) => ({
            ...prev,
            [pageKey]: undefined,
            [sortKey]: undefined,
            [orderKey]: undefined,
          }),
        })
        return
      }
      const nextOrder: 'asc' | 'desc' = first.desc ? 'desc' : 'asc'
      const isDefault = first.id === defaultSort && nextOrder === defaultOrder
      navigate({
        search: (prev) => ({
          ...prev,
          [pageKey]: undefined,
          [sortKey]: isDefault ? undefined : first.id,
          [orderKey]: isDefault ? undefined : nextOrder,
        }),
      })
    }
  }, [
    sortingEnabled,
    sorting,
    navigate,
    pageKey,
    sortKey,
    orderKey,
    defaultSort,
    defaultOrder,
  ])

  const ensurePageInRange = useCallback(
    (pageCount: number, opts: { resetTo?: 'first' | 'last' } = {}) => {
      const resetTo = opts.resetTo ?? 'first'
      const currentPage = search[pageKey]
      const pageNum =
        typeof currentPage === 'number' ? currentPage : defaultPage
      if (pageCount > 0 && pageNum > pageCount) {
        navigate({
          replace: true,
          search: (prev) => ({
            ...prev,
            [pageKey]: resetTo === 'last' ? pageCount : undefined,
          }),
        })
      }
    },
    [search, pageKey, defaultPage, navigate]
  )

  return {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    sorting,
    onSortingChange,
    ensurePageInRange,
  }
}

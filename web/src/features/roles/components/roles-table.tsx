import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  type VisibilityState,
} from '@tanstack/react-table'
import { DataTablePagination, DataTableToolbar } from '@/components/data-table'
import { AlertCircle, Loader2, RefreshCw } from 'lucide-react'
import { normalizeApiError } from '@/lib/api/error'
import { cn } from '@/lib/utils'
import { type NavigateFn, useTableUrlState } from '@/hooks/use-table-url-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { rolesQueryOptions } from '../queries'
import type { RolesSearch } from '../schema'
import type { RoleItem, RolesListParams } from '../types'
import { createRoleColumns } from './roles-columns'

type RolesTableProps = {
  search: RolesSearch
  navigate: NavigateFn
  canUpdate: boolean
  canDelete: boolean
  onViewRole: (role: RoleItem) => void
  onEditRole: (role: RoleItem) => void
  onDeleteRole: (role: RoleItem) => void
}

const COLUMN_FILTERS_CONFIG = [
  { columnId: 'type', searchKey: 'type', type: 'array' as const },
]

export function RolesTable({
  search,
  navigate,
  canUpdate,
  canDelete,
  onViewRole,
  onEditRole,
  onDeleteRole,
}: RolesTableProps) {
  const { t, i18n } = useTranslation('roles')
  const { t: tCommon } = useTranslation('common')
  // Keep hidden synthetic `type` filter column out of rendered table cells.
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({
    type: false,
  })

  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    sorting,
    onSortingChange,
    ensurePageInRange,
  } = useTableUrlState({
    search,
    navigate,
    pagination: { defaultPage: 1, defaultPageSize: 20 },
    globalFilter: { enabled: true, key: 'q' },
    sorting: {
      enabled: true,
    },
    columnFilters: COLUMN_FILTERS_CONFIG,
  })

  const isSystemFilter = useMemo(() => {
    const selected = search.type ?? []
    if (selected.length === 1) {
      return selected[0] === 'system'
    }
    return undefined
  }, [search.type])

  const queryParams: RolesListParams = useMemo(() => {
    const activeSort = sorting[0]
    return {
      page: pagination.pageIndex + 1,
      pageSize: pagination.pageSize,
      q: globalFilter || undefined,
      isSystem: isSystemFilter,
      sort: activeSort?.id as RolesListParams['sort'],
      order: activeSort ? (activeSort.desc ? 'desc' : 'asc') : undefined,
    }
  }, [
    pagination.pageIndex,
    pagination.pageSize,
    globalFilter,
    isSystemFilter,
    sorting,
  ])

  const { data, isLoading, isFetching, isError, error, refetch } = useQuery(
    rolesQueryOptions(queryParams, i18n.language)
  )

  const total = data?.total ?? 0
  const pageCount = data
    ? Math.max(1, Math.ceil(total / pagination.pageSize))
    : 0

  useEffect(() => {
    if (data && pageCount > 0) {
      ensurePageInRange(pageCount)
    }
  }, [data, pageCount, ensurePageInRange])

  const columns = useMemo(
    () =>
      createRoleColumns({
        t,
        locale: i18n.language,
        canUpdate,
        canDelete,
        onView: onViewRole,
        onEdit: onEditRole,
        onDelete: onDeleteRole,
      }),
    [
      t,
      i18n.language,
      canUpdate,
      canDelete,
      onViewRole,
      onEditRole,
      onDeleteRole,
    ]
  )

  const table = useReactTable({
    data: data?.items ?? [],
    columns,
    pageCount,
    state: {
      pagination,
      columnFilters,
      sorting,
      columnVisibility,
      globalFilter,
    },
    manualPagination: true,
    manualFiltering: true,
    manualSorting: true,
    onPaginationChange,
    onColumnFiltersChange,
    onSortingChange,
    onGlobalFilterChange,
    onColumnVisibilityChange: (updater) => {
      setColumnVisibility((prev) => {
        const next = typeof updater === 'function' ? updater(prev) : updater
        return { ...next, type: false }
      })
    },
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) => row.id,
  })

  const hasAnyFilter = Boolean(globalFilter) || columnFilters.length > 0

  const handleResetAll = () => {
    navigate({
      search: (prev) => ({
        ...prev,
        page: undefined,
        q: undefined,
        type: undefined,
        sort: undefined,
        order: undefined,
      }),
    })
  }

  const filterConfigs = useMemo(
    () => [
      {
        columnId: 'type',
        title: t('filters.type'),
        options: [
          { label: t('filters.system'), value: 'system' },
          { label: t('filters.custom'), value: 'custom' },
        ],
      },
    ],
    [t]
  )

  const apiError = isError ? normalizeApiError(error) : null

  return (
    <div className='flex flex-1 flex-col gap-4'>
      <DataTableToolbar
        table={table}
        searchPlaceholder={t('searchPlaceholder')}
        filters={filterConfigs}
        hasActiveFilters={hasAnyFilter}
        onReset={handleResetAll}
        actions={
          <div className='flex items-center gap-2'>
            {isFetching && !isLoading && (
              <Loader2
                className='size-4 animate-spin text-muted-foreground'
                aria-label={tCommon('loading')}
              />
            )}
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='h-8'
              onClick={() => void refetch()}
              aria-label={tCommon('retry')}
            >
              <RefreshCw className='size-3.5' />
            </Button>
          </div>
        }
      />

      {apiError && (
        <Alert variant='destructive'>
          <AlertCircle className='size-4' />
          <AlertTitle>{t('error.loadFailed')}</AlertTitle>
          <AlertDescription className='flex flex-wrap items-center justify-between gap-2'>
            <span>{apiError.message || tCommon('retry')}</span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void refetch()}
            >
              {tCommon('retry')}
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <div className='overflow-hidden rounded-md border'>
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id} className='hover:bg-transparent'>
                {headerGroup.headers.map((header) => {
                  const sorted = header.column.getIsSorted()
                  const ariaSort = header.column.getCanSort()
                    ? sorted === 'asc'
                      ? 'ascending'
                      : sorted === 'desc'
                        ? 'descending'
                        : 'none'
                    : undefined
                  return (
                    <TableHead
                      key={header.id}
                      colSpan={header.colSpan}
                      aria-sort={ariaSort}
                      className={cn(
                        header.column.columnDef.meta?.className,
                        header.column.columnDef.meta?.thClassName
                      )}
                    >
                      {header.isPlaceholder
                        ? null
                        : flexRender(
                            header.column.columnDef.header,
                            header.getContext()
                          )}
                    </TableHead>
                  )
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {isLoading ? (
              Array.from({ length: pagination.pageSize }).map((_, rowIdx) => (
                <TableRow
                  key={`skeleton-${rowIdx}`}
                  className='hover:bg-transparent'
                >
                  {table.getVisibleLeafColumns().map((col) => (
                    <TableCell
                      key={col.id}
                      className={cn(
                        col.columnDef.meta?.className,
                        col.columnDef.meta?.tdClassName
                      )}
                    >
                      <Skeleton className='h-4 w-full max-w-32' />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : table.getRowModel().rows.length > 0 ? (
              table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  tabIndex={0}
                  className='group/row cursor-pointer focus-visible:bg-muted/60 focus-visible:outline-hidden'
                  onClick={() => onViewRole(row.original)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      onViewRole(row.original)
                    }
                  }}
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      className={cn(
                        cell.column.columnDef.meta?.className,
                        cell.column.columnDef.meta?.tdClassName
                      )}
                    >
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext()
                      )}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : (
              <TableRow className='hover:bg-transparent'>
                <TableCell
                  colSpan={table.getVisibleLeafColumns().length}
                  className='h-36 text-center'
                >
                  <div className='flex flex-col items-center justify-center gap-1.5 py-6'>
                    <p className='text-sm font-medium'>
                      {hasAnyFilter
                        ? t('empty.noMatchTitle')
                        : t('empty.noDataTitle')}
                    </p>
                    <p className='text-xs text-muted-foreground'>
                      {hasAnyFilter
                        ? t('empty.noMatchDesc')
                        : t('empty.noDataDesc')}
                    </p>
                    {hasAnyFilter && (
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        className='mt-2'
                        onClick={handleResetAll}
                      >
                        {t('empty.clearFilters')}
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <DataTablePagination table={table} total={data?.total} />
    </div>
  )
}

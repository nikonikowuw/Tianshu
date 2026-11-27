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
import { filesQueryOptions } from '../queries'
import type { FilesSearch } from '../schema'
import type {
  FileCategory,
  FileItem,
  FilesListParams,
  StorageDriver,
} from '../types'
import { createFileColumns } from './files-columns'

type FilesTableProps = {
  search: FilesSearch
  navigate: NavigateFn
  canDelete: boolean
  onInspectFile: (file: FileItem) => void
  onDownloadFile: (file: FileItem) => void
  onDeleteFile: (file: FileItem) => void
}

const COLUMN_FILTERS_CONFIG = [
  { columnId: 'category', searchKey: 'category', type: 'array' as const },
  { columnId: 'driver', searchKey: 'driver', type: 'array' as const },
]

export function FilesTable({
  search,
  navigate,
  canDelete,
  onInspectFile,
  onDownloadFile,
  onDeleteFile,
}: FilesTableProps) {
  const { t, i18n } = useTranslation('files')
  const { t: tCommon } = useTranslation('common')
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})

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
    sorting: { enabled: true },
    columnFilters: COLUMN_FILTERS_CONFIG,
  })

  const queryParams: FilesListParams = useMemo(() => {
    const activeSort = sorting[0]
    return {
      page: pagination.pageIndex + 1,
      pageSize: pagination.pageSize,
      q: globalFilter || undefined,
      category:
        search.category && search.category.length > 0
          ? (search.category as FileCategory[])
          : undefined,
      driver:
        search.driver && search.driver.length > 0
          ? (search.driver as StorageDriver[])
          : undefined,
      sort: activeSort?.id as FilesListParams['sort'],
      order: activeSort ? (activeSort.desc ? 'desc' : 'asc') : undefined,
    }
  }, [
    pagination.pageIndex,
    pagination.pageSize,
    globalFilter,
    search.category,
    search.driver,
    sorting,
  ])

  const { data, isLoading, isFetching, isError, error, refetch } = useQuery(
    filesQueryOptions(queryParams)
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
      createFileColumns({
        t,
        locale: i18n.language,
        canDelete,
        onInspect: onInspectFile,
        onDownload: onDownloadFile,
        onDelete: onDeleteFile,
      }),
    [t, i18n.language, canDelete, onInspectFile, onDownloadFile, onDeleteFile]
  )

  const table = useReactTable({
    data: data?.items ?? [],
    columns,
    pageCount,
    state: {
      pagination,
      sorting,
      columnFilters,
      columnVisibility,
      globalFilter,
    },
    manualPagination: true,
    manualSorting: true,
    manualFiltering: true,
    onPaginationChange,
    onSortingChange,
    onColumnFiltersChange,
    onColumnVisibilityChange: setColumnVisibility,
    onGlobalFilterChange,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) => row.id,
  })

  const filters = useMemo(
    () => [
      {
        columnId: 'category',
        title: t('filters.category'),
        options: [
          { label: t('categories.image'), value: 'image' },
          { label: t('categories.document'), value: 'document' },
        ],
      },
      {
        columnId: 'driver',
        title: t('filters.driver'),
        options: [
          { label: t('drivers.local'), value: 'local' },
          { label: t('drivers.s3'), value: 's3' },
        ],
      },
    ],
    [t]
  )

  const hasActiveFilter =
    Boolean(globalFilter) ||
    (search.category?.length ?? 0) > 0 ||
    (search.driver?.length ?? 0) > 0

  const handleResetFilters = () => {
    navigate({
      replace: true,
      search: (prev) => ({
        ...prev,
        q: undefined,
        category: undefined,
        driver: undefined,
        sort: undefined,
        order: undefined,
        page: undefined,
      }),
    })
  }

  return (
    <div className='flex flex-1 flex-col gap-4'>
      <DataTableToolbar
        table={table}
        searchPlaceholder={t('searchPlaceholder')}
        filters={filters}
        onReset={handleResetFilters}
      />

      {isError && (
        <Alert variant='destructive'>
          <AlertCircle className='size-4' />
          <AlertTitle>{t('error.loadTitle')}</AlertTitle>
          <AlertDescription className='flex items-center justify-between gap-4'>
            <span>{normalizeApiError(error).message}</span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => void refetch()}
            >
              <RefreshCw className='size-3.5' />
              {t('error.retry')}
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <div className='relative overflow-hidden rounded-md border'>
        {isFetching && !isLoading && (
          <div className='pointer-events-none absolute end-3 top-2 z-20 flex items-center gap-1.5 rounded-full bg-background/85 px-2.5 py-0.5 text-xs text-muted-foreground shadow-xs ring-1 ring-border backdrop-blur-xs'>
            <Loader2 className='size-3 animate-spin' />
            <span>{tCommon('loading')}</span>
          </div>
        )}
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
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
                      aria-sort={ariaSort}
                      className={cn(header.column.columnDef.meta?.thClassName)}
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
              Array.from({ length: pagination.pageSize }).map((_, idx) => (
                <TableRow key={`skeleton-${idx}`}>
                  {table.getVisibleLeafColumns().map((col) => (
                    <TableCell
                      key={col.id}
                      className={cn(col.columnDef.meta?.tdClassName)}
                    >
                      <Skeleton className='h-4 w-full max-w-36' />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : table.getRowModel().rows.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={table.getVisibleLeafColumns().length}
                  className='h-36 text-center text-muted-foreground'
                >
                  <div className='flex flex-col items-center justify-center gap-2'>
                    <span>
                      {hasActiveFilter
                        ? t('empty.noResults')
                        : t('empty.noData')}
                    </span>
                    {hasActiveFilter && (
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={handleResetFilters}
                      >
                        {tCommon('table.clearFilters')}
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ) : (
              table.getRowModel().rows.map((row) => (
                <TableRow
                  key={row.id}
                  data-testid='file-row'
                  data-file-name={row.original.originalName}
                  tabIndex={0}
                  className='group/row cursor-pointer focus-visible:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none focus-visible:ring-inset'
                  onClick={() => onInspectFile(row.original)}
                  onKeyDown={(e) => {
                    if (e.target !== e.currentTarget) return
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      onInspectFile(row.original)
                    }
                  }}
                >
                  {row.getVisibleCells().map((cell) => (
                    <TableCell
                      key={cell.id}
                      className={cn(cell.column.columnDef.meta?.tdClassName)}
                    >
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext()
                      )}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      <DataTablePagination table={table} total={total} />
    </div>
  )
}

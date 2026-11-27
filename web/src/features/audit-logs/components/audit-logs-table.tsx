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
import { auditLogsQueryOptions } from '../queries'
import type { AuditLogsSearch } from '../schema'
import type { AuditLogItem, AuditLogsListParams } from '../types'
import { AuditDateRangeFilter } from './audit-date-range-filter'
import { createAuditLogColumns } from './audit-logs-columns'

type AuditLogsTableProps = {
  search: AuditLogsSearch
  navigate: NavigateFn
  onSelectEntry: (entry: AuditLogItem) => void
}

const COLUMN_FILTERS_CONFIG = [
  { columnId: 'status', searchKey: 'status', type: 'array' as const },
  { columnId: 'action', searchKey: 'action', type: 'array' as const },
  { columnId: 'resource', searchKey: 'resource', type: 'array' as const },
]

export function AuditLogsTable({
  search,
  navigate,
  onSelectEntry,
}: AuditLogsTableProps) {
  const { t, i18n } = useTranslation('audit')
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
    sorting: {
      enabled: true,
      defaultSort: 'created_at',
      defaultOrder: 'desc',
    },
    columnFilters: COLUMN_FILTERS_CONFIG,
  })

  const queryParams: AuditLogsListParams = useMemo(() => {
    const activeSort = sorting[0]
    return {
      page: pagination.pageIndex + 1,
      pageSize: pagination.pageSize,
      q: globalFilter || undefined,
      status:
        search.status && search.status.length > 0 ? search.status : undefined,
      action:
        search.action && search.action.length > 0 ? search.action : undefined,
      resource:
        search.resource && search.resource.length > 0
          ? search.resource
          : undefined,
      from: search.from,
      to: search.to,
      sort: activeSort?.id as AuditLogsListParams['sort'],
      order: activeSort ? (activeSort.desc ? 'desc' : 'asc') : undefined,
    }
  }, [
    pagination.pageIndex,
    pagination.pageSize,
    globalFilter,
    search.status,
    search.action,
    search.resource,
    search.from,
    search.to,
    sorting,
  ])

  const { data, isLoading, isFetching, isError, error, refetch } = useQuery(
    auditLogsQueryOptions(queryParams)
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
      createAuditLogColumns({
        t,
        locale: i18n.language,
        onViewDetails: onSelectEntry,
      }),
    [t, i18n.language, onSelectEntry]
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
    onColumnVisibilityChange: setColumnVisibility,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) => row.id,
  })

  const hasDateFilter = Boolean(search.from || search.to)
  const hasAnyFilter =
    Boolean(globalFilter) || columnFilters.length > 0 || hasDateFilter

  const handleResetAll = () => {
    navigate({
      search: (prev) => ({
        ...prev,
        page: undefined,
        q: undefined,
        status: undefined,
        action: undefined,
        resource: undefined,
        from: undefined,
        to: undefined,
        sort: undefined,
        order: undefined,
      }),
    })
  }

  const filterConfigs = useMemo(
    () => [
      {
        columnId: 'status',
        title: t('filters.status'),
        options: [
          { label: t('statuses.success'), value: 'success' },
          { label: t('statuses.failure'), value: 'failure' },
        ],
      },
      {
        columnId: 'action',
        title: t('filters.action'),
        options: [
          { label: t('actions.authLogin'), value: 'auth.login' },
          { label: t('actions.authLogout'), value: 'auth.logout' },
          { label: t('actions.authRefresh'), value: 'auth.refresh' },
          {
            label: t('actions.authRefreshReplay'),
            value: 'auth.refresh_replay',
          },
          { label: t('actions.setupInit'), value: 'setup.init' },
          { label: t('actions.roleCreate'), value: 'role.create' },
          { label: t('actions.roleUpdate'), value: 'role.update' },
          { label: t('actions.roleDelete'), value: 'role.delete' },
          { label: t('actions.fileUpload'), value: 'file.upload' },
          { label: t('actions.fileUploadAvatar'), value: 'file.upload_avatar' },
          { label: t('actions.fileDelete'), value: 'file.delete' },
          { label: t('actions.profileUpdate'), value: 'profile.update' },
          {
            label: t('actions.profileChangePassword'),
            value: 'profile.change_password',
          },
          {
            label: t('actions.profileSessionRevoke'),
            value: 'profile.session_revoke',
          },
          {
            label: t('actions.profileSessionsRevokeOthers'),
            value: 'profile.sessions_revoke_others',
          },
        ],
      },
      {
        columnId: 'resource',
        title: t('filters.resource'),
        options: [
          { label: t('resources.session'), value: 'session' },
          { label: t('resources.system'), value: 'system' },
          { label: t('resources.user'), value: 'user' },
          { label: t('resources.role'), value: 'role' },
          { label: t('resources.file'), value: 'file' },
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
        extraFilters={
          <AuditDateRangeFilter
            from={search.from}
            to={search.to}
            onChange={({ from, to }) => {
              navigate({
                search: (prev) => ({
                  ...prev,
                  page: undefined,
                  from,
                  to,
                }),
              })
            }}
          />
        }
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
                  onClick={() => onSelectEntry(row.original)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      onSelectEntry(row.original)
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

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
import { useCurrentUser, useIsSuperAdmin } from '@/lib/auth/use-permission'
import { formatServerErrorMessage } from '@/lib/handle-server-error'
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
import { roleOptionsQueryOptions, usersQueryOptions } from '../queries'
import type { UsersSearch } from '../schema'
import type { UserItem, UsersListParams, UserStatus } from '../types'
import { createUserColumns } from './users-columns'

type UsersTableProps = {
  search: UsersSearch
  navigate: NavigateFn
  canUpdate: boolean
  canDelete: boolean
  onEditUser: (user: UserItem) => void
  onResetPassword: (user: UserItem) => void
  onToggleStatus: (user: UserItem) => void
  onDeleteUser: (user: UserItem) => void
}

const COLUMN_FILTERS_CONFIG = [
  { columnId: 'status', searchKey: 'status', type: 'array' as const },
  { columnId: 'roleId', searchKey: 'roleId', type: 'array' as const },
]

export function UsersTable({
  search,
  navigate,
  canUpdate,
  canDelete,
  onEditUser,
  onResetPassword,
  onToggleStatus,
  onDeleteUser,
}: UsersTableProps) {
  const { t, i18n } = useTranslation('users')
  const { t: tCommon } = useTranslation('common')
  const currentUser = useCurrentUser()
  const actorIsSuperAdmin = useIsSuperAdmin()

  // Keep hidden synthetic `roleId` filter column out of rendered table cells.
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({
    roleId: false,
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
    sorting: { enabled: true },
    columnFilters: COLUMN_FILTERS_CONFIG,
  })

  const queryParams: UsersListParams = useMemo(() => {
    const activeSort = sorting[0]
    return {
      page: pagination.pageIndex + 1,
      pageSize: pagination.pageSize,
      q: globalFilter || undefined,
      status:
        search.status && search.status.length > 0
          ? (search.status as UserStatus[])
          : undefined,
      roleId:
        search.roleId && search.roleId.length > 0 ? search.roleId : undefined,
      sort: activeSort?.id as UsersListParams['sort'],
      order: activeSort ? (activeSort.desc ? 'desc' : 'asc') : undefined,
    }
  }, [
    pagination.pageIndex,
    pagination.pageSize,
    globalFilter,
    search.status,
    search.roleId,
    sorting,
  ])

  const { data, isLoading, isFetching, isError, error, refetch } = useQuery(
    usersQueryOptions(queryParams, i18n.language)
  )
  const { data: roleOptionsData } = useQuery(
    roleOptionsQueryOptions(i18n.language)
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
      createUserColumns({
        t,
        locale: i18n.language,
        currentUserId: currentUser?.id ?? null,
        actorIsSuperAdmin,
        canUpdate,
        canDelete,
        onEdit: onEditUser,
        onResetPassword,
        onToggleStatus,
        onDelete: onDeleteUser,
      }),
    [
      t,
      i18n.language,
      currentUser?.id,
      actorIsSuperAdmin,
      canUpdate,
      canDelete,
      onEditUser,
      onResetPassword,
      onToggleStatus,
      onDeleteUser,
    ]
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

  const filters = useMemo(() => {
    const statusFilter = {
      columnId: 'status',
      title: t('filters.status'),
      options: [
        { label: t('status.active'), value: 'active' },
        { label: t('status.disabled'), value: 'disabled' },
      ],
    }
    const roleOptions = (roleOptionsData?.items ?? []).map((r) => ({
      label: r.name,
      value: r.id,
    }))
    if (roleOptions.length === 0) {
      return [statusFilter]
    }
    return [
      statusFilter,
      {
        columnId: 'roleId',
        title: t('filters.role'),
        options: roleOptions,
      },
    ]
  }, [t, roleOptionsData])

  const hasActiveFilter =
    Boolean(globalFilter) ||
    (search.status?.length ?? 0) > 0 ||
    (search.roleId?.length ?? 0) > 0

  const handleResetFilters = () => {
    navigate({
      replace: true,
      search: (prev) => ({
        ...prev,
        q: undefined,
        status: undefined,
        roleId: undefined,
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
        searchMaxLength={100}
        filters={filters}
        hasActiveFilters={hasActiveFilter || sorting.length > 0}
        onReset={handleResetFilters}
      />

      {isError && (
        <Alert variant='destructive'>
          <AlertCircle className='size-4' />
          <AlertTitle>{t('error.loadTitle')}</AlertTitle>
          <AlertDescription className='flex items-center justify-between gap-4'>
            <span>{formatServerErrorMessage(error)}</span>
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
            ) : isError ? null : table.getRowModel().rows.length === 0 ? (
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
                <TableRow key={row.id} className='group/row'>
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

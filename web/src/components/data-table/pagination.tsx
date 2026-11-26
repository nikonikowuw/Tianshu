import { useTranslation } from 'react-i18next'
import { type Table } from '@tanstack/react-table'
import {
  ChevronLeft as ChevronLeftIcon,
  ChevronRight as ChevronRightIcon,
  ChevronsLeft as DoubleArrowLeftIcon,
  ChevronsRight as DoubleArrowRightIcon,
} from 'lucide-react'
import { cn, getPageNumbers } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

type DataTablePaginationProps<TData> = {
  table: Table<TData>
  total?: number
  className?: string
}

const PAGE_SIZE_OPTIONS = [10, 20, 50, 100]

export function DataTablePagination<TData>({
  table,
  total,
  className,
}: DataTablePaginationProps<TData>) {
  const { t } = useTranslation('common')
  const currentPage = table.getState().pagination.pageIndex + 1
  const totalPages = Math.max(1, table.getPageCount())
  const pageNumbers = getPageNumbers(currentPage, totalPages)

  return (
    <div
      data-slot='data-table-pagination'
      className={cn('@container/pagination', className)}
    >
      <div className='flex flex-col-reverse items-center justify-between gap-4 px-0.5 py-1 @2xl/pagination:flex-row @2xl/pagination:gap-6'>
        <div className='flex w-full items-center justify-between gap-2 @2xl/pagination:w-auto'>
          <div className='flex items-center gap-2'>
            <Select
              value={`${table.getState().pagination.pageSize}`}
              onValueChange={(value) => {
                table.setPageSize(Number(value))
              }}
            >
              <SelectTrigger
                size='sm'
                className='w-auto min-w-20 tabular-nums'
                aria-label={t('table.rowsPerPage')}
              >
                <SelectValue
                  placeholder={table.getState().pagination.pageSize}
                />
              </SelectTrigger>
              <SelectContent side='top'>
                {PAGE_SIZE_OPTIONS.map((pageSize) => (
                  <SelectItem key={pageSize} value={`${pageSize}`}>
                    {pageSize}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className='text-sm font-medium whitespace-nowrap'>
              {t('table.rowsPerPage')}
            </p>
            {typeof total === 'number' && (
              <span className='hidden text-sm whitespace-nowrap text-muted-foreground tabular-nums sm:inline'>
                · {t('table.totalItems', { total })}
              </span>
            )}
          </div>

          <span className='text-sm font-medium whitespace-nowrap text-muted-foreground tabular-nums @2xl/pagination:hidden'>
            {t('table.pageOf', { page: currentPage, total: totalPages })}
          </span>
        </div>

        <div className='flex max-w-full flex-wrap items-center justify-center gap-4 @2xl/pagination:shrink-0 @2xl/pagination:flex-nowrap @2xl/pagination:gap-5'>
          <span className='hidden text-sm font-medium whitespace-nowrap text-muted-foreground tabular-nums @2xl/pagination:inline'>
            {t('table.pageOf', { page: currentPage, total: totalPages })}
          </span>
          <div className='flex max-w-full flex-wrap items-center justify-center gap-1.5 @2xl/pagination:flex-nowrap'>
            <Button
              type='button'
              variant='outline'
              className='hidden size-8 p-0 @md/pagination:inline-flex'
              onClick={() => table.setPageIndex(0)}
              disabled={!table.getCanPreviousPage()}
            >
              <span className='sr-only'>{t('table.firstPage')}</span>
              <DoubleArrowLeftIcon className='size-4' />
            </Button>
            <Button
              type='button'
              variant='outline'
              className='size-8 p-0'
              onClick={() => table.previousPage()}
              disabled={!table.getCanPreviousPage()}
            >
              <span className='sr-only'>{t('table.prevPage')}</span>
              <ChevronLeftIcon className='size-4' />
            </Button>

            {pageNumbers.map((pageNumber, index) =>
              pageNumber === '...' ? (
                <span
                  key={`ellipsis-${index}`}
                  className='px-1 text-sm text-muted-foreground'
                >
                  ...
                </span>
              ) : (
                <Button
                  key={pageNumber}
                  type='button'
                  variant={currentPage === pageNumber ? 'default' : 'outline'}
                  className='h-8 min-w-8 px-2 tabular-nums'
                  onClick={() => table.setPageIndex((pageNumber as number) - 1)}
                >
                  <span className='sr-only'>
                    {t('table.pageOf', {
                      page: pageNumber,
                      total: totalPages,
                    })}
                  </span>
                  {pageNumber}
                </Button>
              )
            )}

            <Button
              type='button'
              variant='outline'
              className='size-8 p-0'
              onClick={() => table.nextPage()}
              disabled={!table.getCanNextPage()}
            >
              <span className='sr-only'>{t('table.nextPage')}</span>
              <ChevronRightIcon className='size-4' />
            </Button>
            <Button
              type='button'
              variant='outline'
              className='hidden size-8 p-0 @md/pagination:inline-flex'
              onClick={() => table.setPageIndex(totalPages - 1)}
              disabled={!table.getCanNextPage()}
            >
              <span className='sr-only'>{t('table.lastPage')}</span>
              <DoubleArrowRightIcon className='size-4' />
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

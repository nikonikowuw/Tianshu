import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { type Table } from '@tanstack/react-table'
import { X as Cross2Icon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { DataTableFacetedFilter } from './faceted-filter'
import { DataTableViewOptions } from './view-options'

type DataTableToolbarProps<TData> = {
  table: Table<TData>
  searchPlaceholder?: string
  searchKey?: string
  searchMaxLength?: number
  debounceMs?: number
  filters?: {
    columnId: string
    title: string
    options: {
      label: string
      value: string
      icon?: React.ComponentType<{ className?: string }>
    }[]
  }[]
  extraFilters?: React.ReactNode
  actions?: React.ReactNode
  onReset?: () => void
  hasActiveFilters?: boolean
}

export function DataTableToolbar<TData>({
  table,
  searchPlaceholder,
  searchKey,
  searchMaxLength,
  debounceMs = 300,
  filters = [],
  extraFilters,
  actions,
  onReset,
  hasActiveFilters = false,
}: DataTableToolbarProps<TData>) {
  const { t } = useTranslation('common')
  const columnSearchValue = searchKey
    ? ((table.getColumn(searchKey)?.getFilterValue() as string) ?? '')
    : undefined
  const globalFilterValue = (table.getState().globalFilter as string) ?? ''
  const externalValue = searchKey
    ? (columnSearchValue ?? '')
    : globalFilterValue

  const [draftValue, setDraftValue] = useState(externalValue)
  const [prevExternal, setPrevExternal] = useState(externalValue)
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Keep draft synchronized when URL/external filter changes (e.g., Back/Forward or Reset).
  if (externalValue !== prevExternal) {
    setPrevExternal(externalValue)
    setDraftValue(externalValue)
  }

  // 外部筛选值变化时取消未决防抖定时器，避免覆盖浏览器后退/前进恢复的 URL 状态
  useEffect(() => {
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current)
      debounceTimerRef.current = null
    }
  }, [externalValue])

  useEffect(() => {
    return () => {
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current)
      }
    }
  }, [])

  const applySearch = (val: string) => {
    const trimmed = val.trim()
    if (searchKey) {
      table.getColumn(searchKey)?.setFilterValue(trimmed ? val : undefined)
    } else {
      table.setGlobalFilter(trimmed ? val : '')
    }
  }

  const handleSearchChange = (next: string) => {
    setDraftValue(next)
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current)
      debounceTimerRef.current = null
    }
    if (next.trim() === '' || debounceMs <= 0) {
      applySearch(next.trim() === '' ? '' : next)
      return
    }
    debounceTimerRef.current = setTimeout(() => {
      debounceTimerRef.current = null
      applySearch(next)
    }, debounceMs)
  }

  const isFiltered =
    hasActiveFilters ||
    table.getState().columnFilters.length > 0 ||
    Boolean(table.getState().globalFilter)

  return (
    <div className='flex flex-wrap items-center justify-between gap-2'>
      <div className='flex flex-1 flex-wrap items-center gap-2'>
        <Input
          placeholder={searchPlaceholder ?? t('table.filterPlaceholder')}
          aria-label={searchPlaceholder ?? t('table.filterPlaceholder')}
          value={draftValue}
          maxLength={searchMaxLength}
          onChange={(event) => handleSearchChange(event.target.value)}
          className='h-8 w-44 sm:w-56 lg:w-72'
        />
        <div className='flex flex-wrap items-center gap-2'>
          {filters.map((filter) => {
            const column = table.getColumn(filter.columnId)
            if (!column) return null
            return (
              <DataTableFacetedFilter
                key={filter.columnId}
                column={column}
                title={filter.title}
                options={filter.options}
              />
            )
          })}
          {extraFilters}
        </div>
        {isFiltered && (
          <Button
            variant='ghost'
            onClick={() => {
              if (debounceTimerRef.current) {
                clearTimeout(debounceTimerRef.current)
              }
              setDraftValue('')
              if (onReset) {
                onReset()
              } else {
                table.resetColumnFilters()
                table.setGlobalFilter('')
              }
            }}
            className='h-8 px-2 lg:px-3'
          >
            {t('table.reset')}
            <Cross2Icon className='ms-2 size-4' />
          </Button>
        )}
      </div>
      <div className='flex items-center gap-2'>
        {actions}
        <DataTableViewOptions table={table} />
      </div>
    </div>
  )
}

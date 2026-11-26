import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { CalendarIcon } from 'lucide-react'
import { type DateRange } from 'react-day-picker'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Separator } from '@/components/ui/separator'

type AuditDateRangeFilterProps = {
  from?: string
  to?: string
  onChange: (next: { from?: string; to?: string }) => void
}

function parseDateOnly(value?: string): Date | undefined {
  if (!value) return undefined
  const [y, m, d] = value.split('-').map(Number)
  if (!y || !m || !d) return undefined
  const parsed = new Date(y, m - 1, d)
  return Number.isNaN(parsed.getTime()) ? undefined : parsed
}

function formatDateOnly(date: Date): string {
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

function formatDisplayDate(dateStr: string, locale: string): string {
  const parsed = parseDateOnly(dateStr)
  if (!parsed) return dateStr
  return new Intl.DateTimeFormat(locale, {
    month: 'short',
    day: 'numeric',
  }).format(parsed)
}

export function AuditDateRangeFilter({
  from,
  to,
  onChange,
}: AuditDateRangeFilterProps) {
  const { t, i18n } = useTranslation('audit')
  const [open, setOpen] = useState(false)

  const selected: DateRange | undefined =
    from || to
      ? {
          from: parseDateOnly(from),
          to: parseDateOnly(to),
        }
      : undefined

  const applyPresetDays = (daysBack: number) => {
    const end = new Date()
    const start = new Date()
    start.setDate(end.getDate() - daysBack)
    onChange({
      from: formatDateOnly(start),
      to: formatDateOnly(end),
    })
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant='outline'
          size='sm'
          className={cn('h-8 border-dashed', (from || to) && 'border-solid')}
        >
          <CalendarIcon className='size-4' />
          <span>{t('filters.timeRange')}</span>
          {(from || to) && (
            <>
              <Separator orientation='vertical' className='mx-2 h-4' />
              <Badge
                variant='secondary'
                className='rounded-sm px-1.5 font-normal'
              >
                {from ? formatDisplayDate(from, i18n.language) : '...'}
                {' - '}
                {to ? formatDisplayDate(to, i18n.language) : '...'}
              </Badge>
            </>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent className='w-auto p-0' align='start'>
        <div className='flex flex-wrap gap-1 border-b p-2'>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='h-7 px-2 text-xs'
            onClick={() => applyPresetDays(0)}
          >
            {t('filters.today')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='h-7 px-2 text-xs'
            onClick={() => applyPresetDays(6)}
          >
            {t('filters.last7Days')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='h-7 px-2 text-xs'
            onClick={() => applyPresetDays(29)}
          >
            {t('filters.last30Days')}
          </Button>
        </div>
        <Calendar
          mode='range'
          selected={selected}
          onSelect={(range) => {
            if (!range) {
              onChange({ from: undefined, to: undefined })
              return
            }
            const nextFrom = range.from ? formatDateOnly(range.from) : undefined
            const nextTo = range.to ? formatDateOnly(range.to) : undefined
            onChange({ from: nextFrom, to: nextTo })
          }}
          numberOfMonths={1}
        />
        {(from || to) && (
          <div className='border-t p-2'>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              className='w-full text-xs'
              onClick={() => {
                onChange({ from: undefined, to: undefined })
                setOpen(false)
              }}
            >
              {t('filters.clearDates')}
            </Button>
          </div>
        )}
      </PopoverContent>
    </Popover>
  )
}

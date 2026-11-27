import { useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { cn } from '@/lib/utils'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import type { DashboardAuditStats, DailyActivityPoint } from '../types'
import { formatCount, formatShortUtcDate } from './format'

type ActivityTrendChartProps = {
  trend: DailyActivityPoint[]
  audit: DashboardAuditStats
  rangeDays: number
}

type SeriesFilter = 'all' | 'logins' | 'operations' | 'failures'

type ChartRow = DailyActivityPoint & {
  label: string
}

type TooltipPayloadItem = {
  dataKey?: string | number
  value?: number | string
  color?: string
}

type ActivityTooltipProps = {
  active?: boolean
  payload?: ReadonlyArray<TooltipPayloadItem>
  label?: string | number
  locale: string
  seriesLabels: Record<string, string>
}

function ActivityTooltip({
  active,
  payload,
  label,
  locale,
  seriesLabels,
}: ActivityTooltipProps) {
  if (!active || !payload || payload.length === 0) {
    return null
  }
  return (
    <div className='min-w-36 rounded-lg border bg-popover/95 px-3 py-2 text-xs text-popover-foreground shadow-md backdrop-blur-xs'>
      <p className='mb-1.5 font-medium text-foreground'>
        {String(label ?? '')}
      </p>
      <div className='space-y-1'>
        {payload.map((entry) => {
          const key = String(entry.dataKey ?? '')
          const numericValue =
            typeof entry.value === 'number'
              ? entry.value
              : Number(entry.value ?? 0)
          return (
            <div key={key} className='flex items-center justify-between gap-4'>
              <span className='flex items-center gap-1.5 text-muted-foreground'>
                <span
                  className='size-2 rounded-full'
                  style={{ backgroundColor: entry.color }}
                />
                {seriesLabels[key] ?? key}
              </span>
              <span className='font-mono font-medium tabular-nums'>
                {formatCount(numericValue, locale)}
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}

export function ActivityTrendChart({
  trend,
  audit,
  rangeDays,
}: ActivityTrendChartProps) {
  const { t, i18n } = useTranslation('dashboard')
  const locale = i18n.language
  const gradientPrefix = useId().replace(/:/g, '')
  const [activeFilter, setActiveFilter] = useState<SeriesFilter>('all')

  const chartData = useMemo<ChartRow[]>(
    () =>
      trend.map((point) => ({
        ...point,
        label: formatShortUtcDate(point.date, locale),
      })),
    [trend, locale]
  )

  const operationsInRange = useMemo(
    () => trend.reduce((sum, point) => sum + point.operations, 0),
    [trend]
  )

  const seriesLabels = useMemo<Record<string, string>>(
    () => ({
      logins: t('trend.series.logins'),
      operations: t('trend.series.operations'),
      failures: t('trend.series.failures'),
    }),
    [t]
  )

  const filterTabs: Array<{
    key: SeriesFilter
    label: string
    value: number
    dotClass?: string
  }> = [
    {
      key: 'all',
      label: t('trend.tabs.all'),
      value: audit.totalInRange,
    },
    {
      key: 'logins',
      label: seriesLabels.logins,
      value: audit.loginsInRange,
      dotClass: 'bg-chart-1',
    },
    {
      key: 'operations',
      label: seriesLabels.operations,
      value: operationsInRange,
      dotClass: 'bg-chart-2',
    },
    {
      key: 'failures',
      label: seriesLabels.failures,
      value: audit.failuresInRange,
      dotClass: 'bg-destructive',
    },
  ]

  const showLogins = activeFilter === 'all' || activeFilter === 'logins'
  const showOperations = activeFilter === 'all' || activeFilter === 'operations'
  const showFailures = activeFilter === 'all' || activeFilter === 'failures'

  const loginsGradId = `${gradientPrefix}-logins`
  const operationsGradId = `${gradientPrefix}-operations`
  const failuresGradId = `${gradientPrefix}-failures`

  return (
    <Card
      data-testid='dashboard-trend-card'
      className='flex flex-col gap-0 overflow-hidden py-0 shadow-xs xl:col-span-8'
    >
      <CardHeader className='flex min-h-[84px] flex-col items-stretch gap-0 space-y-0 border-b p-0 lg:flex-row [.border-b]:pb-0'>
        <div className='flex flex-1 flex-col justify-center gap-1 px-6 py-5'>
          <CardTitle className='text-base'>{t('trend.title')}</CardTitle>
          <CardDescription>
            {t('trend.description', {
              days: rangeDays,
              total: formatCount(audit.totalInRange, locale),
            })}
          </CardDescription>
        </div>

        <div
          role='group'
          aria-label={t('trend.seriesFilterLabel')}
          className='grid grid-cols-2 divide-x divide-y border-t sm:grid-cols-4 sm:divide-y-0 lg:border-s lg:border-t-0'
        >
          {filterTabs.map((tab) => {
            const isSelected = activeFilter === tab.key
            return (
              <button
                key={tab.key}
                type='button'
                data-testid={`dashboard-series-${tab.key}`}
                aria-pressed={isSelected}
                onClick={() => setActiveFilter(tab.key)}
                className={cn(
                  'relative flex flex-col justify-center gap-1 px-4 py-3.5 text-start transition-colors hover:bg-muted/40 focus-visible:z-10 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden',
                  isSelected && 'bg-muted/50'
                )}
              >
                {isSelected && (
                  <span className='absolute inset-x-0 top-0 h-0.5 bg-primary' />
                )}
                <span className='flex items-center gap-1.5 text-[11px] font-medium whitespace-nowrap text-muted-foreground'>
                  {tab.dotClass && (
                    <span
                      className={cn(
                        'size-2 shrink-0 rounded-full',
                        tab.dotClass
                      )}
                    />
                  )}
                  {tab.label}
                </span>
                <span className='text-lg leading-none font-semibold tracking-tight tabular-nums sm:text-xl'>
                  {formatCount(tab.value, locale)}
                </span>
              </button>
            )
          })}
        </div>
      </CardHeader>

      <CardContent className='flex-1 px-4 pt-6 pb-5 sm:px-6'>
        {audit.totalInRange === 0 ? (
          <div
            data-testid='dashboard-trend-empty'
            className='flex h-[300px] flex-col items-center justify-center rounded-lg border border-dashed bg-muted/15 text-center'
          >
            <p className='text-sm font-medium'>{t('trend.emptyTitle')}</p>
            <p className='mt-1 max-w-xs text-xs text-muted-foreground'>
              {t('trend.emptyDesc', { days: rangeDays })}
            </p>
          </div>
        ) : (
          <div className='h-[300px] w-full' data-testid='dashboard-trend-chart'>
            <ResponsiveContainer width='100%' height={300} minWidth={0}>
              <AreaChart
                data={chartData}
                margin={{ top: 10, right: 12, left: -16, bottom: 0 }}
              >
                <defs>
                  <linearGradient id={loginsGradId} x1='0' y1='0' x2='0' y2='1'>
                    <stop
                      offset='5%'
                      stopColor='var(--chart-1)'
                      stopOpacity={0.32}
                    />
                    <stop
                      offset='95%'
                      stopColor='var(--chart-1)'
                      stopOpacity={0.02}
                    />
                  </linearGradient>
                  <linearGradient
                    id={operationsGradId}
                    x1='0'
                    y1='0'
                    x2='0'
                    y2='1'
                  >
                    <stop
                      offset='5%'
                      stopColor='var(--chart-2)'
                      stopOpacity={0.32}
                    />
                    <stop
                      offset='95%'
                      stopColor='var(--chart-2)'
                      stopOpacity={0.02}
                    />
                  </linearGradient>
                  <linearGradient
                    id={failuresGradId}
                    x1='0'
                    y1='0'
                    x2='0'
                    y2='1'
                  >
                    <stop
                      offset='5%'
                      stopColor='var(--destructive)'
                      stopOpacity={0.28}
                    />
                    <stop
                      offset='95%'
                      stopColor='var(--destructive)'
                      stopOpacity={0.02}
                    />
                  </linearGradient>
                </defs>
                <CartesianGrid
                  strokeDasharray='3 3'
                  vertical={false}
                  stroke='var(--border)'
                />
                <XAxis
                  dataKey='label'
                  tickLine={false}
                  axisLine={false}
                  tickMargin={10}
                  minTickGap={24}
                  tick={{ fontSize: 12, fill: 'var(--muted-foreground)' }}
                />
                <YAxis
                  allowDecimals={false}
                  tickLine={false}
                  axisLine={false}
                  tickMargin={8}
                  tick={{ fontSize: 12, fill: 'var(--muted-foreground)' }}
                />
                <Tooltip
                  cursor={{
                    stroke: 'var(--muted-foreground)',
                    strokeWidth: 1,
                    strokeDasharray: '4 4',
                  }}
                  content={(props) => (
                    <ActivityTooltip
                      active={props.active}
                      payload={
                        props.payload as ReadonlyArray<TooltipPayloadItem>
                      }
                      label={props.label}
                      locale={locale}
                      seriesLabels={seriesLabels}
                    />
                  )}
                />
                {showOperations && (
                  <Area
                    type='monotone'
                    dataKey='operations'
                    stroke='var(--chart-2)'
                    strokeWidth={2}
                    fill={`url(#${operationsGradId})`}
                    activeDot={{ r: 4, strokeWidth: 0 }}
                  />
                )}
                {showLogins && (
                  <Area
                    type='monotone'
                    dataKey='logins'
                    stroke='var(--chart-1)'
                    strokeWidth={2}
                    fill={`url(#${loginsGradId})`}
                    activeDot={{ r: 4, strokeWidth: 0 }}
                  />
                )}
                {showFailures && (
                  <Area
                    type='monotone'
                    dataKey='failures'
                    stroke='var(--destructive)'
                    strokeWidth={2}
                    fill={`url(#${failuresGradId})`}
                    activeDot={{ r: 4, strokeWidth: 0 }}
                  />
                )}
              </AreaChart>
            </ResponsiveContainer>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertCircle, RefreshCw } from 'lucide-react'
import { formatServerErrorMessage } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { LanguageSwitch } from '@/components/shared/language-switch'
import { Search } from '@/components/shared/search'
import { ThemeSwitch } from '@/components/shared/theme-switch'
import { ActivityTrendChart } from './components/activity-trend-chart'
import { formatActivityTimestamp } from './components/format'
import { KpiCards } from './components/kpi-cards'
import { RecentActivityCard } from './components/recent-activity-card'
import { dashboardSummaryQueryOptions } from './queries'
import type { DashboardRangeDays } from './types'

const RANGE_OPTIONS: DashboardRangeDays[] = [7, 14, 30]

function DashboardSkeleton() {
  return (
    <div className='space-y-4'>
      <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4'>
        {[1, 2, 3, 4].map((idx) => (
          <Card key={idx} className='gap-0 overflow-hidden py-0'>
            <CardHeader className='flex flex-row items-center justify-between space-y-0 px-5 pt-5 pb-2'>
              <Skeleton className='h-3.5 w-24' />
              <Skeleton className='h-5 w-14 rounded-full' />
            </CardHeader>
            <CardContent className='space-y-3 px-5 pb-4'>
              <Skeleton className='h-9 w-24' />
              <Skeleton className='h-1.5 w-full rounded-full' />
            </CardContent>
            <div className='border-t bg-muted/20 px-5 py-2.5'>
              <Skeleton className='h-3.5 w-36' />
            </div>
          </Card>
        ))}
      </div>

      <div className='grid grid-cols-1 gap-4 xl:grid-cols-12'>
        <Card className='gap-0 overflow-hidden py-0 xl:col-span-8'>
          <CardHeader className='flex flex-row items-center justify-between gap-0 border-b px-6 py-5 [.border-b]:pb-5'>
            <div className='space-y-2'>
              <Skeleton className='h-5 w-40' />
              <Skeleton className='h-4 w-64' />
            </div>
            <div className='hidden gap-4 sm:flex'>
              <Skeleton className='h-10 w-20' />
              <Skeleton className='h-10 w-20' />
              <Skeleton className='h-10 w-20' />
            </div>
          </CardHeader>
          <CardContent className='px-6 pt-6 pb-5'>
            <Skeleton className='h-[300px] w-full' />
          </CardContent>
        </Card>
        <Card className='gap-0 overflow-hidden py-0 xl:col-span-4'>
          <CardHeader className='border-b px-6 py-5 [.border-b]:pb-5'>
            <Skeleton className='h-5 w-32' />
            <Skeleton className='h-4 w-48' />
          </CardHeader>
          <CardContent className='space-y-4 p-6'>
            {[1, 2, 3, 4, 5].map((idx) => (
              <div key={idx} className='flex items-start gap-3'>
                <Skeleton className='mt-1 size-2.5 rounded-full' />
                <div className='flex-1 space-y-1.5'>
                  <Skeleton className='h-3.5 w-44' />
                  <Skeleton className='h-3 w-28' />
                </div>
                <Skeleton className='h-4 w-12 rounded-full' />
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

export function Dashboard() {
  const { t, i18n } = useTranslation('dashboard')
  const { t: tCommon } = useTranslation('common')
  const queryClient = useQueryClient()
  const [rangeDays, setRangeDays] = useState<DashboardRangeDays>(7)

  const { data, isLoading, isFetching, isError, error, refetch } = useQuery(
    dashboardSummaryQueryOptions(rangeDays)
  )

  return (
    <>
      <Header fixed>
        <Search />
        <div className='ms-auto flex items-center space-x-2'>
          <LanguageSwitch
            onLanguageChange={() => {
              void queryClient.invalidateQueries()
            }}
          />
          <ThemeSwitch />
        </div>
      </Header>

      <Main fluid className='flex flex-1 flex-col gap-5 sm:gap-6'>
        <div className='flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between'>
          <div className='space-y-1'>
            <div className='flex flex-wrap items-center gap-2.5'>
              <h1
                data-testid='dashboard-page-title'
                className='text-2xl font-bold tracking-tight'
              >
                {t('title')}
              </h1>
              {data?.generatedAt && (
                <span className='inline-flex items-center gap-1.5 rounded-full border bg-muted/40 px-2.5 py-0.5 font-mono text-[11px] text-muted-foreground'>
                  <span
                    aria-hidden='true'
                    className='size-1.5 rounded-full bg-success'
                  />
                  {t('updatedAt', {
                    time: formatActivityTimestamp(
                      data.generatedAt,
                      i18n.language
                    ),
                  })}
                </span>
              )}
            </div>
            <p className='text-sm text-muted-foreground'>{t('description')}</p>
          </div>

          <div className='flex flex-wrap items-center gap-2'>
            <div
              role='group'
              aria-label={t('rangeLabel')}
              className='inline-flex items-center rounded-lg border bg-muted/40 p-0.5'
            >
              {RANGE_OPTIONS.map((days) => {
                const active = rangeDays === days
                return (
                  <Button
                    key={days}
                    type='button'
                    size='sm'
                    variant='ghost'
                    data-testid={`dashboard-range-${days}`}
                    aria-pressed={active}
                    className={cn(
                      'h-7 rounded-md px-3 text-xs font-medium text-muted-foreground hover:text-foreground',
                      active &&
                        'bg-background text-foreground shadow-2xs hover:bg-background'
                    )}
                    onClick={() => setRangeDays(days)}
                  >
                    {t('rangeDays', { days })}
                  </Button>
                )
              })}
            </div>

            <Button
              type='button'
              variant='outline'
              size='sm'
              data-testid='dashboard-refresh-btn'
              disabled={isFetching}
              className='h-8'
              onClick={() => void refetch()}
            >
              <RefreshCw
                className={cn('size-3.5', isFetching && 'animate-spin')}
              />
              {t('refresh')}
            </Button>
          </div>
        </div>

        {isLoading ? (
          <DashboardSkeleton />
        ) : isError || !data ? (
          <Alert variant='destructive' data-testid='dashboard-error-alert'>
            <AlertCircle className='size-4' />
            <AlertTitle>{t('error.loadFailed')}</AlertTitle>
            <AlertDescription className='flex flex-wrap items-center justify-between gap-2'>
              <span>{formatServerErrorMessage(error)}</span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => void refetch()}
              >
                <RefreshCw className='size-3.5' />
                {tCommon('retry')}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <div className='space-y-4'>
            <KpiCards summary={data} />
            <div className='grid grid-cols-1 gap-4 xl:grid-cols-12'>
              <ActivityTrendChart
                trend={data.activityTrend}
                audit={data.audit}
                rangeDays={data.rangeDays}
              />
              <RecentActivityCard items={data.recentActivity} />
            </div>
          </div>
        )}
      </Main>
    </>
  )
}

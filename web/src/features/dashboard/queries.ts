import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { fetchDashboardSummary } from './api'
import type { DashboardRangeDays } from './types'

export const dashboardKeys = {
  all: ['dashboard'] as const,
  summary: (days: DashboardRangeDays) =>
    ['dashboard', 'summary', days] as const,
}

export function dashboardSummaryQueryOptions(days: DashboardRangeDays) {
  return queryOptions({
    queryKey: dashboardKeys.summary(days),
    queryFn: ({ signal }) => fetchDashboardSummary(days, signal),
    staleTime: 30_000,
    placeholderData: keepPreviousData,
    meta: { silent: true },
  })
}

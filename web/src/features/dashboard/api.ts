import { apiClient } from '@/lib/api/client'
import type { DashboardRangeDays, DashboardSummary } from './types'

export async function fetchDashboardSummary(
  days: DashboardRangeDays,
  signal?: AbortSignal
): Promise<DashboardSummary> {
  const res = await apiClient.get<DashboardSummary>('/dashboard/summary', {
    params: { days },
    signal,
  })
  return res.data
}

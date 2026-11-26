import { queryOptions } from '@tanstack/react-query'
import { apiClient } from './client'

export type SetupStatus = {
  initialized: boolean
  mode: 'token' | 'local' | 'open' | string
  requiresToken: boolean
}

export const setupQueryKeys = {
  status: ['setup', 'status'] as const,
}

export async function fetchSetupStatus(): Promise<SetupStatus> {
  const res = await apiClient.get<SetupStatus>('/setup/status')
  return res.data
}

export const setupStatusQueryOptions = queryOptions({
  queryKey: setupQueryKeys.status,
  queryFn: fetchSetupStatus,
  staleTime: 30_000,
  meta: { silent: true },
})

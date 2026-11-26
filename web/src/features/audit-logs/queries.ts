import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { fetchAuditLogs } from './api'
import type { AuditLogsListParams } from './types'

export const auditLogsKeys = {
  all: ['audit-logs'] as const,
  list: (params: AuditLogsListParams) =>
    ['audit-logs', 'list', params] as const,
}

export function auditLogsQueryOptions(params: AuditLogsListParams) {
  return queryOptions({
    queryKey: auditLogsKeys.list(params),
    queryFn: ({ signal }) => fetchAuditLogs(params, signal),
    placeholderData: keepPreviousData,
    meta: { silent: true },
  })
}

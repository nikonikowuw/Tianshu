import { apiClient } from '@/lib/api/client'
import { toBoundaryRFC3339 } from './date'
import type { AuditLogsListParams, AuditLogsPage } from './types'

export function buildAuditLogsQueryParams(
  params: AuditLogsListParams
): URLSearchParams {
  const sp = new URLSearchParams()
  sp.set('page', String(params.page))
  sp.set('pageSize', String(params.pageSize))

  if (params.q) {
    sp.set('q', params.q)
  }
  if (params.actor) {
    sp.set('actor', params.actor)
  }
  for (const st of params.status ?? []) {
    sp.append('status', st)
  }
  for (const act of params.action ?? []) {
    sp.append('action', act)
  }
  for (const res of params.resource ?? []) {
    sp.append('resource', res)
  }

  const fromIso = params.from ? toBoundaryRFC3339(params.from, false) : null
  const toIso = params.to ? toBoundaryRFC3339(params.to, true) : null
  if (fromIso) {
    sp.set('from', fromIso)
  }
  if (toIso) {
    sp.set('to', toIso)
  }
  if (params.sort) {
    sp.set('sort', params.sort)
  }
  if (params.order) {
    sp.set('order', params.order)
  }

  return sp
}

export async function fetchAuditLogs(
  params: AuditLogsListParams,
  signal?: AbortSignal
): Promise<AuditLogsPage> {
  const searchParams = buildAuditLogsQueryParams(params)
  const response = await apiClient.get<AuditLogsPage>('/audit-logs', {
    params: searchParams,
    signal,
  })
  return response.data
}

import { apiClient } from '@/lib/api/client'
import type { AuditLogsListParams, AuditLogsPage } from './types'

function toBoundaryRFC3339(dateStr: string, endOfDay: boolean): string | null {
  const parts = dateStr.split('-').map(Number)
  if (parts.length !== 3 || parts.some((n) => !Number.isFinite(n))) {
    return null
  }
  const [year, month, day] = parts
  const d = endOfDay
    ? new Date(year, month - 1, day, 23, 59, 59, 999)
    : new Date(year, month - 1, day, 0, 0, 0, 0)
  if (Number.isNaN(d.getTime())) {
    return null
  }
  return d.toISOString()
}

export function buildAuditLogsQueryParams(
  params: AuditLogsListParams
): URLSearchParams {
  const sp = new URLSearchParams()
  sp.set('page', String(params.page))
  sp.set('pageSize', String(params.pageSize))

  if (params.q) {
    sp.set('q', params.q)
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

  let fromIso = params.from ? toBoundaryRFC3339(params.from, false) : null
  let toIso = params.to ? toBoundaryRFC3339(params.to, true) : null
  if (fromIso && toIso && fromIso > toIso && params.from && params.to) {
    fromIso = toBoundaryRFC3339(params.to, false)
    toIso = toBoundaryRFC3339(params.from, true)
  }
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

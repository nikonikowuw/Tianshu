import type { Paginated } from '@/lib/api/types'

export type AuditLogStatus = 'success' | 'failure'

export type AuditLogItem = {
  id: string
  actorId: string | null
  actorUsername: string
  action: string
  resource: string
  resourceId: string
  status: AuditLogStatus
  ipAddress: string
  userAgent: string
  requestId: string
  metadata: Record<string, unknown>
  createdAt: string
}

export type AuditLogsListParams = {
  page: number
  pageSize: number
  q?: string
  actor?: string
  status?: string[]
  action?: string[]
  resource?: string[]
  from?: string
  to?: string
  sort?: 'created_at' | 'action' | 'resource' | 'status'
  order?: 'asc' | 'desc'
}

export type AuditLogsPage = Paginated<AuditLogItem>

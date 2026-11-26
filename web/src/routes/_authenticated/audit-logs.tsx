import { createFileRoute, redirect } from '@tanstack/react-router'
import { AuditLogs } from '@/features/audit-logs'
import { auditLogsSearchSchema } from '@/features/audit-logs/schema'

export const Route = createFileRoute('/_authenticated/audit-logs')({
  validateSearch: auditLogsSearchSchema,
  beforeLoad: ({ context }) => {
    if (!context.auth.hasPermission('audit:read')) {
      throw redirect({ to: '/403' })
    }
  },
  component: AuditLogs,
})

import { z } from 'zod'

const dateOnlyPattern = /^\d{4}-\d{2}-\d{2}$/

const stringOrArray = z
  .union([z.array(z.string()), z.string()])
  .transform((val) => {
    const items = Array.isArray(val) ? val : val.split(',')
    return items.map((s) => s.trim()).filter(Boolean)
  })

export const auditLogsSearchSchema = z.object({
  page: z.coerce
    .number()
    .int()
    .min(1)
    .optional()
    .transform((v) => v ?? 1)
    .catch(1),
  pageSize: z.coerce
    .number()
    .int()
    .refine((n) => [10, 20, 50, 100].includes(n), {
      message: 'Unsupported pageSize',
    })
    .optional()
    .transform((v) => v ?? 20)
    .catch(20),
  q: z
    .string()
    .trim()
    .transform((v) => (v === '' ? undefined : v))
    .optional()
    .catch(undefined),
  status: stringOrArray
    .transform((arr) => arr.filter((v) => v === 'success' || v === 'failure'))
    .optional()
    .catch([]),
  action: stringOrArray.optional().catch([]),
  resource: stringOrArray.optional().catch([]),
  from: z
    .string()
    .trim()
    .refine((v) => dateOnlyPattern.test(v))
    .optional()
    .catch(undefined),
  to: z
    .string()
    .trim()
    .refine((v) => dateOnlyPattern.test(v))
    .optional()
    .catch(undefined),
  sort: z
    .enum(['created_at', 'action', 'resource', 'status'])
    .optional()
    .catch(undefined),
  order: z.enum(['asc', 'desc']).optional().catch(undefined),
})

export type AuditLogsSearch = z.infer<typeof auditLogsSearchSchema>

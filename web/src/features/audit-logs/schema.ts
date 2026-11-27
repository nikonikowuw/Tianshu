import { z } from 'zod'
import { isValidDateOnly } from './date'

const stringOrArray = z
  .union([z.array(z.string()), z.string()])
  .transform((val) => {
    const items = Array.isArray(val) ? val : val.split(',')
    return items.map((s) => s.trim()).filter(Boolean)
  })

const dateOnly = z
  .string()
  .trim()
  .refine(isValidDateOnly, { message: 'Invalid calendar date' })
  .optional()
  .catch(undefined)

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
    .max(100)
    .transform((v) => (v === '' ? undefined : v))
    .optional()
    .catch(undefined),
  actor: z
    .string()
    .trim()
    .max(100)
    .transform((v) => (v === '' ? undefined : v))
    .optional()
    .catch(undefined),
  status: stringOrArray
    .transform((arr) => arr.filter((v) => v === 'success' || v === 'failure'))
    .optional()
    .catch([]),
  action: stringOrArray.optional().catch([]),
  resource: stringOrArray.optional().catch([]),
  from: dateOnly,
  to: dateOnly,
  sort: z
    .enum(['created_at', 'action', 'resource', 'status'])
    .optional()
    .catch(undefined),
  order: z.enum(['asc', 'desc']).optional().catch(undefined),
})

export type AuditLogsSearch = z.infer<typeof auditLogsSearchSchema>

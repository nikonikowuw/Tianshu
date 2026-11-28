import { z } from 'zod'
import { isValidDateOnly } from './date'

const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const NIL_UUID = '00000000-0000-0000-0000-000000000000'
const MAX_FILTER_TOKEN_LEN = 64
const MAX_MULTI_FILTER_ITEMS = 20

function optionalTrimmedText(maxLen: number) {
  return z
    .preprocess(
      (val) =>
        typeof val === 'number' || typeof val === 'boolean' ? String(val) : val,
      z
        .string()
        .trim()
        .max(maxLen)
        .transform((v) => (v === '' ? undefined : v))
    )
    .optional()
    .catch(undefined)
}

const stringOrArray = z
  .union([z.array(z.string()), z.string()])
  .transform((val) => {
    const items = Array.isArray(val) ? val : val.split(',')
    return items.map((s) => s.trim()).filter(Boolean)
  })

function boundedTokenList() {
  return stringOrArray
    .transform((arr) =>
      Array.from(
        new Set(arr.filter((s) => s.length <= MAX_FILTER_TOKEN_LEN))
      ).slice(0, MAX_MULTI_FILTER_ITEMS)
    )
    .optional()
    .catch([])
}

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
  q: optionalTrimmedText(100),
  actor: optionalTrimmedText(100),
  actorId: z
    .string()
    .trim()
    .refine((v) => UUID_PATTERN.test(v) && v.toLowerCase() !== NIL_UUID, {
      message: 'Invalid actorId UUID',
    })
    .optional()
    .catch(undefined),
  status: stringOrArray
    .transform((arr) =>
      Array.from(new Set(arr.filter((v) => v === 'success' || v === 'failure')))
    )
    .optional()
    .catch([]),
  action: boundedTokenList(),
  resource: boundedTokenList(),
  from: dateOnly,
  to: dateOnly,
  sort: z
    .enum(['created_at', 'action', 'resource', 'status'])
    .optional()
    .catch(undefined),
  order: z.enum(['asc', 'desc']).optional().catch(undefined),
})

export type AuditLogsSearch = z.infer<typeof auditLogsSearchSchema>

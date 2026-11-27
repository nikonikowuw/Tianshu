import { z } from 'zod'

const stringOrArray = z
  .union([z.array(z.string()), z.string()])
  .transform((val) => {
    const items = Array.isArray(val) ? val : val.split(',')
    return items.map((s) => s.trim()).filter(Boolean)
  })

export const filesSearchSchema = z.object({
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
  category: stringOrArray
    .transform((arr) =>
      Array.from(new Set(arr.filter((v) => v === 'image' || v === 'document')))
    )
    .optional()
    .catch([]),
  driver: stringOrArray
    .transform((arr) =>
      Array.from(new Set(arr.filter((v) => v === 'local' || v === 's3')))
    )
    .optional()
    .catch([]),
  sort: z
    .enum(['created_at', 'original_name', 'size_bytes'])
    .optional()
    .catch(undefined),
  order: z.enum(['asc', 'desc']).optional().catch(undefined),
})

export type FilesSearch = z.infer<typeof filesSearchSchema>

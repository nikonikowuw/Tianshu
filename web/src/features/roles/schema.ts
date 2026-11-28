import { z } from 'zod'
import { type TFunction } from 'i18next'

const roleCodePattern = /^[a-z][a-z0-9_-]{1,63}$/

const stringOrArray = z
  .union([z.array(z.string()), z.string()])
  .transform((val) => {
    const items = Array.isArray(val) ? val : val.split(',')
    return items.map((s) => s.trim()).filter(Boolean)
  })

export const rolesSearchSchema = z.object({
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
    .preprocess(
      (val) =>
        typeof val === 'number' || typeof val === 'boolean' ? String(val) : val,
      z
        .string()
        .trim()
        .max(100)
        .transform((v) => (v === '' ? undefined : v))
    )
    .optional()
    .catch(undefined),
  type: stringOrArray
    .transform((arr) =>
      Array.from(new Set(arr.filter((v) => v === 'system' || v === 'custom')))
    )
    .optional()
    .catch([]),
  sort: z
    .enum(['created_at', 'updated_at', 'code', 'name'])
    .optional()
    .catch(undefined),
  order: z.enum(['asc', 'desc']).optional().catch(undefined),
})

export type RolesSearch = z.infer<typeof rolesSearchSchema>

export function createRoleFormSchema(t: TFunction<'validation'>) {
  return z.object({
    code: z
      .string()
      .trim()
      .toLowerCase()
      .min(1, t('required'))
      .refine((v) => roleCodePattern.test(v), t('roleCodeFormat')),
    name: z.string().trim().min(1, t('required')).max(100, t('roleNameMax')),
    description: z.string().trim().max(500, t('roleDescriptionMax')),
    permissions: z.array(z.string()),
  })
}

export function updateRoleFormSchema(t: TFunction<'validation'>) {
  return z.object({
    code: z.string(),
    name: z.string().trim().min(1, t('required')).max(100, t('roleNameMax')),
    description: z.string().trim().max(500, t('roleDescriptionMax')),
    permissions: z.array(z.string()),
  })
}

export type RoleFormValues = {
  code: string
  name: string
  description: string
  permissions: string[]
}

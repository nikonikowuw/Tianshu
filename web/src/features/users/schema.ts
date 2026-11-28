import { z } from 'zod'
import { type TFunction } from 'i18next'

const usernamePattern = /^[a-z0-9._-]+$/
const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const nilUuid = '00000000-0000-0000-0000-000000000000'
const maxRoleFilters = 20

const stringOrArray = z
  .union([z.array(z.string()), z.string()])
  .transform((val) => {
    const items = Array.isArray(val) ? val : val.split(',')
    return items.map((s) => s.trim()).filter(Boolean)
  })

export const usersSearchSchema = z.object({
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
  status: stringOrArray
    .transform((arr) =>
      Array.from(new Set(arr.filter((v) => v === 'active' || v === 'disabled')))
    )
    .optional()
    .catch([]),
  roleId: stringOrArray
    .transform((arr) =>
      Array.from(
        new Set(
          arr
            .filter((v) => uuidPattern.test(v))
            .map((v) => v.toLowerCase())
            .filter((v) => v !== nilUuid)
        )
      ).slice(0, maxRoleFilters)
    )
    .optional()
    .catch([]),
  sort: z
    .enum(['created_at', 'updated_at', 'username', 'last_login_at'])
    .optional()
    .catch(undefined),
  order: z.enum(['asc', 'desc']).optional().catch(undefined),
})

export type UsersSearch = z.infer<typeof usersSearchSchema>

export function createUserFormSchema(t: TFunction<'validation'>) {
  return z.object({
    username: z
      .string()
      .trim()
      .toLowerCase()
      .min(3, t('usernameMin'))
      .max(64, t('usernameMax'))
      .refine((v) => usernamePattern.test(v), t('usernameFormat')),
    email: z
      .string()
      .trim()
      .toLowerCase()
      .min(1, t('required'))
      .max(255, t('emailMax'))
      .email(t('invalidEmail')),
    displayName: z.string().trim().max(100, t('displayNameMax')),
    password: z.string().min(8, t('passwordMin')).max(128, t('passwordMax')),
    status: z.enum(['active', 'disabled']),
    roleIds: z.array(z.string()),
  })
}

export function updateUserFormSchema(t: TFunction<'validation'>) {
  return z.object({
    username: z.string(),
    email: z
      .string()
      .trim()
      .toLowerCase()
      .min(1, t('required'))
      .max(255, t('emailMax'))
      .email(t('invalidEmail')),
    displayName: z.string().trim().max(100, t('displayNameMax')),
    password: z.string(),
    status: z.enum(['active', 'disabled']),
    roleIds: z.array(z.string()),
  })
}

export type UserFormValues = {
  username: string
  email: string
  displayName: string
  password: string
  status: 'active' | 'disabled'
  roleIds: string[]
}

export function resetPasswordFormSchema(t: TFunction<'validation'>) {
  return z
    .object({
      password: z.string().min(8, t('passwordMin')).max(128, t('passwordMax')),
      confirmPassword: z.string().min(1, t('required')),
    })
    .refine((val) => val.password === val.confirmPassword, {
      message: t('passwordMismatch'),
      path: ['confirmPassword'],
    })
}

export type ResetPasswordFormValues = {
  password: string
  confirmPassword: string
}

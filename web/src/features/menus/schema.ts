import { z } from 'zod'
import { type TFunction } from 'i18next'

const menuCodePattern = /^[a-z][a-z0-9_-]{1,63}$/

export function menuFormSchema(t: TFunction<'validation'>) {
  return z
    .object({
      parentId: z.string(),
      type: z.enum(['directory', 'menu', 'button']),
      code: z
        .string()
        .trim()
        .toLowerCase()
        .min(1, t('required'))
        .refine((v) => menuCodePattern.test(v), t('menuCodeFormat')),
      title: z
        .string()
        .trim()
        .min(1, t('required'))
        .max(100, t('menuTitleMax')),
      path: z.string().trim().max(255, t('menuPathFormat')),
      icon: z.string().trim().max(64),
      permissionCode: z.string().trim().max(100),
      sortOrder: z.number().int().min(0).max(99999),
      visible: z.boolean(),
      status: z.enum(['active', 'disabled']),
    })
    .refine(
      (val) => {
        if (val.type === 'menu') {
          return val.path.startsWith('/')
        }
        return val.path === '' || val.path.startsWith('/')
      },
      {
        message: t('menuPathFormat'),
        path: ['path'],
      }
    )
}

export type MenuFormValues = {
  parentId: string
  type: 'directory' | 'menu' | 'button'
  code: string
  title: string
  path: string
  icon: string
  permissionCode: string
  sortOrder: number
  visible: boolean
  status: 'active' | 'disabled'
}

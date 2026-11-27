import { z } from 'zod'
import { type TFunction } from 'i18next'
import type { MenuNodeType } from './types'

const menuCodePattern = /^[a-z][a-z0-9_-]{1,63}$/
const maxMenuPathLen = 255

/** Matches backend isValidMenuPlacement rules for directory, menu, and button hierarchy. */
export function isValidMenuParentPlacement(
  parentType: MenuNodeType | null,
  childType: MenuNodeType
): boolean {
  if (parentType === null || parentType === 'directory') {
    return childType === 'directory' || childType === 'menu'
  }
  if (parentType === 'menu') {
    return childType === 'menu' || childType === 'button'
  }
  return false
}

export function menuFormSchema(
  t: TFunction<'validation'>,
  resolveParentType?: (parentId: string) => MenuNodeType | null | undefined
) {
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
      path: z.string().trim(),
      icon: z.string().trim().max(64),
      permissionCode: z.string().trim().max(100),
      sortOrder: z
        .string()
        .trim()
        .min(1, t('required'))
        .refine((v) => {
          if (!/^\d+$/.test(v)) return false
          const num = Number(v)
          return Number.isSafeInteger(num) && num >= 0 && num <= 99999
        }, t('menuSortOrderRange')),
      visible: z.boolean(),
      status: z.enum(['active', 'disabled']),
    })
    .superRefine((val, ctx) => {
      if (val.type !== 'button') {
        const invalidMenuPath =
          val.type === 'menu' &&
          (!val.path.startsWith('/') || val.path.length > maxMenuPathLen)
        const invalidDirPath =
          val.type === 'directory' &&
          val.path !== '' &&
          (!val.path.startsWith('/') || val.path.length > maxMenuPathLen)
        if (invalidMenuPath || invalidDirPath) {
          ctx.addIssue({
            code: 'custom',
            path: ['path'],
            message: t('menuPathFormat'),
          })
        }
      }

      if (resolveParentType) {
        const parentType = resolveParentType(val.parentId)
        if (
          parentType !== undefined &&
          !isValidMenuParentPlacement(parentType, val.type)
        ) {
          ctx.addIssue({
            code: 'custom',
            path: ['parentId'],
            message: t('menuInvalidParent'),
          })
        }
      }
    })
}

export type MenuFormValues = {
  parentId: string
  type: MenuNodeType
  code: string
  title: string
  path: string
  icon: string
  permissionCode: string
  sortOrder: string
  visible: boolean
  status: 'active' | 'disabled'
}

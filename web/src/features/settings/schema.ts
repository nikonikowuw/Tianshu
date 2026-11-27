import { z } from 'zod'
import type { TFunction } from 'i18next'
import { runeLength } from '@/lib/utils'

export function profileFormSchema(t: TFunction<'validation'>) {
  return z.object({
    displayName: z
      .string()
      .trim()
      .refine((v) => runeLength(v) <= 100, {
        message: t('displayNameMax'),
      }),
    email: z
      .string()
      .trim()
      .min(1, t('required'))
      .email(t('invalidEmail'))
      .refine((v) => runeLength(v) <= 255, {
        message: t('emailMax'),
      }),
  })
}

export type ProfileFormValues = z.infer<ReturnType<typeof profileFormSchema>>

export function changePasswordFormSchema(t: TFunction<'validation'>) {
  return z
    .object({
      currentPassword: z
        .string()
        .min(1, t('required'))
        .refine((v) => runeLength(v) <= 128, {
          message: t('passwordMax'),
        }),
      newPassword: z
        .string()
        .refine((v) => runeLength(v) >= 8, {
          message: t('passwordMin'),
        })
        .refine((v) => runeLength(v) <= 128, {
          message: t('passwordMax'),
        }),
      confirmPassword: z.string().min(1, t('required')),
      revokeOtherSessions: z.boolean(),
    })
    .refine((v) => v.newPassword === v.confirmPassword, {
      path: ['confirmPassword'],
      message: t('passwordMismatch'),
    })
}

export type ChangePasswordFormValues = z.infer<
  ReturnType<typeof changePasswordFormSchema>
>

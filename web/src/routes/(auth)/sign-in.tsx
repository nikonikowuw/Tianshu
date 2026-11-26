import { z } from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { sanitizeRedirect } from '@/lib/auth/gateway'
import { SignIn } from '@/features/auth/sign-in'

const signInSearchSchema = z.object({
  redirect: z
    .string()
    .optional()
    .transform((v) => (v ? sanitizeRedirect(v) : undefined))
    .catch(undefined),
})

export const Route = createFileRoute('/(auth)/sign-in')({
  validateSearch: signInSearchSchema,
  component: SignIn,
})

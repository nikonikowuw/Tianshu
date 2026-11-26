import { z } from 'zod'
import { createFileRoute, isRedirect, redirect } from '@tanstack/react-router'
import { setupStatusQueryOptions } from '@/lib/api/setup'
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
  beforeLoad: async ({ context }) => {
    try {
      const status = await context.queryClient.ensureQueryData(
        setupStatusQueryOptions
      )
      if (!status.initialized) {
        throw redirect({ to: '/setup', replace: true })
      }
    } catch (error) {
      if (isRedirect(error)) {
        throw error
      }
    }
  },
  component: SignIn,
})

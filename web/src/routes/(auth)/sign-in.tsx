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
  beforeLoad: async ({ context, search }) => {
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

    if (context.auth.getStatus() === 'unknown') {
      const ok = await context.auth.ensureSession()
      if (ok) {
        throw redirect({
          href: sanitizeRedirect(search.redirect),
          replace: true,
        })
      }
    } else if (context.auth.getStatus() === 'authenticated') {
      throw redirect({
        href: sanitizeRedirect(search.redirect),
        replace: true,
      })
    }
  },
  component: function SignInRouteComponent() {
    const { redirect: redirectTo } = Route.useSearch()
    return <SignIn redirectTo={redirectTo} />
  },
})

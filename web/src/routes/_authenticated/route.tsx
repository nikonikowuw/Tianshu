import { createFileRoute, isRedirect, redirect } from '@tanstack/react-router'
import { setupStatusQueryOptions } from '@/lib/api/setup'
import { AuthenticatedShell } from '@/app/authenticated-shell'

export const Route = createFileRoute('/_authenticated')({
  beforeLoad: async ({ context, location }) => {
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

    const ok = await context.auth.ensureSession()
    if (!ok) {
      throw redirect({
        to: '/sign-in',
        search: { redirect: location.href },
      })
    }
  },
  component: AuthenticatedShell,
})

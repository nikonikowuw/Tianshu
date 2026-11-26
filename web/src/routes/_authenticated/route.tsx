import { createFileRoute, redirect } from '@tanstack/react-router'
import { AuthenticatedShell } from '@/app/authenticated-shell'

export const Route = createFileRoute('/_authenticated')({
  beforeLoad: async ({ context, location }) => {
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

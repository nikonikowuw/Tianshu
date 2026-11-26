import { createFileRoute, isRedirect, redirect } from '@tanstack/react-router'
import { setupStatusQueryOptions } from '@/lib/api/setup'
import { SetupWizard } from '@/features/setup'

export const Route = createFileRoute('/(setup)/setup')({
  beforeLoad: async ({ context }) => {
    try {
      const status = await context.queryClient.ensureQueryData(
        setupStatusQueryOptions
      )
      if (status.initialized) {
        throw redirect({ to: '/sign-in', replace: true })
      }
    } catch (error) {
      if (isRedirect(error)) {
        throw error
      }
    }
  },
  component: () => <SetupWizard />,
})

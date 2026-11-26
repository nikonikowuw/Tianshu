import { createFileRoute, redirect } from '@tanstack/react-router'
import { Dashboard } from '@/features/dashboard'

export const Route = createFileRoute('/_authenticated/')({
  beforeLoad: ({ context }) => {
    if (!context.auth.hasPermission('dashboard:read')) {
      throw redirect({ to: '/403' })
    }
  },
  component: Dashboard,
})

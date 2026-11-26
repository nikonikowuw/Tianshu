import { createFileRoute, redirect } from '@tanstack/react-router'
import { Users } from '@/features/users'
import { usersSearchSchema } from '@/features/users/schema'

export const Route = createFileRoute('/_authenticated/users')({
  validateSearch: usersSearchSchema,
  beforeLoad: ({ context }) => {
    if (!context.auth.hasPermission('users:read')) {
      throw redirect({ to: '/403' })
    }
  },
  component: Users,
})

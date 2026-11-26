import { createFileRoute, redirect } from '@tanstack/react-router'
import { Roles } from '@/features/roles'
import { rolesSearchSchema } from '@/features/roles/schema'

export const Route = createFileRoute('/_authenticated/roles')({
  validateSearch: rolesSearchSchema,
  beforeLoad: ({ context }) => {
    if (!context.auth.hasPermission('roles:read')) {
      throw redirect({ to: '/403' })
    }
  },
  component: Roles,
})

import { createFileRoute, redirect } from '@tanstack/react-router'
import { Menus } from '@/features/menus'

export const Route = createFileRoute('/_authenticated/menus')({
  beforeLoad: ({ context }) => {
    if (!context.auth.hasPermission('menus:read')) {
      throw redirect({ to: '/403' })
    }
  },
  component: Menus,
})

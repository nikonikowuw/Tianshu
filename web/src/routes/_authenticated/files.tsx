import { createFileRoute, redirect } from '@tanstack/react-router'
import { Files } from '@/features/files'
import { filesSearchSchema } from '@/features/files/schema'

export const Route = createFileRoute('/_authenticated/files')({
  validateSearch: filesSearchSchema,
  beforeLoad: ({ context }) => {
    if (!context.auth.hasPermission('files:read')) {
      throw redirect({ to: '/403' })
    }
  },
  component: Files,
})

import { createFileRoute } from '@tanstack/react-router'
import { SecuritySection } from '@/features/settings'

export const Route = createFileRoute('/_authenticated/settings/security')({
  component: SecuritySection,
})

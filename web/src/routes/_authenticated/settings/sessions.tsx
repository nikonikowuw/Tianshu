import { createFileRoute } from '@tanstack/react-router'
import { SessionsSection } from '@/features/settings'

export const Route = createFileRoute('/_authenticated/settings/sessions')({
  component: SessionsSection,
})

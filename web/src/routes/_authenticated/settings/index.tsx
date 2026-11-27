import { createFileRoute } from '@tanstack/react-router'
import { ProfileSection } from '@/features/settings'

export const Route = createFileRoute('/_authenticated/settings/')({
  component: ProfileSection,
})

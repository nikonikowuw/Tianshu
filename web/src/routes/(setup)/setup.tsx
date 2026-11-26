import { createFileRoute } from '@tanstack/react-router'
import { SetupWizard } from '@/features/setup'

export const Route = createFileRoute('/(setup)/setup')({
  component: SetupWizard,
})

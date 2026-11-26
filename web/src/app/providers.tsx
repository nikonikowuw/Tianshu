import { useEffect } from 'react'
import { I18nextProvider } from 'react-i18next'
import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import i18n from '@/i18n/index'
import { subscribeCrossTabLogout } from '@/stores/auth-store'
import { ThemeProvider } from '@/context/theme-provider'
import { queryClient, router } from './router'

export function AppProviders() {
  useEffect(() => {
    return subscribeCrossTabLogout(() => {
      queryClient.clear()
      void router.navigate({ to: '/sign-in', replace: true })
    })
  }, [])

  return (
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <ThemeProvider>
          <RouterProvider router={router} />
        </ThemeProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
}

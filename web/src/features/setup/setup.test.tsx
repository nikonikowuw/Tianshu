import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import i18n from '@/i18n/index'
import { ThemeProvider } from '@/context/theme-provider'
import { SetupWizard } from './index'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useNavigate: () => vi.fn(),
  }
})

function renderWizard(props?: { requireSetupTokenOverride?: boolean }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <ThemeProvider>
          <SetupWizard {...props} />
        </ThemeProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
}

describe('SetupWizard', () => {
  it('shows setup token field when token protection mode is active', async () => {
    const screen = await renderWizard({ requireSetupTokenOverride: true })
    await expect
      .element(screen.getByLabelText(/Setup Token|初始化令牌/))
      .toBeVisible()
  })

  it('hides setup token field when token is not required', async () => {
    const screen = await renderWizard({ requireSetupTokenOverride: false })
    await expect
      .element(screen.getByLabelText(/Setup Token|初始化令牌/))
      .not.toBeInTheDocument()
  })
})

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

describe('SetupWizard', () => {
  it('validates minimum 8-character password without requiring a setup token', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    const screen = await render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <ThemeProvider>
            <SetupWizard />
          </ThemeProvider>
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect
      .element(screen.getByLabelText(/Setup Token|初始化令牌/))
      .not.toBeInTheDocument()

    await screen
      .getByRole('button', { name: /Complete Setup|完成初始化/ })
      .click()

    await expect
      .element(
        screen.getByText(/at least 8 characters|密码长度至少为 8 个字符/)
      )
      .toBeVisible()
  })
})

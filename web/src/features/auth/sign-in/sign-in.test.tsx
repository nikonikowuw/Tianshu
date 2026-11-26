import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import i18n from '@/i18n/index'
import { ThemeProvider } from '@/context/theme-provider'
import { SignIn } from './index'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useNavigate: () => vi.fn(),
  }
})

describe('SignIn', () => {
  it('validates required fields before submitting', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })

    const screen = await render(
      <QueryClientProvider client={queryClient}>
        <I18nextProvider i18n={i18n}>
          <ThemeProvider>
            <SignIn />
          </ThemeProvider>
        </I18nextProvider>
      </QueryClientProvider>
    )

    await screen.getByRole('button', { name: /Sign in|登录/ }).click()

    await expect
      .element(
        screen.getByText(/This field is required|此字段为必填项/).first()
      )
      .toBeVisible()
  })
})

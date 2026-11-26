import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import i18n from '@/i18n/index'
import { ApiError } from '@/lib/api/error'
import { ThemeProvider } from '@/context/theme-provider'
import { SignIn } from './index'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useNavigate: () => vi.fn(),
  }
})

vi.mock('@/components/layout/auth-three-backdrop', () => ({
  default: () => null,
}))

const loginMock = vi.fn()

vi.mock('@/lib/api/client', () => ({
  apiClient: {
    post: (...args: unknown[]) => loginMock(...args),
  },
}))

function renderSignIn() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })

  return render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <ThemeProvider>
          <SignIn />
        </ThemeProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
}

async function submitCredentials(screen: Awaited<ReturnType<typeof render>>) {
  await screen
    .getByLabelText(/^(Username or Email|用户名或邮箱)$/)
    .fill('admin')
  await screen.getByLabelText(/^(Password|密码) */).fill('password123')
  await screen.getByRole('button', { name: /^(Sign in|登录)$/ }).click()
}

describe('SignIn', () => {
  afterEach(() => {
    loginMock.mockReset()
  })

  it('validates required fields before submitting', async () => {
    const screen = await renderSignIn()

    await screen.getByRole('button', { name: /^(Sign in|登录)$/ }).click()

    await expect
      .element(
        screen.getByText(/This field is required|此字段为必填项/).first()
      )
      .toBeVisible()
    expect(loginMock).not.toHaveBeenCalled()
  })

  it('keeps a visible, announced banner with countdown and re-enables submit after cooldown', async () => {
    loginMock.mockRejectedValueOnce(
      new ApiError({
        status: 429,
        code: 'RATE_LIMITED',
        message: 'Too many requests',
        retryAfter: 1,
      })
    )

    const screen = await renderSignIn()
    await submitCredentials(screen)

    const alert = screen.getByRole('alert')
    await expect
      .element(alert)
      .toHaveTextContent(/Too many attempts|尝试次数过多/)
    await expect.element(alert).toHaveTextContent(/1s|1 秒/)

    const submit = screen.getByRole('button', { name: /^(Sign in|登录)$/ })
    await expect.element(submit).toBeDisabled()

    await expect.element(submit, { timeout: 3000 }).toBeEnabled()
    await expect.element(alert).not.toBeInTheDocument()
  })

  it('clears a stale failure banner once the user edits any field', async () => {
    loginMock.mockRejectedValueOnce(
      new ApiError({
        status: 401,
        code: 'AUTH_INVALID_CREDENTIALS',
        message: 'Invalid username or password',
      })
    )

    const screen = await renderSignIn()
    await submitCredentials(screen)

    await expect.element(screen.getByRole('alert')).toBeVisible()

    await screen
      .getByLabelText(/^(Username or Email|用户名或邮箱)$/)
      .fill('admin2')

    await expect.element(screen.getByRole('alert')).not.toBeInTheDocument()
  })

  it('maps server 422 field errors back onto the form', async () => {
    loginMock.mockRejectedValueOnce(
      new ApiError({
        status: 422,
        code: 'VALIDATION_FAILED',
        message: 'Validation failed',
        fieldErrors: { password: 'Password is too long' },
      })
    )

    const screen = await renderSignIn()
    await submitCredentials(screen)

    await expect.element(screen.getByText('Password is too long')).toBeVisible()
  })

  it('shows an inline failure banner for invalid credentials', async () => {
    loginMock.mockRejectedValueOnce(
      new ApiError({
        status: 401,
        code: 'AUTH_INVALID_CREDENTIALS',
        message: 'Invalid username or password',
      })
    )

    const screen = await renderSignIn()
    await submitCredentials(screen)

    await expect
      .element(screen.getByRole('alert'))
      .toHaveTextContent(/Sign-in failed|登录失败/)
    await expect
      .element(screen.getByText('Invalid username or password'))
      .toBeVisible()
  })

  it('shows an inline failure banner when the account is disabled', async () => {
    loginMock.mockRejectedValueOnce(
      new ApiError({
        status: 403,
        code: 'AUTH_ACCOUNT_DISABLED',
        message: 'Account has been disabled',
      })
    )

    const screen = await renderSignIn()
    await submitCredentials(screen)

    await expect
      .element(screen.getByRole('alert'))
      .toHaveTextContent(/Sign-in failed|登录失败/)
    await expect
      .element(screen.getByText('Account has been disabled'))
      .toBeVisible()
  })
})

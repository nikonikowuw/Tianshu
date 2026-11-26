import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import i18n from '@/i18n/index'
import { ApiError } from '@/lib/api/error'
import { ThemeProvider } from '@/context/theme-provider'
import { SetupWizard } from './index'

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

const initMock = vi.fn()

vi.mock('@/lib/api/client', () => ({
  apiClient: {
    post: (...args: unknown[]) => initMock(...args),
  },
}))

function renderWizard() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })

  return render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <ThemeProvider>
          <SetupWizard />
        </ThemeProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
}

async function fillValidSetupForm(screen: Awaited<ReturnType<typeof render>>) {
  await screen.getByLabelText(/^(Username|用户名) */).fill('admin')
  await screen.getByLabelText(/^(Display Name|显示名称) */).fill('Super')
  await screen
    .getByLabelText(/^(Administrator Email|管理员邮箱) */)
    .fill('admin@example.com')
  await screen
    .getByLabelText(/^(Password|登录密码|密码) */)
    .fill('secret-pass-1')
  await screen
    .getByLabelText(/^(Confirm Password|确认密码) */)
    .fill('secret-pass-1')
}

async function submitSetupForm(screen: Awaited<ReturnType<typeof render>>) {
  await screen
    .getByRole('button', {
      name: /Create Administrator|创建管理员并完成初始化/,
    })
    .click()
}

describe('SetupWizard', () => {
  afterEach(() => {
    initMock.mockReset()
  })

  it('does not require a setup token', async () => {
    const screen = await renderWizard()

    await expect
      .element(screen.getByLabelText(/Setup Token|初始化令牌/))
      .not.toBeInTheDocument()
    // The static irreversibility notice must not announce itself as an error alert.
    await expect.element(screen.getByRole('alert')).not.toBeInTheDocument()
  })

  it('validates the minimum password length before submitting', async () => {
    const screen = await renderWizard()

    await submitSetupForm(screen)

    await expect
      .element(
        screen.getByText(/at least 8 characters|密码长度至少为 8 个字符/)
      )
      .toBeVisible()
    expect(initMock).not.toHaveBeenCalled()
  })

  it('re-validates the confirmation after the password changes', async () => {
    const screen = await renderWizard()

    const password = screen.getByLabelText(/^(Password|登录密码|密码) */)
    const confirm = screen.getByLabelText(/^(Confirm Password|确认密码) */)

    await password.fill('secret-pass-1')
    await confirm.fill('secret-pass-2')
    await submitSetupForm(screen)

    await expect
      .element(screen.getByText(/Passwords do not match|两次输入的密码不一致/))
      .toBeVisible()

    // Fixing the first field must clear the stale mismatch immediately.
    await password.fill('secret-pass-2')

    await expect
      .element(screen.getByText(/Passwords do not match|两次输入的密码不一致/))
      .not.toBeInTheDocument()
  })

  it('shows an inline alert on 409 conflict and clears it once edited', async () => {
    initMock.mockRejectedValueOnce(
      new ApiError({
        status: 409,
        code: 'SETUP_ALREADY_INITIALIZED',
        message: 'Instance already initialized',
      })
    )

    const screen = await renderWizard()
    await fillValidSetupForm(screen)
    await submitSetupForm(screen)

    const alert = screen.getByRole('alert')
    await expect.element(alert).toHaveTextContent(/Setup failed|初始化失败/)
    await expect
      .element(alert)
      .toHaveTextContent('Instance already initialized')

    await screen.getByLabelText(/^(Username|用户名) */).fill('admin2')
    await expect.element(alert).not.toBeInTheDocument()
  })

  it('keeps a visible alert and disables submit while rate limited', async () => {
    initMock.mockRejectedValueOnce(
      new ApiError({
        status: 429,
        code: 'RATE_LIMITED',
        message: 'Too many requests',
        retryAfter: 25,
      })
    )

    const screen = await renderWizard()
    await fillValidSetupForm(screen)
    await submitSetupForm(screen)

    const alert = screen.getByRole('alert')
    await expect
      .element(alert)
      .toHaveTextContent(/Too many attempts|尝试次数过多/)
    await expect.element(alert).toHaveTextContent(/25s|25 秒/)

    await expect
      .element(
        screen.getByRole('button', {
          name: /Create Administrator|创建管理员并完成初始化/,
        })
      )
      .toBeDisabled()
  })

  it('maps server 422 field errors back onto the form', async () => {
    initMock.mockRejectedValueOnce(
      new ApiError({
        status: 422,
        code: 'VALIDATION_FAILED',
        message: 'Validation failed',
        fieldErrors: { email: 'Email already in use' },
      })
    )

    const screen = await renderWizard()
    await fillValidSetupForm(screen)
    await submitSetupForm(screen)

    await expect.element(screen.getByText('Email already in use')).toBeVisible()
  })
})

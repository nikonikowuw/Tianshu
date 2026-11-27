import type { ComponentProps } from 'react'
import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import i18n from '@/i18n/index'
import { useAuthStore } from '@/stores/auth-store'
import { ApiError } from '@/lib/api/error'
import { ThemeProvider } from '@/context/theme-provider'
import type { LoginResponse } from './api'
import { SignIn } from './index'

const navigateMock = vi.fn()
const loginMock = vi.fn()
const toastErrorMock = vi.fn()

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    useNavigate: () => navigateMock,
  }
})

vi.mock('@/components/layout/auth-three-backdrop', () => ({
  default: () => null,
}))

vi.mock('@/lib/api/client', () => ({
  apiClient: {
    post: (...args: unknown[]) => loginMock(...args),
  },
}))

vi.mock('sonner', () => ({
  toast: {
    error: (...args: unknown[]) => toastErrorMock(...args),
  },
}))

const SUCCESS_LOGIN_RESPONSE: LoginResponse = {
  accessToken: 'access-token-123',
  expiresAt: '2026-11-27T12:00:00Z',
  user: {
    id: 'user-1',
    username: 'admin',
    email: 'admin@example.com',
    displayName: 'System Admin',
    avatarFileId: null,
    avatarUrl: null,
  },
  roles: ['super_admin'],
  permissions: ['dashboard:read', 'users:read'],
}

function resetTestState() {
  loginMock.mockReset()
  navigateMock.mockReset()
  toastErrorMock.mockReset()
  useAuthStore.getState().clearSession({ broadcast: false })
  window.localStorage.clear()
}

function renderSignIn(props: ComponentProps<typeof SignIn> = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })

  return render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <ThemeProvider>
          <SignIn {...props} />
        </ThemeProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
}

async function submitCredentials(
  screen: Awaited<ReturnType<typeof render>>,
  identifier = 'admin',
  password = 'password123'
) {
  await screen
    .getByLabelText(/^(Username or Email|用户名或邮箱)$/)
    .fill(identifier)
  await screen.getByLabelText(/^(Password|密码) */).fill(password)
  await screen.getByRole('button', { name: /^(Sign in|登录)$/ }).click()
}

describe('SignIn', () => {
  beforeEach(() => {
    resetTestState()
  })

  afterEach(() => {
    resetTestState()
  })

  it('signs in successfully, stores session strictly in memory, trims identifier only, and navigates to /', async () => {
    loginMock.mockResolvedValueOnce({ data: SUCCESS_LOGIN_RESPONSE })

    const screen = await renderSignIn()
    await submitCredentials(screen, '  admin  ', ' pass 123 ')

    await expect.poll(() => navigateMock).toHaveBeenCalledTimes(1)
    expect(loginMock).toHaveBeenCalledWith('/auth/login', {
      identifier: 'admin',
      password: ' pass 123 ',
    })
    expect(navigateMock).toHaveBeenCalledWith({ href: '/', replace: true })

    const authState = useAuthStore.getState()
    expect(authState.status).toBe('authenticated')
    expect(authState.accessToken).toBe('access-token-123')
    expect(authState.user).toEqual(SUCCESS_LOGIN_RESPONSE.user)
    expect(authState.roles).toEqual(['super_admin'])
    expect(authState.permissions).toEqual(['dashboard:read', 'users:read'])
    expect(window.localStorage.length).toBe(0)
  })

  it('preserves safe redirectTo href with query special characters after login', async () => {
    loginMock.mockResolvedValueOnce({ data: SUCCESS_LOGIN_RESPONSE })

    const screen = await renderSignIn({
      redirectTo: '/files?q=a//b&filter=/$admin',
    })
    await submitCredentials(screen)

    await expect
      .poll(() => navigateMock)
      .toHaveBeenCalledWith({
        href: '/files?q=a//b&filter=/$admin',
        replace: true,
      })
  })

  it.each(['https://evil.com', '//evil.com', '/\\evil.com'])(
    'sanitizes unsafe redirect target %s back to /',
    async (unsafeRedirect) => {
      loginMock.mockResolvedValueOnce({ data: SUCCESS_LOGIN_RESPONSE })

      const screen = await renderSignIn({ redirectTo: unsafeRedirect })
      await submitCredentials(screen)

      await expect
        .poll(() => navigateMock)
        .toHaveBeenCalledWith({ href: '/', replace: true })
    }
  )

  it('invokes onSuccess callback instead of navigating when provided', async () => {
    loginMock.mockResolvedValueOnce({ data: SUCCESS_LOGIN_RESPONSE })
    const onSuccess = vi.fn()

    const screen = await renderSignIn({
      redirectTo: '/users',
      onSuccess,
    })
    await submitCredentials(screen)

    await expect.poll(() => onSuccess).toHaveBeenCalledTimes(1)
    expect(navigateMock).not.toHaveBeenCalled()
  })

  it('validates required fields, whitespace-only identifier, and max lengths before submitting', async () => {
    const screen = await renderSignIn()

    await screen.getByRole('button', { name: /^(Sign in|登录)$/ }).click()

    await expect
      .element(
        screen.getByText(/This field is required|此字段为必填项/).first()
      )
      .toBeVisible()
    expect(loginMock).not.toHaveBeenCalled()

    await submitCredentials(screen, '   ', 'password123')
    await expect
      .element(
        screen.getByText(/This field is required|此字段为必填项/).first()
      )
      .toBeVisible()
    expect(loginMock).not.toHaveBeenCalled()

    await submitCredentials(screen, 'a'.repeat(256), 'p'.repeat(129))
    await expect
      .element(screen.getByText(/at most 255|不能超过 255/))
      .toBeVisible()
    await expect
      .element(screen.getByText(/at most 128|不能超过 128/))
      .toBeVisible()
    expect(loginMock).not.toHaveBeenCalled()
  })

  it('disables inputs and submit button with aria-busy while request is in flight', async () => {
    let resolveLogin!: (value: { data: LoginResponse }) => void
    loginMock.mockImplementationOnce(
      () =>
        new Promise<{ data: LoginResponse }>((resolve) => {
          resolveLogin = resolve
        })
    )

    const screen = await renderSignIn()
    await submitCredentials(screen)

    const identifierInput = screen.getByLabelText(
      /^(Username or Email|用户名或邮箱)$/
    )
    const passwordInput = screen.getByLabelText(/^(Password|密码) */)
    const submittingButton = screen.getByRole('button', {
      name: /^(Signing in\.\.\.|正在登录\.\.\.)$/,
    })

    await expect.element(submittingButton).toBeDisabled()
    await expect.element(submittingButton).toHaveAttribute('aria-busy', 'true')
    await expect.element(identifierInput).toBeDisabled()
    await expect.element(passwordInput).toBeDisabled()
    expect(loginMock).toHaveBeenCalledTimes(1)

    resolveLogin({ data: SUCCESS_LOGIN_RESPONSE })
    await expect.poll(() => navigateMock).toHaveBeenCalledTimes(1)
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

    const identifierInput = screen.getByLabelText(
      /^(Username or Email|用户名或邮箱)$/
    )
    const passwordInput = screen.getByLabelText(/^(Password|密码) */)
    await expect.element(identifierInput).toBeEnabled()
    await expect.element(passwordInput).toBeEnabled()

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

  it('falls back to toast via handleServerError without inline alert for 500 and network errors', async () => {
    loginMock.mockRejectedValueOnce(
      new ApiError({
        status: 500,
        code: 'INTERNAL_ERROR',
        message: 'Internal error',
        requestId: 'req-500-auth',
      })
    )

    const screen = await renderSignIn()
    await submitCredentials(screen)

    await expect
      .poll(() => toastErrorMock)
      .toHaveBeenCalledWith(expect.stringContaining('req-500-auth'))
    await expect.element(screen.getByRole('alert')).not.toBeInTheDocument()

    toastErrorMock.mockReset()
    loginMock.mockRejectedValueOnce(
      new ApiError({
        status: 0,
        code: 'NETWORK_ERROR',
        message: 'Network Error',
      })
    )

    await submitCredentials(screen)

    await expect
      .poll(() => toastErrorMock)
      .toHaveBeenCalledWith(expect.stringMatching(/Network error|网络连接异常/))
    await expect.element(screen.getByRole('alert')).not.toBeInTheDocument()
  })
})

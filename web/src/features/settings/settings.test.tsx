import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/styles/index.css'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
import i18n, { changeAppLanguage } from '@/i18n/index'
import { useAuthStore } from '@/stores/auth-store'
import { apiClient } from '@/lib/api/client'
import { ApiError } from '@/lib/api/error'
import { LayoutProvider } from '@/context/layout-provider'
import { ThemeProvider } from '@/context/theme-provider'
import { AppearanceSection } from './components/appearance-section'
import { ProfileSection } from './components/profile-section'
import { SecuritySection } from './components/security-section'
import {
  formatSessionDeviceLabel,
  parseSessionUserAgent,
} from './components/session-device'
import { SessionsSection } from './components/sessions-section'
import { profileKeys } from './queries'
import type { ActiveSessionItem, ProfileItem } from './types'

vi.mock('@/lib/api/client', () => ({
  apiClient: {
    defaults: { baseURL: '/api/v1' },
    get: vi.fn(),
    post: vi.fn(),
    patch: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
  resolveApiUrl: (url: string) => url,
}))

const mockedApi = vi.mocked(apiClient)

const MOCK_PROFILE: ProfileItem = {
  id: '01950000-0000-7000-8000-000000000001',
  username: 'alice',
  email: 'alice@example.com',
  displayName: 'Alice Chen',
  avatarFileId: null,
  avatarUrl: null,
  status: 'active',
  lastLoginAt: '2026-11-27T02:00:00Z',
  roles: [
    {
      id: '01950000-0000-7000-8000-000000000099',
      code: 'super_admin',
      name: 'Super Administrator',
      isSystem: true,
    },
  ],
  createdAt: '2026-11-26T10:00:00Z',
  updatedAt: '2026-11-27T02:00:00Z',
}

const MOCK_SESSIONS: ActiveSessionItem[] = [
  {
    id: '01950000-0000-7000-8000-000000000101',
    ipAddress: '127.0.0.1',
    userAgent:
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/130.0.0.0 Safari/537.36',
    isCurrent: true,
    lastUsedAt: '2026-11-27T02:30:00Z',
    createdAt: '2026-11-27T01:00:00Z',
    expiresAt: '2026-12-04T01:00:00Z',
  },
  {
    id: '01950000-0000-7000-8000-000000000102',
    ipAddress: '192.168.1.42',
    userAgent:
      'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1',
    isCurrent: false,
    lastUsedAt: '2026-11-27T02:15:00Z',
    createdAt: '2026-11-26T12:00:00Z',
    expiresAt: '2026-12-03T12:00:00Z',
  },
  {
    id: '01950000-0000-7000-8000-000000000103',
    ipAddress: '10.0.0.88',
    userAgent:
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:131.0) Gecko/20100101 Firefox/131.0',
    isCurrent: false,
    lastUsedAt: '2026-11-27T01:15:00Z',
    createdAt: '2026-11-25T12:00:00Z',
    expiresAt: '2026-12-02T12:00:00Z',
  },
]

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function renderWithProviders(
  ui: React.ReactNode,
  client = createQueryClient()
) {
  return render(
    <QueryClientProvider client={client}>
      <I18nextProvider i18n={i18n}>
        <ThemeProvider defaultTheme='light'>
          <LayoutProvider>{ui}</LayoutProvider>
        </ThemeProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
}

describe('parseSessionUserAgent', () => {
  it('parses desktop and mobile browser/OS pairs', () => {
    const macChrome = parseSessionUserAgent(MOCK_SESSIONS[0].userAgent)
    expect(macChrome).toEqual({
      deviceType: 'desktop',
      browser: 'Chrome',
      os: 'macOS',
    })
    expect(formatSessionDeviceLabel(macChrome, 'Unknown')).toBe(
      'Chrome · macOS'
    )

    const iosSafari = parseSessionUserAgent(MOCK_SESSIONS[1].userAgent)
    expect(iosSafari).toEqual({
      deviceType: 'mobile',
      browser: 'Safari',
      os: 'iOS',
    })

    const empty = parseSessionUserAgent('')
    expect(empty.deviceType).toBe('unknown')
    expect(formatSessionDeviceLabel(empty, 'Unknown')).toBe('Unknown')
  })
})

describe('Settings Sections', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    window.localStorage.clear()
    document.documentElement.classList.remove('dark')
    document.documentElement.classList.add('light')
    await changeAppLanguage('en-US')
    useAuthStore.getState().setSession({
      accessToken: 'test-token',
      user: {
        id: MOCK_PROFILE.id,
        username: MOCK_PROFILE.username,
        email: MOCK_PROFILE.email,
        displayName: MOCK_PROFILE.displayName,
        avatarFileId: null,
        avatarUrl: null,
      },
      roles: ['super_admin'],
      permissions: [],
    })
  })

  afterEach(async () => {
    useAuthStore.getState().clearSession({ broadcast: false })
    window.localStorage.clear()
    document.documentElement.classList.remove('dark')
    document.documentElement.classList.add('light')
    await changeAppLanguage('en-US')
  })

  it('validates client-side email, maps 422 field errors & 409 conflict, and saves profile', async () => {
    mockedApi.get.mockImplementation(async (url) => {
      if (url === '/profile') return { data: MOCK_PROFILE }
      if (url === '/files/config') {
        return {
          data: {
            maxFileSizeBytes: 10 * 1024 * 1024,
            maxAvatarSizeBytes: 2 * 1024 * 1024,
            allowedExtensions: ['.png', '.jpg'],
          },
        }
      }
      throw new Error(`Unexpected GET ${String(url)}`)
    })

    const queryClient = createQueryClient()
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const screen = await renderWithProviders(<ProfileSection />, queryClient)
    // Verify accessible FormLabel -> Input association via getByLabelText.
    const displayInput = screen.getByLabelText(
      i18n.t('settings:profile.displayName')
    )
    const emailInput = screen.getByLabelText(i18n.t('settings:profile.email'))
    await expect.element(displayInput).toHaveValue('Alice Chen')
    await expect.element(emailInput).toHaveValue('alice@example.com')

    // Client-side Zod validation blocks invalid email without issuing PATCH.
    await userEvent.clear(emailInput)
    await userEvent.type(emailInput, 'not-an-email')
    await userEvent.click(screen.getByTestId('profile-submit'))
    expect(mockedApi.patch).not.toHaveBeenCalled()

    // Server 422 fieldErrors backfill onto form fields.
    mockedApi.patch.mockRejectedValueOnce(
      new ApiError({
        status: 422,
        code: 'VALIDATION_FAILED',
        message: 'Validation failed',
        fieldErrors: {
          displayName: 'Display name rejected by policy',
        },
      })
    )
    await userEvent.clear(emailInput)
    await userEvent.type(emailInput, 'alice.new@example.com')
    await userEvent.clear(displayInput)
    await userEvent.type(displayInput, 'Alice Updated')
    await userEvent.click(screen.getByTestId('profile-submit'))
    await expect
      .element(screen.getByText('Display name rejected by policy'))
      .toBeVisible()

    // Successful save syncs auth-store, narrows query invalidation to meRoot, and resets dirty state.
    mockedApi.patch.mockResolvedValueOnce({
      data: {
        ...MOCK_PROFILE,
        displayName: 'Alice Updated',
        email: 'alice.new@example.com',
      },
    })
    await userEvent.click(screen.getByTestId('profile-submit'))
    expect(mockedApi.patch).toHaveBeenLastCalledWith('/profile', {
      displayName: 'Alice Updated',
      email: 'alice.new@example.com',
    })
    expect(useAuthStore.getState().user?.displayName).toBe('Alice Updated')
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: profileKeys.meRoot,
    })
    expect(invalidateSpy).not.toHaveBeenCalledWith({
      queryKey: profileKeys.all,
    })

    // 409 email conflict maps directly onto the email field.
    mockedApi.patch.mockRejectedValueOnce(
      new ApiError({
        status: 409,
        code: 'USERS_EMAIL_CONFLICT',
        message: 'Email address is already in use',
      })
    )
    await userEvent.clear(emailInput)
    await userEvent.type(emailInput, 'taken@example.com')
    await userEvent.click(screen.getByTestId('profile-submit'))
    await expect
      .element(screen.getByText('Email address is already in use'))
      .toBeVisible()
  })

  it('handles avatar validation, 422 avatarFileId error, upload/removal, and preserves unsaved edits', async () => {
    let currentProfile: ProfileItem = { ...MOCK_PROFILE }
    mockedApi.get.mockImplementation(async (url) => {
      if (url === '/profile') return { data: currentProfile }
      if (url === '/files/config') {
        return {
          data: {
            maxFileSizeBytes: 10 * 1024 * 1024,
            maxAvatarSizeBytes: 1024,
            allowedExtensions: ['.png'],
          },
        }
      }
      throw new Error(`Unexpected GET ${String(url)}`)
    })

    const queryClient = createQueryClient()
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const screen = await renderWithProviders(<ProfileSection />, queryClient)
    const displayInput = screen.getByLabelText(
      i18n.t('settings:profile.displayName')
    )
    await expect.element(displayInput).toHaveValue('Alice Chen')

    // Make an unsaved edit to displayName; avatar operations must NOT wipe it.
    await userEvent.clear(displayInput)
    await userEvent.type(displayInput, 'Unsaved Draft Name')

    const inputEl = screen
      .getByTestId('profile-avatar-input')
      .element() as HTMLInputElement

    // Oversized image blocked client-side.
    const bigFile = new File([new Uint8Array(2048)], 'big.png', {
      type: 'image/png',
    })
    await userEvent.upload(inputEl, bigFile)
    await expect.element(screen.getByRole('alert')).toBeVisible()
    expect(mockedApi.post).not.toHaveBeenCalled()

    // 422 avatarFileId fieldError surfaces the specific avatar error message.
    const avatarFileId = '01950000-0000-7000-8000-000000000555'
    mockedApi.post.mockResolvedValueOnce({
      data: {
        id: avatarFileId,
        originalName: 'ok.png',
        mimeType: 'image/png',
        sizeBytes: 64,
      },
    })
    mockedApi.patch.mockRejectedValueOnce(
      new ApiError({
        status: 422,
        code: 'VALIDATION_FAILED',
        message: 'Generic validation error',
        fieldErrors: {
          avatarFileId: 'Selected avatar is invalid or inaccessible',
        },
      })
    )
    const smallFile = new File([new Uint8Array(64)], 'ok.png', {
      type: 'image/png',
    })
    await userEvent.upload(inputEl, smallFile)
    await expect
      .element(screen.getByText('Selected avatar is invalid or inaccessible'))
      .toBeVisible()

    // Valid avatar upload & bind succeeds, invalidates files cache, and preserves unsaved displayName draft.
    mockedApi.post.mockResolvedValueOnce({
      data: {
        id: avatarFileId,
        originalName: 'ok.png',
        mimeType: 'image/png',
        sizeBytes: 64,
      },
    })
    mockedApi.patch.mockImplementationOnce(async () => {
      currentProfile = {
        ...MOCK_PROFILE,
        avatarFileId,
        avatarUrl: '/api/v1/files/signed/ok-token',
      }
      return { data: currentProfile }
    })
    await userEvent.upload(inputEl, smallFile)
    expect(mockedApi.patch).toHaveBeenLastCalledWith('/profile', {
      avatarFileId,
    })
    expect(useAuthStore.getState().user?.avatarFileId).toBe(avatarFileId)
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['files'] })
    await expect.element(displayInput).toHaveValue('Unsaved Draft Name')

    // Remove avatar sends explicit avatarFileId: null.
    mockedApi.patch.mockImplementationOnce(async () => {
      currentProfile = { ...MOCK_PROFILE, avatarFileId: null, avatarUrl: null }
      return { data: currentProfile }
    })
    const removeBtn = screen.getByTestId('profile-avatar-remove')
    await expect.element(removeBtn).toBeVisible()
    await userEvent.click(removeBtn)
    expect(mockedApi.patch).toHaveBeenLastCalledWith('/profile', {
      avatarFileId: null,
    })
    expect(useAuthStore.getState().user?.avatarFileId).toBeNull()
  })

  it('renders profile load error state with localized 5xx requestId and retries on click', async () => {
    let failFirst = true
    mockedApi.get.mockImplementation(async (url) => {
      if (url === '/files/config') {
        return {
          data: {
            maxFileSizeBytes: 10 * 1024 * 1024,
            maxAvatarSizeBytes: 2 * 1024 * 1024,
            allowedExtensions: ['.png'],
          },
        }
      }
      if (url === '/profile') {
        if (failFirst) {
          failFirst = false
          throw new ApiError({
            status: 500,
            code: 'INTERNAL_ERROR',
            message: 'Raw internal error detail',
            requestId: 'req-profile-500',
          })
        }
        return { data: MOCK_PROFILE }
      }
      throw new Error(`Unexpected GET ${String(url)}`)
    })

    const screen = await renderWithProviders(<ProfileSection />)
    await expect
      .element(
        screen.getByText(
          i18n.t('errors:serverError', { requestId: 'req-profile-500' })
        )
      )
      .toBeVisible()

    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('common:retry') })
    )
    await expect
      .element(screen.getByLabelText(i18n.t('settings:profile.displayName')))
      .toHaveValue('Alice Chen')
  })

  it('validates password form, handles 400/422, succeeds with reset & invalidation, and locks on 429', async () => {
    const queryClient = createQueryClient()
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const screen = await renderWithProviders(<SecuritySection />, queryClient)

    // Verify FormLabel -> PasswordInput accessible association via getByLabelText.
    const currentInput = screen.getByLabelText(
      i18n.t('settings:security.currentPassword')
    )
    const newInput = screen.getByLabelText(
      i18n.t('settings:security.newPassword'),
      { exact: true }
    )
    const confirmInput = screen.getByLabelText(
      i18n.t('settings:security.confirmPassword')
    )
    const submitBtn = screen.getByTestId('security-submit')

    // Client-side validation: mismatched confirmation shows error, then clears when newPassword updates to match.
    await userEvent.type(currentInput, 'OldPass123!')
    await userEvent.type(newInput, 'Short1!')
    await userEvent.type(confirmInput, 'MismatchPass123!')
    await userEvent.click(submitBtn)
    expect(mockedApi.put).not.toHaveBeenCalled()

    await userEvent.clear(newInput)
    await userEvent.type(newInput, 'MismatchPass123!')
    await expect
      .element(screen.getByText(i18n.t('validation:passwordMismatch')))
      .not.toBeInTheDocument()

    // 400 wrong current password maps onto currentPassword field.
    mockedApi.put.mockRejectedValueOnce(
      new ApiError({
        status: 400,
        code: 'PROFILE_INVALID_CURRENT_PASSWORD',
        message: 'Current password is incorrect',
      })
    )
    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText('Current password is incorrect'))
      .toBeVisible()

    // 422 fieldErrors maps onto newPassword (e.g. PROFILE_PASSWORD_UNCHANGED).
    mockedApi.put.mockRejectedValueOnce(
      new ApiError({
        status: 422,
        code: 'VALIDATION_FAILED',
        message: 'Validation failed',
        fieldErrors: {
          newPassword: 'New password must differ from current password',
        },
      })
    )
    await userEvent.click(submitBtn)
    await expect
      .element(
        screen.getByText('New password must differ from current password')
      )
      .toBeVisible()

    // Network failure (status 0) maps to localized errors:network banner instead of raw Axios text.
    mockedApi.put.mockRejectedValueOnce(
      new ApiError({
        status: 0,
        code: 'NETWORK_ERROR',
        message: 'Network Error',
      })
    )
    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText(i18n.t('errors:network')))
      .toBeVisible()

    // 200 OK succeeds, sends expected payload, resets form, and invalidates sessions query.
    mockedApi.put.mockResolvedValueOnce({
      data: { revokedSessions: 2 },
    })
    await userEvent.clear(newInput)
    await userEvent.type(newInput, 'NewStrongPass123!')
    await userEvent.clear(confirmInput)
    await userEvent.type(confirmInput, 'NewStrongPass123!')
    await userEvent.click(submitBtn)

    expect(mockedApi.put).toHaveBeenLastCalledWith('/profile/password', {
      currentPassword: 'OldPass123!',
      newPassword: 'NewStrongPass123!',
      revokeOtherSessions: true,
    })
    await expect.element(currentInput).toHaveValue('')
    await expect.element(newInput).toHaveValue('')
    await expect.element(confirmInput).toHaveValue('')
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: profileKeys.sessions,
    })

    // 429 locks submit button, displays Retry-After countdown in role="alert", and recovers when cooldown ends.
    mockedApi.put.mockRejectedValueOnce(
      new ApiError({
        status: 429,
        code: 'RATE_LIMITED',
        message: 'Too many requests',
        retryAfter: 1,
      })
    )
    await userEvent.type(currentInput, 'NewStrongPass123!')
    await userEvent.type(newInput, 'AnotherStrongPass456!')
    await userEvent.type(confirmInput, 'AnotherStrongPass456!')
    await userEvent.click(submitBtn)
    const rateAlert = screen.getByRole('alert')
    await expect.element(rateAlert).toBeVisible()
    await expect
      .element(rateAlert)
      .toHaveTextContent(i18n.t('errors:rateLimitWait', { seconds: 1 }))
    await expect.element(submitBtn).toBeDisabled()
    await expect.element(submitBtn).toBeEnabled()
  })

  it('renders active sessions, handles single revoke, 404 stale dialog close, and bulk revoke-others', async () => {
    let sessions = [...MOCK_SESSIONS]
    mockedApi.get.mockImplementation(async (url) => {
      if (url === '/profile/sessions') return { data: { items: sessions } }
      throw new Error(`Unexpected GET ${String(url)}`)
    })
    mockedApi.delete.mockImplementation(async (url) => {
      if (url === `/profile/sessions/${MOCK_SESSIONS[1].id}`) {
        sessions = [MOCK_SESSIONS[0], MOCK_SESSIONS[2]]
        return { data: undefined }
      }
      if (url === '/profile/sessions') {
        sessions = [MOCK_SESSIONS[0]]
        return { data: { revokedSessions: 1 } }
      }
      throw new Error(`Unexpected DELETE ${String(url)}`)
    })

    const screen = await renderWithProviders(<SessionsSection />)
    await expect
      .element(screen.getByTestId('session-current-badge'))
      .toBeVisible()
    await expect.element(screen.getByText('Chrome · macOS')).toBeVisible()
    await expect.element(screen.getByText('Safari · iOS')).toBeVisible()
    await expect.element(screen.getByText('Firefox · Windows')).toBeVisible()

    // Single session revoke.
    await userEvent.click(screen.getByTestId('session-revoke-btn').first())
    await userEvent.click(screen.getByTestId('session-revoke-confirm'))
    expect(mockedApi.delete).toHaveBeenCalledWith(
      `/profile/sessions/${MOCK_SESSIONS[1].id}`
    )
    await expect
      .element(screen.getByText('Safari · iOS'))
      .not.toBeInTheDocument()

    // 404 stale session revoke closes dialog and refreshes list.
    mockedApi.delete.mockRejectedValueOnce(
      new ApiError({
        status: 404,
        code: 'PROFILE_SESSION_NOT_FOUND',
        message: 'Session no longer exists',
      })
    )
    await userEvent.click(screen.getByTestId('session-revoke-btn').first())
    await userEvent.click(screen.getByTestId('session-revoke-confirm'))
    await expect
      .element(screen.getByTestId('session-revoke-confirm'))
      .not.toBeInTheDocument()

    // Bulk revoke all other devices.
    await userEvent.click(screen.getByTestId('sessions-revoke-all-btn'))
    await userEvent.click(screen.getByTestId('sessions-revoke-all-confirm'))
    expect(mockedApi.delete).toHaveBeenLastCalledWith('/profile/sessions')
    await expect
      .element(screen.getByText('Firefox · Windows'))
      .not.toBeInTheDocument()
  })

  it('renders sessions error-retry with localized 5xx requestId and empty states', async () => {
    let failFirst = true
    mockedApi.get.mockImplementation(async (url) => {
      if (url === '/profile/sessions') {
        if (failFirst) {
          failFirst = false
          throw new ApiError({
            status: 500,
            code: 'INTERNAL_ERROR',
            message: 'Raw sessions error detail',
            requestId: 'req-sessions-500',
          })
        }
        return { data: { items: [] } }
      }
      throw new Error(`Unexpected GET ${String(url)}`)
    })

    const screen = await renderWithProviders(<SessionsSection />)
    await expect
      .element(
        screen.getByText(
          i18n.t('errors:serverError', { requestId: 'req-sessions-500' })
        )
      )
      .toBeVisible()

    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('common:retry') })
    )
    await expect
      .element(
        screen.getByText(i18n.t('settings:sessions.emptyTitle'), {
          exact: true,
        })
      )
      .toBeVisible()
  })

  it('switches theme with distinct light/dark previews in dark mode and switches language', async () => {
    const queryClient = createQueryClient()
    const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries')

    const screen = await renderWithProviders(<AppearanceSection />, queryClient)

    await userEvent.click(screen.getByTestId('appearance-theme-dark'))
    expect(document.documentElement.classList.contains('dark')).toBe(true)

    // Verify .light preview stays light even while <html class="dark"> is active.
    const lightBg = getComputedStyle(
      screen.getByTestId('appearance-preview-light').element()
    ).backgroundColor
    const darkBg = getComputedStyle(
      screen.getByTestId('appearance-preview-dark').element()
    ).backgroundColor
    expect(lightBg).not.toBe(darkBg)

    await userEvent.click(screen.getByTestId('appearance-lang-zh-CN'))
    expect(document.documentElement.lang).toBe('zh-CN')
    expect(invalidateSpy).toHaveBeenCalled()
  })
})

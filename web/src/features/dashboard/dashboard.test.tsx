import { type ReactNode } from 'react'
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
import { SearchProvider } from '@/context/search-provider'
import { ThemeProvider } from '@/context/theme-provider'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import {
  formatCount,
  formatShortUtcDate,
  formatStorageBytes,
} from './components/format'
import { Dashboard } from './index'
import type { DashboardSummary } from './types'

vi.mock('@tanstack/react-router', () => ({
  Link: ({
    to,
    children,
    ...props
  }: {
    to: string
    children: ReactNode
    className?: string
  }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
}))

vi.mock('@/lib/api/client', () => ({
  apiClient: {
    get: vi.fn(),
  },
}))

const mockedApi = vi.mocked(apiClient)

const MOCK_SUMMARY_7D: DashboardSummary = {
  generatedAt: '2026-11-27T06:30:00Z',
  rangeDays: 7,
  users: {
    total: 12,
    active: 10,
    disabled: 2,
    newLast7Days: 3,
  },
  roles: {
    total: 4,
    system: 1,
    custom: 3,
  },
  files: {
    total: 18,
    totalSizeBytes: 2 * 1024 * 1024,
    images: 11,
    documents: 7,
  },
  sessions: {
    active: 5,
  },
  audit: {
    totalInRange: 11,
    loginsInRange: 5,
    failuresInRange: 1,
  },
  activityTrend: [
    { date: '2026-11-21', logins: 0, operations: 0, failures: 0 },
    { date: '2026-11-22', logins: 2, operations: 3, failures: 0 },
    { date: '2026-11-23', logins: 0, operations: 0, failures: 0 },
    { date: '2026-11-24', logins: 1, operations: 1, failures: 1 },
    { date: '2026-11-25', logins: 0, operations: 0, failures: 0 },
    { date: '2026-11-26', logins: 1, operations: 0, failures: 0 },
    { date: '2026-11-27', logins: 1, operations: 1, failures: 0 },
  ],
  recentActivity: [
    {
      id: '01950000-0000-7000-8000-000000000001',
      actorUsername: 'alice',
      action: 'auth.login',
      resource: 'session',
      resourceId: 'sess-1',
      status: 'success',
      ipAddress: '127.0.0.1',
      createdAt: '2026-11-27T06:28:00Z',
    },
    {
      id: '01950000-0000-7000-8000-000000000002',
      actorUsername: '',
      action: 'auth.login',
      resource: 'session',
      resourceId: '',
      status: 'failure',
      ipAddress: '198.51.100.7',
      createdAt: '2026-11-27T06:15:00Z',
    },
  ],
}

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  })
}

function renderDashboard(client = createQueryClient()) {
  return render(
    <QueryClientProvider client={client}>
      <I18nextProvider i18n={i18n}>
        <ThemeProvider defaultTheme='light'>
          <SidebarProvider>
            <SearchProvider>
              <SidebarInset className='@container/content'>
                <Dashboard />
              </SidebarInset>
            </SearchProvider>
          </SidebarProvider>
        </ThemeProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
}

function setSessionPermissions(permissions: string[]) {
  useAuthStore.getState().setSession({
    accessToken: 'test-token',
    user: {
      id: '01950000-0000-7000-8000-000000000099',
      username: 'alice',
      email: 'alice@example.com',
      displayName: 'Alice',
      avatarFileId: null,
      avatarUrl: null,
    },
    roles: ['admin'],
    permissions,
  })
}

describe('dashboard formatters', () => {
  it('formats counts, storage bytes, and UTC dates deterministically', () => {
    expect(formatCount(1234, 'en-US')).toBe('1,234')
    expect(formatStorageBytes(0, 'en-US')).toBe('0 B')
    expect(formatStorageBytes(1536, 'en-US')).toBe('1.5 KB')
    expect(formatStorageBytes(2 * 1024 * 1024, 'en-US')).toBe('2 MB')
    expect(formatShortUtcDate('2026-11-27', 'en-US')).toBe('Nov 27')
  })
})

describe('Dashboard page', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    await changeAppLanguage('en-US')
    setSessionPermissions([
      'dashboard:read',
      'users:read',
      'roles:read',
      'files:read',
      'audit:read',
    ])
  })

  afterEach(async () => {
    useAuthStore.getState().clearSession({ broadcast: false })
    await changeAppLanguage('en-US')
  })

  it('renders KPI cards, trend chart, recent activity, and switches time ranges', async () => {
    mockedApi.get.mockImplementation(async (url, config) => {
      if (url === '/dashboard/summary') {
        const days =
          (config?.params as { days?: number } | undefined)?.days ?? 7
        return {
          data: {
            ...MOCK_SUMMARY_7D,
            rangeDays: days,
          },
        }
      }
      throw new Error(`Unexpected GET ${String(url)}`)
    })

    const screen = await renderDashboard()

    await expect
      .element(screen.getByTestId('dashboard-kpi-users-value'))
      .toHaveTextContent('12')
    await expect
      .element(screen.getByTestId('dashboard-kpi-roles-value'))
      .toHaveTextContent('4')
    await expect
      .element(screen.getByTestId('dashboard-kpi-files-value'))
      .toHaveTextContent('18')
    await expect
      .element(screen.getByTestId('dashboard-kpi-files-size'))
      .toHaveTextContent('2 MB')
    await expect
      .element(screen.getByTestId('dashboard-kpi-sessions-value'))
      .toHaveTextContent('5')
    await expect
      .element(screen.getByTestId('dashboard-trend-chart'))
      .toBeVisible()
    await expect
      .element(screen.getByTestId('dashboard-recent-item').first())
      .toBeVisible()
    await expect
      .element(screen.getByTestId('dashboard-view-all-audit'))
      .toBeVisible()

    // Switch to 14-day window and verify API request + aria-pressed state.
    const range14Btn = screen.getByTestId('dashboard-range-14')
    await userEvent.click(range14Btn)
    await expect.element(range14Btn).toHaveAttribute('aria-pressed', 'true')
    expect(mockedApi.get).toHaveBeenCalledWith(
      '/dashboard/summary',
      expect.objectContaining({
        params: { days: 14 },
      })
    )

    // Toggle integrated trend series filter tabs.
    const allSeriesTab = screen.getByTestId('dashboard-series-all')
    const loginsSeriesTab = screen.getByTestId('dashboard-series-logins')
    const operationsSeriesTab = screen.getByTestId(
      'dashboard-series-operations'
    )
    const failuresSeriesTab = screen.getByTestId('dashboard-series-failures')
    await expect.element(allSeriesTab).toHaveAttribute('aria-pressed', 'true')
    await expect.element(allSeriesTab).toHaveTextContent('11')
    await expect.element(loginsSeriesTab).toHaveTextContent('5')
    await expect.element(operationsSeriesTab).toHaveTextContent('5')
    await expect.element(failuresSeriesTab).toHaveTextContent('1')

    await userEvent.click(loginsSeriesTab)
    await expect
      .element(loginsSeriesTab)
      .toHaveAttribute('aria-pressed', 'true')
    await expect.element(allSeriesTab).toHaveAttribute('aria-pressed', 'false')
    await expect
      .element(screen.getByTestId('dashboard-trend-chart'))
      .toBeVisible()
  })

  it('hides module quick links when user only holds dashboard:read', async () => {
    setSessionPermissions(['dashboard:read'])
    mockedApi.get.mockResolvedValueOnce({ data: MOCK_SUMMARY_7D })

    const screen = await renderDashboard()
    await expect
      .element(screen.getByTestId('dashboard-kpi-users-value'))
      .toHaveTextContent('12')

    const manageLinks = screen.getByRole('link', {
      name: i18n.t('dashboard:kpi.manage'),
    })
    await expect.element(manageLinks).not.toBeInTheDocument()
    await expect
      .element(screen.getByTestId('dashboard-view-all-audit'))
      .not.toBeInTheDocument()
  })

  it('renders empty states when trend and recent activity are empty', async () => {
    mockedApi.get.mockResolvedValueOnce({
      data: {
        ...MOCK_SUMMARY_7D,
        audit: { totalInRange: 0, loginsInRange: 0, failuresInRange: 0 },
        activityTrend: MOCK_SUMMARY_7D.activityTrend.map((p) => ({
          ...p,
          logins: 0,
          operations: 0,
          failures: 0,
        })),
        recentActivity: [],
      },
    })

    const screen = await renderDashboard()
    await expect
      .element(screen.getByTestId('dashboard-trend-empty'))
      .toBeVisible()
    await expect
      .element(screen.getByTestId('dashboard-recent-empty'))
      .toBeVisible()
  })

  it('renders localized 5xx requestId error alert and recovers on retry', async () => {
    let failFirst = true
    mockedApi.get.mockImplementation(async () => {
      if (failFirst) {
        failFirst = false
        throw new ApiError({
          status: 500,
          code: 'INTERNAL_ERROR',
          message: 'Raw backend detail',
          requestId: 'req-dash-500',
        })
      }
      return { data: MOCK_SUMMARY_7D }
    })

    const screen = await renderDashboard()
    await expect
      .element(
        screen.getByText(
          i18n.t('errors:serverError', { requestId: 'req-dash-500' })
        )
      )
      .toBeVisible()

    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('common:retry') })
    )
    await expect
      .element(screen.getByTestId('dashboard-kpi-users-value'))
      .toHaveTextContent('12')
  })
})

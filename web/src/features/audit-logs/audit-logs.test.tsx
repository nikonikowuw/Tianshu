import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import i18n from '@/i18n/index'
import { ApiError } from '@/lib/api/error'
import { buildAuditLogsQueryParams } from './api'
import { AuditLogDetailSheet } from './components/audit-log-detail-sheet'
import { AuditLogsTable } from './components/audit-logs-table'
import { auditLogsSearchSchema, type AuditLogsSearch } from './schema'
import type { AuditLogItem, AuditLogsPage } from './types'

const getMock = vi.fn()

vi.mock('@/lib/api/client', () => ({
  apiClient: {
    get: (...args: unknown[]) => getMock(...args),
  },
}))

const sampleEntries: AuditLogItem[] = [
  {
    id: '01930000-0000-7000-8000-000000000001',
    actorId: '01930000-0000-7000-8000-000000000099',
    actorUsername: 'alice_admin',
    action: 'auth.login',
    resource: 'session',
    resourceId: 'sess-101',
    status: 'success',
    ipAddress: '10.0.0.12',
    userAgent: 'Mozilla/5.0',
    requestId: 'req_audit_alpha_01',
    metadata: { method: 'password', region: 'ap-east' },
    createdAt: '2026-11-26T09:30:00Z',
  },
  {
    id: '01930000-0000-7000-8000-000000000002',
    actorId: null,
    actorUsername: 'bad_actor',
    action: 'auth.refresh_replay',
    resource: 'session',
    resourceId: 'sess-202',
    status: 'failure',
    ipAddress: '198.51.100.77',
    userAgent: 'curl/8.4.0',
    requestId: 'req_audit_beta_02',
    metadata: { reason: 'refresh_token_reused' },
    createdAt: '2026-11-26T09:35:00Z',
  },
]

function AuditLogsHarness({
  initialSearch,
}: {
  initialSearch?: Partial<AuditLogsSearch>
}) {
  const [search, setSearch] = useState<AuditLogsSearch>(() =>
    auditLogsSearchSchema.parse(initialSearch ?? {})
  )
  const [selected, setSelected] = useState<AuditLogItem | null>(null)

  const navigate = (opts: {
    search:
      | true
      | Record<string, unknown>
      | ((prev: Record<string, unknown>) => Record<string, unknown>)
  }) => {
    if (typeof opts.search === 'function') {
      setSearch((prev) =>
        auditLogsSearchSchema.parse(
          (
            opts.search as (
              p: Record<string, unknown>
            ) => Record<string, unknown>
          )(prev as unknown as Record<string, unknown>)
        )
      )
    } else if (opts.search !== true) {
      setSearch(auditLogsSearchSchema.parse(opts.search))
    }
  }

  return (
    <>
      <AuditLogsTable
        search={search}
        navigate={navigate}
        onSelectEntry={setSelected}
      />
      <AuditLogDetailSheet
        entry={selected}
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
      />
    </>
  )
}

function renderAuditLogs(initialSearch?: Partial<AuditLogsSearch>) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <AuditLogsHarness initialSearch={initialSearch} />
      </I18nextProvider>
    </QueryClientProvider>
  )
}

describe('audit-logs feature', () => {
  afterEach(() => {
    getMock.mockReset()
  })

  it('normalizes invalid URL search params and builds repeated query params', () => {
    const parsed = auditLogsSearchSchema.parse({
      page: '-3',
      pageSize: '999',
      status: ['success', 'invalid'],
      from: 'bad-date',
    })
    expect(parsed).toEqual({
      page: 1,
      pageSize: 20,
      status: ['success'],
      from: undefined,
    })

    const sp = buildAuditLogsQueryParams({
      page: 2,
      pageSize: 50,
      q: 'alice',
      status: ['success', 'failure'],
      action: ['auth.login'],
      sort: 'created_at',
      order: 'desc',
    })
    expect(sp.get('page')).toBe('2')
    expect(sp.get('pageSize')).toBe('50')
    expect(sp.getAll('status')).toEqual(['success', 'failure'])
    expect(sp.getAll('action')).toEqual(['auth.login'])
  })

  it('renders rows, opens detail sheet on row click, and stops propagation on row menu', async () => {
    const pageData: AuditLogsPage = {
      items: sampleEntries,
      total: 2,
      page: 1,
      pageSize: 20,
    }
    getMock.mockResolvedValue({ data: pageData })

    const screen = await renderAuditLogs()

    await expect.element(screen.getByText('alice_admin')).toBeVisible()
    await expect.element(screen.getByText('bad_actor')).toBeVisible()

    // Clicking the row actions trigger opens menu without opening the detail Sheet.
    const menuButtons = screen.getByRole('button', {
      name: /打开操作菜单|Open actions menu/,
    })
    await menuButtons.first().click()

    const viewDetailsItem = screen.getByRole('menuitem', {
      name: /查看详情|View details/,
    })
    await expect.element(viewDetailsItem).toBeVisible()
    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()

    // Selecting "View details" from the menu opens the Sheet with formatted metadata.
    await viewDetailsItem.click()

    const sheet = screen.getByRole('dialog')
    await expect.element(sheet).toBeVisible()
    await expect.element(sheet).toHaveTextContent('req_audit_alpha_01')
    await expect.element(sheet).toHaveTextContent('"region": "ap-east"')
  })

  it('opens detail sheet when clicking a table row directly', async () => {
    const pageData: AuditLogsPage = {
      items: sampleEntries,
      total: 2,
      page: 1,
      pageSize: 20,
    }
    getMock.mockResolvedValue({ data: pageData })

    const screen = await renderAuditLogs()

    await expect.element(screen.getByText('bad_actor')).toBeVisible()
    await screen.getByText('bad_actor').click()

    const sheet = screen.getByRole('dialog')
    await expect.element(sheet).toBeVisible()
    await expect.element(sheet).toHaveTextContent('refresh_token_reused')
    await expect.element(sheet).toHaveTextContent('req_audit_beta_02')
  })

  it('distinguishes empty filtered state and resets filters on Clear filters', async () => {
    getMock.mockImplementation(
      (_url: string, opts?: { params?: URLSearchParams }) => {
        const q = opts?.params?.get('q')
        if (q) {
          return Promise.resolve({
            data: { items: [], total: 0, page: 1, pageSize: 20 },
          })
        }
        return Promise.resolve({
          data: { items: sampleEntries, total: 2, page: 1, pageSize: 20 },
        })
      }
    )

    const screen = await renderAuditLogs({ q: 'nonexistent-query' })

    await expect
      .element(
        screen.getByText(/没有符合筛选条件的记录|No matching audit logs/)
      )
      .toBeVisible()

    await screen.getByRole('button', { name: /清除筛选|Clear filters/ }).click()

    await expect.element(screen.getByText('alice_admin')).toBeVisible()
  })

  it('renders inline alert on failure and retries on button click', async () => {
    getMock
      .mockRejectedValueOnce(
        new ApiError({
          status: 500,
          code: 'INTERNAL_ERROR',
          message: 'Server failed',
        })
      )
      .mockResolvedValueOnce({
        data: { items: sampleEntries, total: 2, page: 1, pageSize: 20 },
      })

    const screen = await renderAuditLogs()

    const alert = screen.getByRole('alert')
    await expect.element(alert).toBeVisible()
    await expect.element(alert).toHaveTextContent('Server failed')

    await alert.getByRole('button', { name: /重试|Retry/ }).click()

    await expect.element(screen.getByText('alice_admin')).toBeVisible()
    await expect.element(alert).not.toBeInTheDocument()
  })
})

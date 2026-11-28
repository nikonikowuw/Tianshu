import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/styles/index.css'
import { toast } from 'sonner'
import {
  afterAll,
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
import i18n from '@/i18n/index'
import { ApiError } from '@/lib/api/error'
import { buildAuditLogsQueryParams } from './api'
import { AUDIT_ACTION_OPTIONS, AUDIT_RESOURCE_OPTIONS } from './catalog'
import { AuditLogDetailSheet } from './components/audit-log-detail-sheet'
import { AuditLogsTable } from './components/audit-logs-table'
import { formatDateOnly, normalizeDateRange, toBoundaryRFC3339 } from './date'
import { auditLogsSearchSchema, type AuditLogsSearch } from './schema'
import type { AuditLogItem, AuditLogsPage } from './types'

const getMock = vi.fn()
const initialLanguage = i18n.language

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
  {
    id: '01930000-0000-7000-8000-000000000003',
    actorId: '01930000-0000-7000-8000-000000000098',
    actorUsername: 'bob_ops',
    action: 'user.delete',
    resource: 'menu',
    resourceId: 'menu-9',
    status: 'success',
    ipAddress: '10.0.0.13',
    userAgent: 'Mozilla/5.0',
    requestId: 'req_audit_gamma_03',
    metadata: {},
    createdAt: '2026-11-26T09:40:00Z',
  },
]

function makePage(items: AuditLogItem[] = sampleEntries): AuditLogsPage {
  return { items, total: items.length, page: 1, pageSize: 20 }
}

function lastRequestParams(): URLSearchParams {
  const calls = getMock.mock.calls
  const call = calls[calls.length - 1]
  return (call?.[1] as { params: URLSearchParams }).params
}

function searchState(screen: Awaited<ReturnType<typeof renderAuditLogs>>) {
  return JSON.parse(
    screen.getByTestId('audit-search-state').element().textContent ?? '{}'
  ) as AuditLogsSearch
}

function AuditLogsHarness({
  initialSearch,
}: {
  initialSearch?: Partial<AuditLogsSearch>
}) {
  const [search, setSearch] = useState<AuditLogsSearch>(() =>
    auditLogsSearchSchema.parse(initialSearch ?? {})
  )
  const [selected, setSelected] = useState<AuditLogItem | null>(null)
  const [lastReplace, setLastReplace] = useState<boolean | undefined>(undefined)

  const navigate = (opts: {
    replace?: boolean
    search:
      | true
      | Record<string, unknown>
      | ((prev: Record<string, unknown>) => Record<string, unknown>)
  }) => {
    setLastReplace(opts.replace)
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
      <output
        data-testid='audit-search-state'
        data-last-replace={String(lastReplace)}
        className='sr-only'
      >
        {JSON.stringify(search)}
      </output>
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
  beforeEach(async () => {
    await i18n.changeLanguage('zh-CN')
  })

  afterEach(() => {
    getMock.mockReset()
  })

  afterAll(async () => {
    await i18n.changeLanguage(initialLanguage)
  })

  it('normalizes invalid URL search params and builds repeated query params', () => {
    const validActorId = '01930000-0000-7000-8000-000000000099'
    const parsed = auditLogsSearchSchema.parse({
      page: '-3',
      pageSize: '999',
      q: 'x'.repeat(101),
      actor: 'a'.repeat(101),
      actorId: '00000000-0000-0000-0000-000000000000',
      status: ['success', 'success', 'invalid'],
      action: [
        'auth.login',
        'auth.login',
        'x'.repeat(65),
        ...Array.from({ length: 25 }, (_, idx) => `custom.action_${idx}`),
      ],
      from: '2026-02-30',
    })
    expect(parsed).toMatchObject({
      page: 1,
      pageSize: 20,
      status: ['success'],
      from: undefined,
      q: undefined,
      actor: undefined,
      actorId: undefined,
    })
    expect(parsed.action).toHaveLength(20)
    expect(parsed.action?.[0]).toBe('auth.login')

    const numericAndActorId = auditLogsSearchSchema.parse({
      q: 12345,
      actor: 67890,
      actorId: validActorId,
    })
    expect(numericAndActorId).toMatchObject({
      q: '12345',
      actor: '67890',
      actorId: validActorId,
    })

    const sp = buildAuditLogsQueryParams({
      page: 2,
      pageSize: 50,
      q: 'alice',
      actor: 'alice_admin',
      actorId: validActorId,
      status: ['success', 'failure'],
      action: ['auth.login'],
      from: '2026-03-01',
      to: '2026-03-02',
      sort: 'created_at',
      order: 'desc',
    })
    expect(sp.get('page')).toBe('2')
    expect(sp.get('pageSize')).toBe('50')
    expect(sp.get('actor')).toBe('alice_admin')
    expect(sp.get('actorId')).toBe(validActorId)
    expect(sp.getAll('status')).toEqual(['success', 'failure'])
    expect(sp.getAll('action')).toEqual(['auth.login'])
    expect(sp.get('from')).toBe(toBoundaryRFC3339('2026-03-01', false))
    expect(sp.get('to')).toBe(toBoundaryRFC3339('2026-03-02', true))
    expect(toBoundaryRFC3339('2026-02-30', false)).toBeNull()
    expect(normalizeDateRange('2026-03-10', '2026-03-01')).toEqual({
      from: '2026-03-01',
      to: '2026-03-10',
      reversed: true,
    })
  })

  it('renders rows, opens detail sheet on row click, and stops propagation on row menu', async () => {
    getMock.mockResolvedValue({ data: makePage() })

    const screen = await renderAuditLogs()

    await expect.element(screen.getByText('alice_admin')).toBeVisible()
    await expect.element(screen.getByText('bad_actor')).toBeVisible()
    await expect.element(screen.getByText('bob_ops')).toBeVisible()

    const menuButtons = screen.getByRole('button', {
      name: /打开操作菜单|Open actions menu/,
    })
    await menuButtons.first().click()

    const viewDetailsItem = screen.getByRole('menuitem', {
      name: /查看详情|View details/,
    })
    await expect.element(viewDetailsItem).toBeVisible()
    await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()

    await viewDetailsItem.click()

    const sheet = screen.getByRole('dialog')
    await expect.element(sheet).toBeVisible()
    await expect.element(sheet).toHaveTextContent('req_audit_alpha_01')
    await expect.element(sheet).toHaveTextContent('"region": "ap-east"')
  })

  it('opens detail sheet when clicking a table row directly', async () => {
    getMock.mockResolvedValue({ data: makePage() })

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
        return Promise.resolve({ data: makePage() })
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

  it('renders a localized 5xx alert with request id, hides the empty state, and retries', async () => {
    getMock
      .mockRejectedValueOnce(
        new ApiError({
          status: 500,
          code: 'INTERNAL_ERROR',
          message: 'Server failed',
          requestId: 'req_fail_01',
        })
      )
      .mockResolvedValueOnce({ data: makePage() })

    const screen = await renderAuditLogs()

    const alert = screen.getByRole('alert')
    await expect.element(alert).toBeVisible()
    await expect
      .element(alert)
      .toHaveTextContent(/服务器内部错误|Internal server error/)
    await expect.element(alert).toHaveTextContent('req_fail_01')
    expect(alert.element().textContent).not.toContain('Server failed')

    // A failed first load must not also claim there is no audit data.
    expect(screen.getByRole('table').element().textContent).not.toMatch(
      /暂无审计日志|No audit logs yet|没有符合筛选条件的记录|No matching audit logs/
    )

    await alert.getByRole('button', { name: /重试|Retry/ }).click()

    await expect.element(screen.getByText('alice_admin')).toBeVisible()
    await expect.element(alert).not.toBeInTheDocument()
  })

  it('renders localized user and menu actions and has complete label catalogs', async () => {
    getMock.mockResolvedValue({ data: makePage() })

    const screen = await renderAuditLogs()
    await expect.element(screen.getByText('bob_ops')).toBeVisible()
    await expect
      .element(screen.getByText(/删除用户 \(user\.delete\)|Delete User/))
      .toBeVisible()
    await expect
      .element(screen.getByText(/菜单 \(menu\)|Menu \(menu\)/))
      .toBeVisible()

    expect(AUDIT_ACTION_OPTIONS).toHaveLength(22)
    expect(AUDIT_RESOURCE_OPTIONS).toHaveLength(6)
    for (const option of [...AUDIT_ACTION_OPTIONS, ...AUDIT_RESOURCE_OPTIONS]) {
      expect(i18n.exists(`audit:${option.labelKey}`)).toBe(true)
    }
  })

  it('writes sorting changes into the URL and request', async () => {
    getMock.mockResolvedValue({ data: makePage() })

    const screen = await renderAuditLogs()
    await expect.element(screen.getByText('alice_admin')).toBeVisible()

    const table = screen.getByRole('table')
    await table.getByRole('button', { name: /^动作$|^Action$/ }).click()
    await screen.getByRole('menuitem', { name: /^升序$|^Asc$/ }).click()

    await vi.waitFor(() => {
      expect(lastRequestParams().get('sort')).toBe('action')
      expect(lastRequestParams().get('order')).toBe('asc')
    })
    expect(searchState(screen)).toMatchObject({
      sort: 'action',
      order: 'asc',
    })
  })

  it('applies the actor filter from the toolbar', async () => {
    getMock.mockResolvedValue({ data: makePage() })

    const screen = await renderAuditLogs()
    await expect.element(screen.getByText('alice_admin')).toBeVisible()

    const input = screen.getByRole('textbox', { name: /^操作人$|^Actor$/ })
    await input.click()
    await userEvent.type(input, 'alice_admin{Enter}')

    await vi.waitFor(() => {
      expect(lastRequestParams().get('actor')).toBe('alice_admin')
    })
    expect(searchState(screen).actor).toBe('alice_admin')
  })

  it('repairs a reversed date range in the URL with replace and request', async () => {
    getMock.mockResolvedValue({ data: makePage() })

    const screen = await renderAuditLogs({
      from: '2026-11-26',
      to: '2026-11-20',
    })

    await vi.waitFor(() => {
      expect(searchState(screen)).toMatchObject({
        from: '2026-11-20',
        to: '2026-11-26',
      })
      expect(
        screen
          .getByTestId('audit-search-state')
          .element()
          .getAttribute('data-last-replace')
      ).toBe('true')
      expect(lastRequestParams().get('from')).toBe(
        toBoundaryRFC3339('2026-11-20', false)
      )
      expect(lastRequestParams().get('to')).toBe(
        toBoundaryRFC3339('2026-11-26', true)
      )
    })
  })

  it('applies the today preset and closes the picker', async () => {
    getMock.mockResolvedValue({ data: makePage() })

    const screen = await renderAuditLogs()
    await expect.element(screen.getByText('alice_admin')).toBeVisible()

    await screen.getByRole('button', { name: /时间范围|Date range/ }).click()
    const todayBtn = screen.getByRole('button', { name: /^今天$|^Today$/ })
    await expect.element(todayBtn).toBeVisible()
    await todayBtn.click()

    const today = formatDateOnly(new Date())
    await vi.waitFor(() => {
      expect(searchState(screen)).toMatchObject({ from: today, to: today })
    })
    await expect
      .element(screen.getByRole('button', { name: /^近 7 天$|^Last 7 days$/ }))
      .not.toBeInTheDocument()
  })

  it('renders semantic badge variants for success and failure', async () => {
    getMock.mockResolvedValue({ data: makePage() })

    const screen = await renderAuditLogs()
    await expect.element(screen.getByText('alice_admin')).toBeVisible()

    const successBadge = screen
      .getByText(/^成功$|^Success$/)
      .first()
      .element()
    const failureBadge = screen
      .getByText(/^失败$|^Failure$/)
      .first()
      .element()
    expect(successBadge.className).toContain('text-success-foreground')
    expect(failureBadge.className).toContain('text-destructive-foreground')
  })

  it('keeps the pageSize 100 value readable and reachable', async () => {
    getMock.mockResolvedValue({ data: makePage() })

    const screen = await renderAuditLogs({ pageSize: 100 })
    await expect.element(screen.getByText('alice_admin')).toBeVisible()

    const trigger = screen.getByRole('combobox', {
      name: /每页行数|Rows per page/,
    })
    await expect.element(trigger).toHaveTextContent('100')
    await expect
      .element(screen.getByText(/第 1 \/ 1 页|Page 1 of 1/).first())
      .toBeVisible()
  })

  it('copies the request id from the row action menu and surfaces clipboard errors', async () => {
    getMock.mockResolvedValue({ data: makePage() })
    const originalClipboard = navigator.clipboard
    const writeText = vi
      .fn()
      .mockResolvedValueOnce(undefined)
      .mockRejectedValueOnce(new Error('Permission denied'))
    const errorToastSpy = vi.spyOn(toast, 'error')
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })

    try {
      const screen = await renderAuditLogs()
      await expect.element(screen.getByText('alice_admin')).toBeVisible()

      await screen
        .getByRole('button', { name: /打开操作菜单|Open actions menu/ })
        .first()
        .click()
      await screen
        .getByRole('menuitem', { name: /复制请求 ID|Copy request ID/ })
        .click()

      await vi.waitFor(() => {
        expect(writeText).toHaveBeenCalledWith('req_audit_alpha_01')
      })
      await expect
        .element(
          screen.getByRole('menuitem', { name: /复制请求 ID|Copy request ID/ })
        )
        .not.toBeInTheDocument()

      await screen
        .getByRole('button', { name: /打开操作菜单|Open actions menu/ })
        .nth(2)
        .click()
      await screen
        .getByRole('menuitem', { name: /复制请求 ID|Copy request ID/ })
        .click()

      await vi.waitFor(() => {
        expect(errorToastSpy).toHaveBeenCalledTimes(1)
      })
    } finally {
      errorToastSpy.mockRestore()
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: originalClipboard,
      })
    }
  })

  it('supports keyboard row selection while isolating row action menu keyboard events', async () => {
    getMock.mockResolvedValue({ data: makePage() })
    const originalClipboard = navigator.clipboard
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText },
    })

    try {
      const screen = await renderAuditLogs()
      await expect.element(screen.getByText('alice_admin')).toBeVisible()

      const rows = screen.getByRole('row')
      const firstBodyRow = rows.nth(1).element() as HTMLElement
      firstBodyRow.focus()
      await userEvent.keyboard('{Enter}')
      await expect.element(screen.getByRole('dialog')).toBeVisible()

      await userEvent.keyboard('{Escape}')
      await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()

      const menuBtn = screen
        .getByRole('button', { name: /打开操作菜单|Open actions menu/ })
        .first()
      ;(menuBtn.element() as HTMLElement).focus()
      await userEvent.keyboard('{Enter}')

      const copyItem = screen.getByRole('menuitem', {
        name: /复制请求 ID|Copy request ID/,
      })
      await expect.element(copyItem).toBeVisible()
      ;(copyItem.element() as HTMLElement).focus()
      await userEvent.keyboard('{Enter}')

      await vi.waitFor(() => {
        expect(writeText).toHaveBeenCalledWith('req_audit_alpha_01')
      })
      await expect.element(screen.getByRole('dialog')).not.toBeInTheDocument()
    } finally {
      Object.defineProperty(navigator, 'clipboard', {
        configurable: true,
        value: originalClipboard,
      })
    }
  })

  it('distinguishes empty no-data state and clears actorId deep-link filters', async () => {
    const actorUuid = '01930000-0000-7000-8000-000000000099'
    getMock.mockImplementation(
      (_url: string, opts?: { params?: URLSearchParams }) => {
        if (opts?.params?.get('actorId')) {
          return Promise.resolve({
            data: { items: [], total: 0, page: 1, pageSize: 20 },
          })
        }
        return Promise.resolve({
          data: { items: [], total: 0, page: 1, pageSize: 20 },
        })
      }
    )

    const screen = await renderAuditLogs({ actorId: actorUuid })
    await expect
      .element(
        screen.getByText(/没有符合筛选条件的记录|No matching audit logs/)
      )
      .toBeVisible()
    expect(lastRequestParams().get('actorId')).toBe(actorUuid)

    await screen.getByRole('button', { name: /清除筛选|Clear filters/ }).click()
    await expect
      .element(screen.getByText(/暂无审计日志|No audit logs yet/))
      .toBeVisible()
    expect(searchState(screen).actorId).toBeUndefined()
  })

  it('applies faceted status filter and debounced global search', async () => {
    getMock.mockResolvedValue({ data: makePage() })

    const screen = await renderAuditLogs()
    await expect.element(screen.getByText('alice_admin')).toBeVisible()

    await screen
      .getByRole('button', { name: /^状态$|^Status$/ })
      .first()
      .click()
    await screen.getByRole('option', { name: /^失败$|^Failure$/ }).click()

    await vi.waitFor(() => {
      expect(lastRequestParams().getAll('status')).toEqual(['failure'])
    })

    const searchInput = screen.getByPlaceholder(
      /搜索操作人、动作、资源或请求 ID|Search actor/
    )
    await searchInput.fill('req_audit_beta_02')

    await vi.waitFor(() => {
      expect(lastRequestParams().get('q')).toBe('req_audit_beta_02')
    })
  })
})

import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/styles/index.css'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
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

  it('aligns first column spacing, removes action divider, matches row hover brightness, and highlights action button distinctly', async () => {
    const pageData: AuditLogsPage = {
      items: sampleEntries,
      total: 2,
      page: 1,
      pageSize: 20,
    }
    getMock.mockResolvedValue({ data: pageData })

    const screen = await renderAuditLogs()
    await expect.element(screen.getByText('alice_admin')).toBeVisible()

    const dataRow = document.querySelector('tbody tr') as HTMLTableRowElement
    const firstHeader = document.querySelector(
      'thead th:first-child'
    ) as HTMLTableCellElement
    const firstCell = dataRow.querySelector(
      'td:first-child'
    ) as HTMLTableCellElement
    const actionHeader = document.querySelector(
      'thead th:last-child'
    ) as HTMLTableCellElement
    const actionCell = dataRow.querySelector(
      'td:last-child'
    ) as HTMLTableCellElement
    const actionBtn = actionCell.querySelector('button') as HTMLButtonElement

    // 1. First column has comfortable left padding (16px = ps-4) away from table border.
    expect(getComputedStyle(firstHeader).paddingInlineStart).toBe('16px')
    expect(getComputedStyle(firstCell).paddingInlineStart).toBe('16px')

    // 2. No vertical divider border between data columns and sticky action column.
    expect(getComputedStyle(actionHeader).borderInlineStartWidth).toBe('0px')
    expect(getComputedStyle(actionCell).borderInlineStartWidth).toBe('0px')

    // 3. Hovering a data row makes sticky action cell opaque and brightness-matched to row hover.
    await userEvent.hover(firstCell)
    await expect
      .poll(() => getComputedStyle(actionCell).backgroundColor)
      .not.toBe(getComputedStyle(document.body).backgroundColor)
    const rowHoverBg = getComputedStyle(dataRow).backgroundColor
    const actionCellHoverBg = getComputedStyle(actionCell).backgroundColor
    expect(rowHoverBg).not.toBe('rgba(0, 0, 0, 0)')
    // Action cell background must be fully opaque (no alpha channel) so scrolled text never bleeds through.
    expect(actionCellHoverBg).not.toMatch(/rgba\(|\/\s*0\./)

    // 4. Action button is compact (28x28 inside 48px cell) and highlights distinctly on hover.
    const btnRect = actionBtn.getBoundingClientRect()
    const cellRect = actionCell.getBoundingClientRect()
    expect(btnRect.width).toBe(28)
    expect(btnRect.height).toBe(28)
    expect(cellRect.width - btnRect.width).toBeGreaterThanOrEqual(16)

    await userEvent.hover(actionBtn)
    await expect
      .poll(() => getComputedStyle(actionBtn).backgroundColor)
      .not.toBe('rgba(0, 0, 0, 0)')
    const actionBtnHoverBg = getComputedStyle(actionBtn).backgroundColor
    expect(actionBtnHoverBg).not.toBe(actionCellHoverBg)
  })

  it('keeps pagination page indicator inline with page buttons at desktop width', async () => {
    const pageData: AuditLogsPage = {
      items: sampleEntries,
      total: 2,
      page: 1,
      pageSize: 20,
    }
    getMock.mockResolvedValue({ data: pageData })

    const screen = await renderAuditLogs()
    await expect.element(screen.getByText('alice_admin')).toBeVisible()

    // Ensure container is desktop width (>= 672px / @2xl/pagination).
    const container = document.querySelector(
      '[data-slot="data-table-pagination"]'
    ) as HTMLElement
    container.style.width = '900px'

    const pageOfTexts = Array.from(container.querySelectorAll('span')).filter(
      (el) =>
        !el.classList.contains('sr-only') &&
        /第\s*1\s*\/\s*1\s*页|Page 1 of 1/.test(el.textContent ?? '') &&
        getComputedStyle(el).display !== 'none'
    )
    expect(pageOfTexts).toHaveLength(1)

    const pageOneBtn = screen
      .getByRole('button', { name: /第\s*1\s*\/\s*1\s*页|Page 1 of 1/ })
      .element()
    const infoRect = pageOfTexts[0].getBoundingClientRect()
    const btnRect = pageOneBtn.getBoundingClientRect()

    // Inline on the same row, vertically aligned, and positioned to the left of page buttons.
    expect(Math.abs(infoRect.top - btnRect.top)).toBeLessThan(10)
    expect(infoRect.right).toBeLessThan(btnRect.left)
  })

  it('displays pageSize 100 in SelectTrigger without text clipping', async () => {
    const pageData: AuditLogsPage = {
      items: sampleEntries,
      total: 2,
      page: 1,
      pageSize: 100,
    }
    getMock.mockResolvedValue({ data: pageData })

    const screen = await renderAuditLogs({ pageSize: 100 })
    await expect.element(screen.getByText('alice_admin')).toBeVisible()

    const trigger = screen
      .getByRole('combobox', { name: /每页行数|Rows per page/ })
      .element() as HTMLElement
    const valueEl = trigger.querySelector(
      '[data-slot="select-value"]'
    ) as HTMLElement

    expect(trigger.textContent).toContain('100')
    expect(valueEl.scrollWidth).toBeLessThanOrEqual(valueEl.clientWidth)
    expect(trigger.getBoundingClientRect().width).toBeGreaterThanOrEqual(80)
  })

  it('renders success status with soft semantic success badge styling', async () => {
    const pageData: AuditLogsPage = {
      items: sampleEntries,
      total: 2,
      page: 1,
      pageSize: 20,
    }
    getMock.mockResolvedValue({ data: pageData })

    const screen = await renderAuditLogs()
    await expect.element(screen.getByText('alice_admin')).toBeVisible()

    const badges = Array.from(
      document.querySelectorAll('[data-slot="badge"]')
    ) as HTMLElement[]
    const successBadge = badges.find((el) =>
      /成功|Success/.test(el.textContent ?? '')
    )
    const failureBadge = badges.find((el) =>
      /失败|Failure/.test(el.textContent ?? '')
    )
    expect(successBadge).toBeDefined()
    expect(failureBadge).toBeDefined()

    const successStyle = getComputedStyle(successBadge!)
    const failureStyle = getComputedStyle(failureBadge!)
    expect(successStyle.backgroundColor).not.toBe(failureStyle.backgroundColor)
    expect(successStyle.borderTopColor).not.toBe('rgba(0, 0, 0, 0)')
  })

  it('wraps pagination buttons cleanly on narrow 320px viewports without horizontal overflow', async () => {
    const pageData: AuditLogsPage = {
      items: sampleEntries,
      total: 20000,
      page: 500,
      pageSize: 20,
    }
    getMock.mockResolvedValue({ data: pageData })

    const screen = await renderAuditLogs({ page: 500, pageSize: 20 })
    await expect.element(screen.getByText('alice_admin')).toBeVisible()

    const container = document.querySelector(
      '[data-slot="data-table-pagination"]'
    ) as HTMLElement
    container.style.width = '320px'

    await expect
      .poll(() => container.scrollWidth <= container.clientWidth)
      .toBe(true)
  })
})

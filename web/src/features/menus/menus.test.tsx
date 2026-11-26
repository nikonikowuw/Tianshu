import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/styles/index.css'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
import i18n, { changeAppLanguage } from '@/i18n/index'
import { ApiError } from '@/lib/api/error'
import { buildNavGroupsFromMenus } from '@/app/navigation'
import { MenuDeleteDialog } from './components/menu-delete-dialog'
import { MenuFormSheet } from './components/menu-form-sheet'
import { MenuTreeTable } from './components/menu-tree-table'
import type { MenuItem } from './types'

const getMock = vi.fn()
const postMock = vi.fn()
const patchMock = vi.fn()
const putMock = vi.fn()
const deleteMock = vi.fn()

vi.mock('@/lib/api/client', () => ({
  apiClient: {
    get: (...args: unknown[]) => getMock(...args),
    post: (...args: unknown[]) => postMock(...args),
    patch: (...args: unknown[]) => patchMock(...args),
    put: (...args: unknown[]) => putMock(...args),
    delete: (...args: unknown[]) => deleteMock(...args),
  },
}))

const sampleTree: MenuItem[] = [
  {
    id: 'dir-1',
    parentId: null,
    type: 'directory',
    code: 'dir_system',
    title: 'System',
    path: '',
    icon: '',
    permissionCode: '',
    sortOrder: 10,
    visible: true,
    status: 'active',
    isSystem: true,
    children: [
      {
        id: 'menu-1',
        parentId: 'dir-1',
        type: 'menu',
        code: 'menu_users',
        title: 'Users',
        path: '/users',
        icon: 'Users',
        permissionCode: 'users:read',
        sortOrder: 10,
        visible: true,
        status: 'active',
        isSystem: true,
        children: [],
        createdBy: null,
        updatedBy: null,
        createdAt: '2026-11-26T00:00:00Z',
        updatedAt: '2026-11-26T00:00:00Z',
      },
      {
        id: 'menu-2',
        parentId: 'dir-1',
        type: 'menu',
        code: 'menu_roles',
        title: 'Roles',
        path: '/roles',
        icon: 'Shield',
        permissionCode: 'roles:read',
        sortOrder: 20,
        visible: true,
        status: 'active',
        isSystem: true,
        children: [],
        createdBy: null,
        updatedBy: null,
        createdAt: '2026-11-26T00:00:00Z',
        updatedAt: '2026-11-26T00:00:00Z',
      },
    ],
    createdBy: null,
    updatedBy: null,
    createdAt: '2026-11-26T00:00:00Z',
    updatedAt: '2026-11-26T00:00:00Z',
  },
]

describe('menus feature', () => {
  afterEach(async () => {
    vi.clearAllMocks()
    await changeAppLanguage('zh-CN')
  })

  it('builds dynamic nav groups from menu tree and omits button nodes', () => {
    const groups = buildNavGroupsFromMenus([
      {
        ...sampleTree[0],
        children: [
          ...sampleTree[0].children,
          {
            id: 'btn-1',
            type: 'button',
            title: 'Create',
            path: '',
            icon: '',
            children: [],
          },
        ],
      },
    ])
    expect(groups).toHaveLength(1)
    expect(groups[0].title).toBe('System')
    expect(groups[0].items).toHaveLength(2)
  })

  it('renders menu tree table, empty state, and triggers move down reorder', async () => {
    await changeAppLanguage('en-US')
    const onMoveSibling = vi.fn()
    const client = new QueryClient()

    const emptyScreen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <MenuTreeTable
            items={[]}
            isLoading={false}
            expandedIds={new Set()}
            onToggleExpand={vi.fn()}
            canCreate={false}
            canUpdate={false}
            canDelete={false}
            isReordering={false}
            onMoveSibling={vi.fn()}
            onAddChild={vi.fn()}
            onEdit={vi.fn()}
            onDelete={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )
    await expect
      .element(emptyScreen.getByText(i18n.t('menus:empty')))
      .toBeVisible()

    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <MenuTreeTable
            items={sampleTree}
            isLoading={false}
            expandedIds={new Set(['dir-1'])}
            onToggleExpand={vi.fn()}
            canCreate={true}
            canUpdate={true}
            canDelete={true}
            isReordering={false}
            onMoveSibling={onMoveSibling}
            onAddChild={vi.fn()}
            onEdit={vi.fn()}
            onDelete={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect
      .element(screen.getByText('System', { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByText('Users', { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByText('Roles', { exact: true }))
      .toBeVisible()

    const moveDownBtns = screen.getByRole('button', {
      name: i18n.t('menus:actions.moveDown'),
    })
    await userEvent.click(moveDownBtns.nth(1))
    expect(onMoveSibling).toHaveBeenCalledWith(
      sampleTree[0].children,
      0,
      'down'
    )
  })

  it('validates MenuFormSheet and maps 409/422 errors onto fields', async () => {
    await changeAppLanguage('en-US')
    getMock.mockResolvedValue({ data: { groups: [] } })
    postMock
      .mockRejectedValueOnce(
        new ApiError({
          status: 409,
          code: 'MENUS_CODE_CONFLICT',
          message: 'Menu code already exists',
        })
      )
      .mockRejectedValueOnce(
        new ApiError({
          status: 422,
          code: 'VALIDATION_FAILED',
          message: 'Validation failed',
          fieldErrors: { path: 'Invalid path from server' },
        })
      )

    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })

    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <MenuFormSheet
            open={true}
            mode='create'
            menu={null}
            defaultParentId={null}
            treeItems={sampleTree}
            onOpenChange={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    const submitBtn = screen.getByRole('button', {
      name: i18n.t('menus:form.submitCreate'),
    })

    // Client-side validation triggers when path is missing for type=menu.
    await userEvent.fill(
      screen.getByPlaceholder(i18n.t('menus:form.codePlaceholder')),
      'custom_reports'
    )
    await userEvent.fill(
      screen.getByPlaceholder(i18n.t('menus:form.titlePlaceholder')),
      'Reports'
    )
    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText(i18n.t('validation:menuPathFormat')))
      .toBeVisible()

    // Fill valid path -> hits 409 code conflict, then 422 path fieldError.
    await userEvent.fill(
      screen.getByPlaceholder(i18n.t('menus:form.pathPlaceholder')),
      '/reports'
    )
    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText('Menu code already exists'))
      .toBeVisible()

    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText('Invalid path from server'))
      .toBeVisible()
  })

  it('calls deleteMenu on MenuDeleteDialog confirm', async () => {
    await changeAppLanguage('en-US')
    deleteMock.mockResolvedValueOnce({ data: undefined })
    const client = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })

    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <MenuDeleteDialog
            menu={sampleTree[0].children[0]}
            open={true}
            onOpenChange={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('menus:deleteDialog.confirm') })
    )
    expect(deleteMock).toHaveBeenCalledWith(
      `/menus/${sampleTree[0].children[0].id}`
    )
  })
})

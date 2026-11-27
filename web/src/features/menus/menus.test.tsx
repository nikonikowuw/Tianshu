import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/styles/index.css'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
import i18n, { changeAppLanguage } from '@/i18n/index'
import { useAuthStore } from '@/stores/auth-store'
import { ApiError } from '@/lib/api/error'
import { SearchProvider } from '@/context/search-provider'
import { ThemeProvider } from '@/context/theme-provider'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { buildNavGroupsFromMenus } from '@/app/navigation'
import { MenuDeleteDialog } from './components/menu-delete-dialog'
import { MenuFormSheet } from './components/menu-form-sheet'
import { Menus } from './index'
import type { MenuItem, PermissionCatalogResponse } from './types'

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
        children: [
          {
            id: 'btn-1',
            parentId: 'menu-1',
            type: 'button',
            code: 'btn_users_create',
            title: 'Create User Button',
            path: '',
            icon: '',
            permissionCode: 'users:create',
            sortOrder: 10,
            visible: false,
            status: 'active',
            isSystem: false,
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
        isSystem: false,
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

const samplePermissionsCatalog: PermissionCatalogResponse = {
  groups: [
    {
      module: 'identity',
      label: 'Identity',
      permissions: [
        { code: 'users:read', name: 'Read Users' },
        { code: 'users:delete', name: 'Delete Users' },
      ],
    },
  ],
}

function createTestClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function setTestPermissions(permissions: string[]) {
  useAuthStore.getState().setSession({
    accessToken: 'test-token',
    user: {
      id: 'user-1',
      username: 'tester',
      email: 'tester@example.com',
      displayName: 'Tester',
    },
    roles: ['custom'],
    permissions,
  })
}

async function renderMenusPage(
  permissions: string[],
  client = createTestClient()
) {
  setTestPermissions(permissions)
  return render(
    <QueryClientProvider client={client}>
      <I18nextProvider i18n={i18n}>
        <ThemeProvider>
          <SearchProvider>
            <SidebarProvider>
              <SidebarInset>
                <Menus />
              </SidebarInset>
            </SidebarProvider>
          </SearchProvider>
        </ThemeProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
}

describe('menus feature', () => {
  afterEach(async () => {
    vi.clearAllMocks()
    useAuthStore.getState().clearSession()
    await changeAppLanguage('zh-CN')
  })

  it('builds dynamic nav groups from menu tree and omits button nodes', () => {
    const groups = buildNavGroupsFromMenus([
      {
        ...sampleTree[0],
        children: [
          ...sampleTree[0].children,
          {
            id: 'btn-extra',
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

  it('renders load error without empty table, recovers via retry, and supports expand/collapse', async () => {
    await changeAppLanguage('en-US')
    getMock
      .mockRejectedValueOnce(
        new ApiError({
          status: 500,
          code: 'INTERNAL_ERROR',
          message: 'boom',
          requestId: 'req-menu-500',
        })
      )
      .mockResolvedValueOnce({ data: { items: [] } })
      .mockResolvedValueOnce({ data: { items: sampleTree } })

    const client = createTestClient()
    const errorScreen = await renderMenusPage(['menus:read'], client)

    await expect
      .element(
        errorScreen.getByText(
          i18n.t('errors:serverError', { requestId: 'req-menu-500' })
        )
      )
      .toBeVisible()
    await expect
      .element(errorScreen.getByText(i18n.t('menus:empty')))
      .not.toBeInTheDocument()

    await userEvent.click(
      errorScreen.getByRole('button', { name: i18n.t('menus:error.retry') })
    )
    await expect
      .element(errorScreen.getByText(i18n.t('menus:empty')))
      .toBeVisible()
    await expect
      .element(
        errorScreen.getByRole('button', { name: i18n.t('menus:expandAll') })
      )
      .toBeDisabled()
    await expect
      .element(
        errorScreen.getByRole('button', { name: i18n.t('menus:collapseAll') })
      )
      .toBeDisabled()
    await errorScreen.unmount()

    const screen = await renderMenusPage(['menus:read'], createTestClient())
    await expect
      .element(screen.getByText('System', { exact: true }))
      .toBeVisible()
    await expect
      .element(screen.getByText('Users', { exact: true }))
      .toBeVisible()

    const collapseSystemBtn = screen.getByRole('button', {
      name: i18n.t('menus:actions.collapseNode', { title: 'System' }),
    })
    await expect
      .element(collapseSystemBtn)
      .toHaveAttribute('aria-expanded', 'true')
    await userEvent.click(collapseSystemBtn)
    await expect
      .element(screen.getByText('Users', { exact: true }))
      .not.toBeInTheDocument()

    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('menus:expandAll') })
    )
    await expect
      .element(screen.getByText('Users', { exact: true }))
      .toBeVisible()

    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('menus:collapseAll') })
    )
    await expect
      .element(screen.getByText('Users', { exact: true }))
      .not.toBeInTheDocument()
  })

  it('enforces fine-grained permissions, hides empty row action menus, and submits sibling reorder', async () => {
    await changeAppLanguage('en-US')
    getMock.mockResolvedValue({ data: { items: sampleTree } })
    putMock.mockResolvedValue({ data: { items: sampleTree } })

    const readOnlyScreen = await renderMenusPage(
      ['menus:read'],
      createTestClient()
    )
    await expect
      .element(readOnlyScreen.getByText('System', { exact: true }))
      .toBeVisible()
    await expect
      .element(
        readOnlyScreen.getByRole('button', {
          name: i18n.t('menus:createMenu'),
        })
      )
      .not.toBeInTheDocument()
    await expect
      .element(
        readOnlyScreen.getByRole('button', {
          name: i18n.t('menus:actions.openMenu'),
        })
      )
      .not.toBeInTheDocument()
    await readOnlyScreen.unmount()

    // With only menus:create, button nodes have no actions and must not render an empty menu trigger.
    const createOnlyScreen = await renderMenusPage(
      ['menus:read', 'menus:create'],
      createTestClient()
    )
    await expect
      .element(
        createOnlyScreen.getByText('Create User Button', { exact: true })
      )
      .toBeVisible()
    await expect
      .element(
        createOnlyScreen.getByRole('button', {
          name: i18n.t('menus:actions.openMenu'),
        })
      )
      .toHaveLength(3)
    await createOnlyScreen.unmount()

    // With only menus:delete, delete menuitem renders without an orphan separator and disables system/parent rows with tooltip.
    const deleteOnlyScreen = await renderMenusPage(
      ['menus:read', 'menus:delete'],
      createTestClient()
    )
    await expect
      .element(deleteOnlyScreen.getByText('System', { exact: true }))
      .toBeVisible()
    const menuTriggers = deleteOnlyScreen.getByRole('button', {
      name: i18n.t('menus:actions.openMenu'),
    })
    await userEvent.click(menuTriggers.first())
    const disabledDeleteItem = deleteOnlyScreen.getByRole('menuitem', {
      name: i18n.t('menus:actions.delete'),
    })
    await expect
      .element(disabledDeleteItem)
      .toHaveAttribute('aria-disabled', 'true')
    await expect
      .element(deleteOnlyScreen.getByRole('separator'))
      .not.toBeInTheDocument()
    await userEvent.hover(disabledDeleteItem, { force: true })
    await expect
      .element(
        deleteOnlyScreen
          .getByRole('tooltip', {
            name: i18n.t('menus:actions.systemImmutableHint'),
          })
          .first()
      )
      .toBeVisible()
    await deleteOnlyScreen.unmount()

    // With menus:update, moving Users down submits PUT /menus/reorder and invalidates menu + role caches.
    const updateClient = createTestClient()
    const invalidateSpy = vi.spyOn(updateClient, 'invalidateQueries')
    const updateScreen = await renderMenusPage(
      ['menus:read', 'menus:update'],
      updateClient
    )
    await expect
      .element(updateScreen.getByText('Users', { exact: true }))
      .toBeVisible()

    await userEvent.click(
      updateScreen.getByRole('button', {
        name: i18n.t('menus:actions.moveDownItem', { title: 'Users' }),
      })
    )
    expect(putMock).toHaveBeenCalledWith('/menus/reorder', {
      items: [
        { id: 'menu-2', parentId: 'dir-1', sortOrder: 10 },
        { id: 'menu-1', parentId: 'dir-1', sortOrder: 20 },
      ],
    })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['menus'] })
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ['roles', 'menu-tree'],
    })
    await updateScreen.unmount()
  })

  it('guards /permissions fetch with roles:read, preserves fallback values, and retries catalog load', async () => {
    await changeAppLanguage('en-US')
    const customIconMenu: MenuItem = {
      ...sampleTree[0].children[1],
      icon: 'CustomLegacyIcon',
      permissionCode: 'users:read',
    }

    setTestPermissions(['menus:read', 'menus:update'])
    const restrictedScreen = await render(
      <QueryClientProvider client={createTestClient()}>
        <I18nextProvider i18n={i18n}>
          <MenuFormSheet
            open={true}
            mode='edit'
            menu={customIconMenu}
            defaultParentId={customIconMenu.parentId}
            treeItems={sampleTree}
            onOpenChange={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    expect(getMock).not.toHaveBeenCalled()
    await expect
      .element(
        restrictedScreen.getByText(
          i18n.t('menus:form.permissionsRestrictedHint')
        )
      )
      .toBeVisible()
    await expect
      .element(
        restrictedScreen.getByLabelText(i18n.t('menus:form.permissionCode'), {
          exact: true,
        })
      )
      .toHaveTextContent('users:read')
    await expect
      .element(
        restrictedScreen.getByLabelText(i18n.t('menus:form.icon'), {
          exact: true,
        })
      )
      .toHaveTextContent('CustomLegacyIcon')
    await restrictedScreen.unmount()

    setTestPermissions(['menus:read', 'menus:create', 'roles:read'])
    getMock
      .mockRejectedValueOnce(new Error('network fail'))
      .mockResolvedValueOnce({ data: samplePermissionsCatalog })

    const retryScreen = await render(
      <QueryClientProvider client={createTestClient()}>
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

    await expect
      .element(
        retryScreen.getByText(i18n.t('menus:form.permissionsLoadFailed'))
      )
      .toBeVisible()
    await userEvent.click(
      retryScreen.getByRole('button', { name: i18n.t('common:retry') })
    )
    expect(getMock).toHaveBeenCalledTimes(2)
    await expect
      .element(
        retryScreen.getByText(i18n.t('menus:form.permissionsLoadFailed'))
      )
      .not.toBeInTheDocument()
  })

  it('validates via labels, sanitizes button payload, and maps 409/422/500 errors cleanly', async () => {
    await changeAppLanguage('en-US')
    setTestPermissions(['menus:read', 'menus:create', 'roles:read'])
    getMock.mockResolvedValue({ data: samplePermissionsCatalog })
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
          fieldErrors: {
            path: 'Invalid path from server',
            visible: 'Invalid visible from server',
            status: 'Invalid status from server',
          },
        })
      )
      .mockRejectedValueOnce(
        new ApiError({
          status: 500,
          code: 'INTERNAL_ERROR',
          message: 'Internal error',
          requestId: 'req-form-500',
        })
      )
      .mockRejectedValueOnce(
        new ApiError({
          status: 422,
          code: 'VALIDATION_FAILED',
          message: 'Button validation fallback',
          fieldErrors: { path: 'Hidden path rejected' },
        })
      )
      .mockResolvedValueOnce({
        data: {
          ...sampleTree[0].children[0].children[0],
          id: 'btn-created',
          code: 'btn_export',
          title: 'Export',
        },
      })

    const onOpenChange = vi.fn()
    const client = createTestClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <MenuFormSheet
            open={true}
            mode='create'
            menu={null}
            defaultParentId={null}
            treeItems={sampleTree}
            onOpenChange={onOpenChange}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    const submitBtn = screen.getByRole('button', {
      name: i18n.t('menus:form.submitCreate'),
    })
    const codeInput = screen.getByLabelText(i18n.t('menus:form.code'), {
      exact: true,
    })
    const titleInput = screen.getByLabelText(i18n.t('menus:form.title'), {
      exact: true,
    })
    const pathInput = screen.getByLabelText(i18n.t('menus:form.path'), {
      exact: true,
    })
    const sortInput = screen.getByLabelText(i18n.t('menus:form.sortOrder'), {
      exact: true,
    })
    const typeSelect = screen.getByLabelText(i18n.t('menus:form.type'), {
      exact: true,
    })
    const parentSelect = screen.getByLabelText(i18n.t('menus:form.parent'), {
      exact: true,
    })

    await userEvent.fill(codeInput, 'custom_reports')
    await userEvent.fill(titleInput, 'Reports')
    await userEvent.clear(sortInput)
    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText(i18n.t('validation:required')))
      .toBeVisible()
    await expect
      .element(screen.getByText(i18n.t('validation:menuPathFormat')))
      .toBeVisible()

    await userEvent.fill(sortInput, '100000')
    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText(i18n.t('validation:menuSortOrderRange')))
      .toBeVisible()

    await userEvent.fill(sortInput, '25')
    await userEvent.fill(pathInput, '/reports')

    // 409 code conflict -> code field, 422 fieldErrors -> path field, 500 -> AuthAlert banner.
    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText('Menu code already exists'))
      .toBeVisible()

    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText('Invalid path from server'))
      .toBeVisible()
    await expect
      .element(screen.getByText('Invalid visible from server'))
      .toBeVisible()
    await expect
      .element(screen.getByText('Invalid status from server'))
      .toBeVisible()

    await userEvent.click(submitBtn)
    await expect
      .element(
        screen.getByText(
          i18n.t('errors:serverError', { requestId: 'req-form-500' })
        )
      )
      .toBeVisible()

    // Changing Radix Select clears bannerError and switching to button omits invalid hidden path.
    await userEvent.fill(pathInput, 'bad-hidden-path')
    await userEvent.click(typeSelect)
    await userEvent.click(
      screen.getByRole('option', { name: i18n.t('menus:types.button') })
    )
    await expect
      .element(
        screen.getByText(
          i18n.t('errors:serverError', { requestId: 'req-form-500' })
        )
      )
      .not.toBeInTheDocument()
    await expect.element(pathInput).not.toBeInTheDocument()

    // Root parent is incompatible with button type -> client validation catches it before submitting.
    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText(i18n.t('validation:menuInvalidParent')))
      .toBeVisible()
    expect(postMock).toHaveBeenCalledTimes(3)

    // Pick a menu parent -> unmounted 422 path error surfaces in banner instead of swallowing silently, then retry succeeds.
    await userEvent.click(parentSelect)
    await userEvent.click(screen.getByRole('option', { name: /Users/ }))
    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText('Button validation fallback'))
      .toBeVisible()

    await userEvent.click(submitBtn)

    expect(postMock).toHaveBeenLastCalledWith('/menus', {
      parentId: 'menu-1',
      type: 'button',
      code: 'custom_reports',
      title: 'Reports',
      path: '',
      icon: '',
      permissionCode: '',
      sortOrder: 25,
      visible: false,
      status: 'active',
    })
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('edits custom/system menus via PATCH and handles MenuDeleteDialog success and 409 error', async () => {
    await changeAppLanguage('en-US')
    setTestPermissions(['menus:read', 'menus:update', 'menus:delete'])
    patchMock.mockResolvedValueOnce({
      data: { ...sampleTree[0].children[1], title: 'Roles Updated' },
    })

    const client = createTestClient()
    const invalidateSpy = vi.spyOn(client, 'invalidateQueries')
    const onOpenChange = vi.fn()

    const customEditScreen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <MenuFormSheet
            open={true}
            mode='edit'
            menu={sampleTree[0].children[1]}
            defaultParentId={sampleTree[0].id}
            treeItems={sampleTree}
            onOpenChange={onOpenChange}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect
      .element(
        customEditScreen.getByLabelText(i18n.t('menus:form.code'), {
          exact: true,
        })
      )
      .toBeDisabled()
    const titleInput = customEditScreen.getByLabelText(
      i18n.t('menus:form.title'),
      { exact: true }
    )
    await userEvent.clear(titleInput)
    await userEvent.fill(titleInput, 'Roles Updated')
    await userEvent.click(
      customEditScreen.getByRole('button', {
        name: i18n.t('menus:form.submitSave'),
      })
    )

    expect(patchMock).toHaveBeenCalledWith('/menus/menu-2', {
      parentId: 'dir-1',
      type: 'menu',
      title: 'Roles Updated',
      path: '/roles',
      icon: 'Shield',
      permissionCode: 'roles:read',
      sortOrder: 20,
      visible: true,
      status: 'active',
    })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['menus'] })
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ['roles', 'menu-tree'],
    })
    expect(onOpenChange).toHaveBeenCalledWith(false)
    await customEditScreen.unmount()

    const systemEditScreen = await render(
      <QueryClientProvider client={createTestClient()}>
        <I18nextProvider i18n={i18n}>
          <MenuFormSheet
            open={true}
            mode='edit'
            menu={sampleTree[0]}
            defaultParentId={null}
            treeItems={sampleTree}
            onOpenChange={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )
    await expect
      .element(
        systemEditScreen.getByLabelText(i18n.t('menus:form.type'), {
          exact: true,
        })
      )
      .toBeDisabled()
    await systemEditScreen.unmount()

    deleteMock
      .mockRejectedValueOnce(
        new ApiError({
          status: 409,
          code: 'MENUS_HAS_CHILDREN',
          message: 'Cannot delete menu with children',
        })
      )
      .mockResolvedValueOnce({ data: undefined })

    const onDeleteOpenChange = vi.fn()
    const deleteScreen = await render(
      <QueryClientProvider client={createTestClient()}>
        <I18nextProvider i18n={i18n}>
          <MenuDeleteDialog
            menu={sampleTree[0].children[1]}
            open={true}
            onOpenChange={onDeleteOpenChange}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    const confirmBtn = deleteScreen.getByRole('button', {
      name: i18n.t('menus:deleteDialog.confirm'),
    })
    await userEvent.click(confirmBtn)
    expect(deleteMock).toHaveBeenCalledWith('/menus/menu-2')
    expect(onDeleteOpenChange).not.toHaveBeenCalledWith(false)

    await userEvent.click(confirmBtn)
    expect(onDeleteOpenChange).toHaveBeenCalledWith(false)
  })
})

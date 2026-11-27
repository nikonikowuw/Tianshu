import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/styles/index.css'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
import i18n, { changeAppLanguage } from '@/i18n/index'
import { useAuthStore } from '@/stores/auth-store'
import { ApiError } from '@/lib/api/error'
import { Button } from '@/components/ui/button'
import { Toaster } from '@/components/ui/sonner'
import { PermissionGuard } from '@/components/shared/permission-guard'
import { buildRolesQueryParams } from './api'
import { buildPermissionTree } from './components/permission-tree-builder'
import { RoleDeleteDialog } from './components/role-delete-dialog'
import { RoleFormSheet, type RoleSheetMode } from './components/role-form-sheet'
import { RolesTable } from './components/roles-table'
import { rolesSearchSchema, type RolesSearch } from './schema'
import type {
  PermissionsCatalogResponse,
  RoleItem,
  RoleMenuTreeNode,
  RoleMenuTreeResponse,
  RolesPage,
} from './types'

const getMock = vi.fn()
const postMock = vi.fn()
const patchMock = vi.fn()
const deleteMock = vi.fn()

vi.mock('@/lib/api/client', () => ({
  apiClient: {
    get: (...args: unknown[]) => getMock(...args),
    post: (...args: unknown[]) => postMock(...args),
    patch: (...args: unknown[]) => patchMock(...args),
    delete: (...args: unknown[]) => deleteMock(...args),
  },
}))

const catalogZh: PermissionsCatalogResponse = {
  groups: [
    {
      module: 'dashboard',
      label: '仪表盘',
      permissions: [
        {
          code: 'dashboard:read',
          module: 'dashboard',
          name: '查看仪表盘',
          description: '查看系统概览统计指标',
        },
      ],
    },
    {
      module: 'identity',
      label: '用户管理',
      permissions: [
        {
          code: 'users:read',
          module: 'identity',
          name: '查看用户',
          description: '查看用户列表',
        },
        {
          code: 'users:create',
          module: 'identity',
          name: '创建用户',
          description: '新增系统用户',
        },
        {
          code: 'users:delete',
          module: 'identity',
          name: '删除用户',
          description: '软删除用户账号',
        },
      ],
    },
    {
      module: 'audit',
      label: '审计日志',
      permissions: [
        {
          code: 'audit:export',
          module: 'audit',
          name: '导出审计日志',
          description: '导出审计流水',
        },
      ],
    },
  ],
}

const catalogEn: PermissionsCatalogResponse = {
  groups: [
    {
      module: 'dashboard',
      label: 'Dashboard',
      permissions: [
        {
          code: 'dashboard:read',
          module: 'dashboard',
          name: 'View Dashboard',
          description: 'View overview metrics',
        },
      ],
    },
    {
      module: 'identity',
      label: 'Users',
      permissions: [
        {
          code: 'users:read',
          module: 'identity',
          name: 'View Users',
          description: 'View user directory',
        },
        {
          code: 'users:create',
          module: 'identity',
          name: 'Create Users',
          description: 'Create user accounts',
        },
        {
          code: 'users:delete',
          module: 'identity',
          name: 'Delete Users',
          description: 'Soft-delete user accounts',
        },
      ],
    },
    {
      module: 'audit',
      label: 'Audit Logs',
      permissions: [
        {
          code: 'audit:export',
          module: 'audit',
          name: 'Export Audit Logs',
          description: 'Export audit entries',
        },
      ],
    },
  ],
}

const menuTreeZh: RoleMenuTreeResponse = {
  items: [
    {
      id: 'dir-general',
      type: 'directory',
      code: 'dir_general',
      title: '概览',
      icon: '',
      permissionCode: '',
      children: [
        {
          id: 'menu-dashboard',
          type: 'menu',
          code: 'menu_dashboard',
          title: '仪表盘',
          icon: 'LayoutDashboard',
          permissionCode: 'dashboard:read',
          children: [],
        },
      ],
    },
    {
      id: 'dir-system',
      type: 'directory',
      code: 'dir_system',
      title: '系统管理',
      icon: '',
      permissionCode: '',
      children: [
        {
          id: 'menu-users',
          type: 'menu',
          code: 'menu_users',
          title: '用户管理',
          icon: 'Users',
          permissionCode: 'users:read',
          children: [
            {
              id: 'btn-users-create',
              type: 'button',
              code: 'btn_users_create',
              title: '创建用户',
              icon: '',
              permissionCode: 'users:create',
              children: [],
            },
            {
              id: 'btn-users-delete',
              type: 'button',
              code: 'btn_users_delete',
              title: '删除用户',
              icon: '',
              permissionCode: 'users:delete',
              children: [],
            },
          ],
        },
        {
          id: 'menu-settings',
          type: 'menu',
          code: 'menu_settings',
          title: '个人设置',
          icon: 'Settings',
          permissionCode: '',
          children: [],
        },
      ],
    },
  ],
}

const menuTreeEn: RoleMenuTreeResponse = {
  items: [
    {
      id: 'dir-general',
      type: 'directory',
      code: 'dir_general',
      title: 'General',
      icon: '',
      permissionCode: '',
      children: [
        {
          id: 'menu-dashboard',
          type: 'menu',
          code: 'menu_dashboard',
          title: 'Dashboard',
          icon: 'LayoutDashboard',
          permissionCode: 'dashboard:read',
          children: [],
        },
      ],
    },
    {
      id: 'dir-system',
      type: 'directory',
      code: 'dir_system',
      title: 'System',
      icon: '',
      permissionCode: '',
      children: [
        {
          id: 'menu-users',
          type: 'menu',
          code: 'menu_users',
          title: 'Users',
          icon: 'Users',
          permissionCode: 'users:read',
          children: [
            {
              id: 'btn-users-create',
              type: 'button',
              code: 'btn_users_create',
              title: 'Create Users',
              icon: '',
              permissionCode: 'users:create',
              children: [],
            },
            {
              id: 'btn-users-delete',
              type: 'button',
              code: 'btn_users_delete',
              title: 'Delete Users',
              icon: '',
              permissionCode: 'users:delete',
              children: [],
            },
          ],
        },
        {
          id: 'menu-settings',
          type: 'menu',
          code: 'menu_settings',
          title: 'Settings',
          icon: 'Settings',
          permissionCode: '',
          children: [],
        },
      ],
    },
  ],
}

const sampleRoles: RoleItem[] = [
  {
    id: '01930000-0000-7000-8000-000000000001',
    code: 'super_admin',
    name: 'Super Admin',
    description: 'Built-in full access role',
    isSystem: true,
    userCount: 1,
    permissionCount: 5,
    permissions: [
      'dashboard:read',
      'users:read',
      'users:create',
      'users:delete',
      'audit:export',
    ],
    createdBy: null,
    updatedBy: null,
    createdAt: '2026-11-26T08:00:00Z',
    updatedAt: '2026-11-26T08:00:00Z',
  },
  {
    id: '01930000-0000-7000-8000-000000000002',
    code: 'ops_editor',
    name: 'Operations Editor',
    description: 'Daily operations read and view',
    isSystem: false,
    userCount: 0,
    permissionCount: 1,
    permissions: ['dashboard:read'],
    createdBy: null,
    updatedBy: null,
    createdAt: '2026-11-26T09:00:00Z',
    updatedAt: '2026-11-26T09:30:00Z',
  },
  {
    id: '01930000-0000-7000-8000-000000000003',
    code: 'bound_auditor',
    name: 'Bound Auditor',
    description: 'Custom role assigned to users',
    isSystem: false,
    userCount: 2,
    permissionCount: 1,
    permissions: ['users:read'],
    createdBy: null,
    updatedBy: null,
    createdAt: '2026-11-26T09:10:00Z',
    updatedAt: '2026-11-26T09:10:00Z',
  },
]

function createTestClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function RolesHarness({
  initialSearch,
  canCreate = true,
  canUpdate = true,
  canDelete = true,
  onSearchChange,
  onRawSearchChange,
}: {
  initialSearch?: Partial<RolesSearch>
  canCreate?: boolean
  canUpdate?: boolean
  canDelete?: boolean
  onSearchChange?: (next: RolesSearch) => void
  onRawSearchChange?: (raw: Record<string, unknown>) => void
}) {
  const [search, setSearch] = useState<RolesSearch>(() =>
    rolesSearchSchema.parse(initialSearch ?? {})
  )
  const [sheet, setSheet] = useState<{
    open: boolean
    mode: RoleSheetMode
    role: RoleItem | null
  }>({ open: false, mode: 'create', role: null })
  const [deleting, setDeleting] = useState<RoleItem | null>(null)

  const navigate = (opts: {
    search:
      | true
      | Record<string, unknown>
      | ((prev: Record<string, unknown>) => Record<string, unknown>)
  }) => {
    if (typeof opts.search === 'function') {
      setSearch((prev) => {
        const raw = (
          opts.search as (p: Record<string, unknown>) => Record<string, unknown>
        )(prev as unknown as Record<string, unknown>)
        onRawSearchChange?.(raw)
        const parsed = rolesSearchSchema.parse(raw)
        onSearchChange?.(parsed)
        return parsed
      })
    } else if (opts.search !== true) {
      onRawSearchChange?.(opts.search)
      const parsed = rolesSearchSchema.parse(opts.search)
      onSearchChange?.(parsed)
      setSearch(parsed)
    }
  }

  return (
    <>
      <PermissionGuard allowed={canCreate}>
        <Button
          type='button'
          onClick={() => setSheet({ open: true, mode: 'create', role: null })}
        >
          {i18n.t('roles:createRole')}
        </Button>
      </PermissionGuard>

      <RolesTable
        search={search}
        navigate={navigate}
        canUpdate={canUpdate}
        canDelete={canDelete}
        onViewRole={(role) => setSheet({ open: true, mode: 'view', role })}
        onEditRole={(role) => setSheet({ open: true, mode: 'edit', role })}
        onDeleteRole={setDeleting}
      />

      <RoleFormSheet
        open={sheet.open}
        mode={sheet.mode}
        role={sheet.role}
        canUpdate={canUpdate}
        onOpenChange={(open) => {
          if (!open) setSheet((prev) => ({ ...prev, open: false }))
        }}
      />

      <RoleDeleteDialog
        role={deleting}
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
      />

      <Toaster duration={5000} />
    </>
  )
}

function renderRoles(
  props?: {
    initialSearch?: Partial<RolesSearch>
    canCreate?: boolean
    canUpdate?: boolean
    canDelete?: boolean
    onSearchChange?: (next: RolesSearch) => void
    onRawSearchChange?: (raw: Record<string, unknown>) => void
  },
  queryClient = createTestClient()
) {
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <RolesHarness {...props} />
      </I18nextProvider>
    </QueryClientProvider>
  )
}

function currentCatalog() {
  return i18n.language === 'en-US' ? catalogEn : catalogZh
}

function currentMenuTree() {
  return i18n.language === 'en-US' ? menuTreeEn : menuTreeZh
}

function mockStandardGet(roles: RoleItem[] = sampleRoles) {
  getMock.mockImplementation((url: string) => {
    if (url === '/permissions') {
      return Promise.resolve({ data: currentCatalog() })
    }
    if (url === '/menus/tree') {
      return Promise.resolve({ data: currentMenuTree() })
    }
    const page: RolesPage = {
      items: roles,
      total: roles.length,
      page: 1,
      pageSize: 20,
    }
    return Promise.resolve({ data: page })
  })
}

describe('roles feature', () => {
  afterEach(async () => {
    getMock.mockReset()
    postMock.mockReset()
    patchMock.mockReset()
    deleteMock.mockReset()
    useAuthStore.getState().clearSession({ broadcast: false })
    await changeAppLanguage('zh-CN')
  })

  it('normalizes search schema including max q length and serializes query params cleanly', () => {
    const parsed = rolesSearchSchema.parse({
      page: '0',
      pageSize: '999',
      q: '  ops  ',
      type: 'custom,invalid',
      sort: 'code',
      order: 'asc',
    })
    expect(parsed.page).toBe(1)
    expect(parsed.pageSize).toBe(20)
    expect(parsed.q).toBe('ops')
    expect(parsed.type).toEqual(['custom'])

    const overlong = rolesSearchSchema.parse({ q: 'a'.repeat(101) })
    expect(overlong.q).toBeUndefined()

    const sp = buildRolesQueryParams({
      page: 2,
      pageSize: 50,
      q: 'ops',
      isSystem: false,
      sort: 'code',
      order: 'asc',
    })
    expect(sp.get('page')).toBe('2')
    expect(sp.get('pageSize')).toBe('50')
    expect(sp.get('q')).toBe('ops')
    expect(sp.get('isSystem')).toBe('false')
    expect(sp.get('sort')).toBe('code')
    expect(sp.get('order')).toBe('asc')
  })

  it('respects fine-grained permissions and shows viewReadOnlyDesc when update is absent', async () => {
    mockStandardGet()
    const { getByRole, getByText } = await renderRoles({
      canCreate: false,
      canUpdate: false,
      canDelete: false,
    })

    await expect.element(getByText('Operations Editor')).toBeVisible()
    await expect
      .element(getByRole('button', { name: i18n.t('roles:createRole') }))
      .not.toBeInTheDocument()

    const menus = getByRole('button', {
      name: i18n.t('roles:rowActions.openMenu'),
    })
    await userEvent.click(menus.nth(1))

    await expect
      .element(getByRole('menuitem', { name: i18n.t('roles:rowActions.view') }))
      .toBeVisible()
    await expect
      .element(getByRole('menuitem', { name: i18n.t('roles:rowActions.edit') }))
      .not.toBeInTheDocument()
    await expect
      .element(
        getByRole('menuitem', { name: i18n.t('roles:rowActions.delete') })
      )
      .not.toBeInTheDocument()

    await userEvent.click(
      getByRole('menuitem', { name: i18n.t('roles:rowActions.view') })
    )
    await expect
      .element(getByText(i18n.t('roles:sheet.viewReadOnlyDesc')))
      .toBeVisible()
  })

  it('builds hierarchical permission tree with independent menu read leaf, pruning, and unbound permissions', () => {
    const tree = buildPermissionTree(
      menuTreeZh.items as RoleMenuTreeNode[],
      catalogZh.groups,
      i18n.t('roles:sheet.otherPermissions')
    )

    expect(tree.some((n) => n.id === 'dir-general')).toBe(true)
    const dirSystem = tree.find((n) => n.id === 'dir-system')
    expect(dirSystem).toBeDefined()
    expect(dirSystem?.children.some((n) => n.id === 'menu-settings')).toBe(
      false
    )

    const menuUsers = dirSystem?.children.find((n) => n.id === 'menu-users')
    expect(menuUsers?.permissionCode).toBeUndefined()
    expect(menuUsers?.children.map((c) => c.permissionCode)).toEqual([
      'users:read',
      'users:create',
      'users:delete',
    ])
    expect(menuUsers?.children[0]?.label).toBe('查看用户')

    const unboundGroup = tree.find((n) => n.id === 'group:__unbound__')
    expect(unboundGroup?.label).toBe(i18n.t('roles:sheet.otherPermissions'))
    expect(unboundGroup?.children.map((c) => c.permissionCode)).toEqual([
      'audit:export',
    ])
  })

  it('creates a custom role via hierarchical permission tree with independent read selection and expand/collapse', async () => {
    mockStandardGet()
    postMock.mockResolvedValueOnce({
      data: {
        ...sampleRoles[1],
        id: '01930000-0000-7000-8000-000000000099',
        code: 'content_mgr',
        name: 'Content Manager',
        permissions: ['users:read'],
      },
    })

    const { getByRole, getByLabelText, getByText } = await renderRoles()
    await expect.element(getByText('Operations Editor')).toBeVisible()

    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:createRole') })
    )
    await expect.element(getByText('系统管理')).toBeVisible()
    await expect.element(getByText('查看用户', { exact: true })).toBeVisible()
    await expect
      .element(getByText(i18n.t('roles:sheet.otherPermissions')))
      .toBeVisible()

    // Collapse and re-expand a tree branch while asserting aria-expanded state.
    const collapseSystemBtn = getByRole('button', {
      name: i18n.t('roles:sheet.collapseNode', { label: '系统管理' }),
    })
    await expect
      .element(collapseSystemBtn)
      .toHaveAttribute('aria-expanded', 'true')
    await userEvent.click(collapseSystemBtn)
    await expect
      .element(getByText('查看用户', { exact: true }))
      .not.toBeInTheDocument()

    const expandSystemBtn = getByRole('button', {
      name: i18n.t('roles:sheet.expandNode', { label: '系统管理' }),
    })
    await expect
      .element(expandSystemBtn)
      .toHaveAttribute('aria-expanded', 'false')
    await userEvent.click(expandSystemBtn)
    await expect.element(getByText('查看用户', { exact: true })).toBeVisible()

    await userEvent.fill(
      getByLabelText(i18n.t('roles:sheet.codeLabel'), { exact: true }),
      'content_mgr'
    )
    await userEvent.fill(
      getByLabelText(i18n.t('roles:sheet.nameLabel'), { exact: true }),
      'Content Manager'
    )
    await userEvent.fill(
      getByLabelText(i18n.t('roles:sheet.descriptionLabel'), { exact: true }),
      'Read-only user directory access'
    )

    // Toggle only `users:read` independently; parent `用户管理` must become indeterminate.
    await userEvent.click(getByRole('checkbox', { name: '查看用户' }))
    await expect
      .element(getByRole('checkbox', { name: '用户管理' }))
      .toHaveAttribute('aria-checked', 'mixed')
    await expect
      .element(getByRole('checkbox', { name: '创建用户' }))
      .not.toBeChecked()
    await expect
      .element(
        getByText(
          i18n.t('roles:sheet.selectedSummary', { selected: 1, total: 5 })
        )
      )
      .toBeVisible()

    // Parent checkbox selects all children under `用户管理`, and clicking again clears them.
    await userEvent.click(getByRole('checkbox', { name: '用户管理' }))
    await expect
      .element(
        getByText(
          i18n.t('roles:sheet.selectedSummary', { selected: 3, total: 5 })
        )
      )
      .toBeVisible()
    await userEvent.click(getByRole('checkbox', { name: '用户管理' }))
    await expect
      .element(
        getByText(
          i18n.t('roles:sheet.selectedSummary', { selected: 0, total: 5 })
        )
      )
      .toBeVisible()

    // Re-select solely `users:read` via its label and submit.
    await userEvent.click(getByText('查看用户', { exact: true }))
    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:sheet.saveCreate') })
    )

    await vi.waitFor(() => {
      expect(postMock).toHaveBeenCalledTimes(1)
    })
    const [url, payload] = postMock.mock.calls[0] as [
      string,
      {
        code: string
        name: string
        description: string
        permissions: string[]
      },
    ]
    expect(url).toBe('/roles')
    expect(payload.code).toBe('content_mgr')
    expect(payload.name).toBe('Content Manager')
    expect(payload.description).toBe('Read-only user directory access')
    expect(payload.permissions).toEqual(['users:read'])
  })

  it('validates create role form inputs client-side before sending requests', async () => {
    mockStandardGet()
    const { getByRole, getByLabelText, getByText } = await renderRoles()
    await expect.element(getByText('Operations Editor')).toBeVisible()

    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:createRole') })
    )
    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:sheet.saveCreate') })
    )

    await expect
      .element(getByText(i18n.t('validation:required')).first())
      .toBeVisible()
    expect(postMock).not.toHaveBeenCalled()

    await userEvent.fill(
      getByLabelText(i18n.t('roles:sheet.codeLabel'), { exact: true }),
      '9_bad_code'
    )
    await userEvent.fill(
      getByLabelText(i18n.t('roles:sheet.nameLabel'), { exact: true }),
      'Valid Name'
    )
    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:sheet.saveCreate') })
    )

    await expect
      .element(getByText(i18n.t('validation:roleCodeFormat')))
      .toBeVisible()
    expect(postMock).not.toHaveBeenCalled()
  })

  it('shows persistent conflict alert banner on 409 and clears it on edit', async () => {
    mockStandardGet()
    postMock.mockRejectedValueOnce(
      new ApiError({
        status: 409,
        code: 'ROLES_CODE_CONFLICT',
        message: 'Role code already exists',
      })
    )

    const { getByRole, getByLabelText, getByText } = await renderRoles()
    await expect.element(getByText('Operations Editor')).toBeVisible()

    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:createRole') })
    )
    await userEvent.fill(
      getByLabelText(i18n.t('roles:sheet.codeLabel'), { exact: true }),
      'ops_editor'
    )
    await userEvent.fill(
      getByLabelText(i18n.t('roles:sheet.nameLabel'), { exact: true }),
      'Duplicate Role'
    )
    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:sheet.saveCreate') })
    )

    const alert = getByRole('alert')
    await expect.element(alert).toBeVisible()
    await expect.element(alert).toHaveTextContent('Role code already exists')

    await userEvent.fill(
      getByLabelText(i18n.t('roles:sheet.codeLabel'), { exact: true }),
      'ops_editor_2'
    )
    await expect.element(getByRole('alert')).not.toBeInTheDocument()
  })

  it('maps 422 field errors to controls and formats 5xx errors with requestId in sheet banner', async () => {
    mockStandardGet()
    postMock
      .mockRejectedValueOnce(
        new ApiError({
          status: 422,
          code: 'VALIDATION_FAILED',
          message: 'Validation failed',
          fieldErrors: {
            code: 'Code already taken',
            permissions: 'Unknown permission code',
          },
        })
      )
      .mockRejectedValueOnce(
        new ApiError({
          status: 500,
          code: 'INTERNAL_ERROR',
          message: 'raw internal error',
          requestId: 'req-sheet-500',
        })
      )

    const { getByRole, getByLabelText, getByText } = await renderRoles()
    await expect.element(getByText('Operations Editor')).toBeVisible()

    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:createRole') })
    )
    await userEvent.fill(
      getByLabelText(i18n.t('roles:sheet.codeLabel'), { exact: true }),
      'ops_editor'
    )
    await userEvent.fill(
      getByLabelText(i18n.t('roles:sheet.nameLabel'), { exact: true }),
      'Duplicate Role'
    )
    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:sheet.saveCreate') })
    )

    await expect.element(getByText('Code already taken')).toBeVisible()
    await expect.element(getByText('Unknown permission code')).toBeVisible()
    await expect.element(getByRole('alert')).not.toBeInTheDocument()

    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:sheet.saveCreate') })
    )
    await expect
      .element(
        getByText(i18n.t('errors:serverError', { requestId: 'req-sheet-500' }))
      )
      .toBeVisible()
  })

  it('edits custom role, syncs active session when holding edited role, and invalidates cross-module caches', async () => {
    useAuthStore.getState().setSession({
      accessToken: 'token-123',
      user: {
        id: 'u-1',
        username: 'ops',
        email: 'ops@example.com',
        displayName: 'Ops User',
      },
      roles: ['ops_editor'],
      permissions: ['roles:read', 'roles:update', 'dashboard:read'],
    })

    const queryClient = createTestClient()
    queryClient.setQueryData(['users', 'list', 'zh-CN', { page: 1 }], {
      items: [],
      total: 0,
      page: 1,
      pageSize: 20,
    })
    queryClient.setQueryData(['menus', 'user', 'zh-CN'], { items: [] })

    getMock.mockImplementation((url: string) => {
      if (url === '/permissions') {
        return Promise.resolve({ data: currentCatalog() })
      }
      if (url === '/menus/tree') {
        return Promise.resolve({ data: currentMenuTree() })
      }
      if (url === '/auth/me') {
        return Promise.resolve({
          data: {
            user: useAuthStore.getState().user,
            roles: ['ops_editor'],
            permissions: [
              'roles:read',
              'roles:update',
              'dashboard:read',
              'users:read',
            ],
          },
        })
      }
      return Promise.resolve({
        data: {
          items: sampleRoles,
          total: sampleRoles.length,
          page: 1,
          pageSize: 20,
        } satisfies RolesPage,
      })
    })

    patchMock.mockResolvedValueOnce({
      data: {
        ...sampleRoles[1],
        name: 'Senior Operations Editor',
        permissions: ['dashboard:read', 'users:read'],
      },
    })

    const { getByRole, getByLabelText, getByText } = await renderRoles(
      undefined,
      queryClient
    )
    await expect.element(getByText('Operations Editor')).toBeVisible()

    const menus = getByRole('button', {
      name: i18n.t('roles:rowActions.openMenu'),
    })
    await userEvent.click(menus.nth(1))
    await userEvent.click(
      getByRole('menuitem', { name: i18n.t('roles:rowActions.edit') })
    )

    const codeInput = getByLabelText(i18n.t('roles:sheet.codeLabel'), {
      exact: true,
    })
    await expect.element(codeInput).toBeDisabled()

    const nameInput = getByLabelText(i18n.t('roles:sheet.nameLabel'), {
      exact: true,
    })
    await userEvent.clear(nameInput)
    await userEvent.fill(nameInput, 'Senior Operations Editor')
    await userEvent.click(getByText('查看用户', { exact: true }))
    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:sheet.saveEdit') })
    )

    await vi.waitFor(() => {
      expect(patchMock).toHaveBeenCalledTimes(1)
    })
    const [url, payload] = patchMock.mock.calls[0] as [
      string,
      { name: string; permissions: string[] },
    ]
    expect(url).toBe(`/roles/${sampleRoles[1].id}`)
    expect(payload.name).toBe('Senior Operations Editor')
    expect([...payload.permissions].sort()).toEqual([
      'dashboard:read',
      'users:read',
    ])

    await vi.waitFor(() => {
      expect(useAuthStore.getState().permissions).toContain('users:read')
    })
    expect(
      queryClient.getQueryState(['users', 'list', 'zh-CN', { page: 1 }])
        ?.isInvalidated
    ).toBe(true)
    expect(
      queryClient.getQueryState(['menus', 'user', 'zh-CN'])?.isInvalidated
    ).toBe(true)
  })

  it('opens system role read-only, isolates row-action keyboard events from row view, and shows tooltip on disabled delete', async () => {
    mockStandardGet()
    const { getByRole, getByText } = await renderRoles()
    await expect.element(getByText('Super Admin')).toBeVisible()

    await userEvent.click(getByText('Super Admin'))
    await expect
      .element(getByText(i18n.t('roles:sheet.viewSystemDesc')))
      .toBeVisible()
    await expect
      .element(getByRole('button', { name: i18n.t('roles:sheet.saveEdit') }))
      .not.toBeInTheDocument()
    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:sheet.close') })
    )
    await expect.element(getByRole('dialog')).not.toBeInTheDocument()

    // Keyboard activation inside row action menu must NOT bubble to TableRow onKeyDown -> onViewRole.
    const menus = getByRole('button', {
      name: i18n.t('roles:rowActions.openMenu'),
    })
    await expect.element(menus.nth(1)).toBeVisible()
    const opsTrigger = menus.nth(1).element() as HTMLElement
    opsTrigger.focus()
    await userEvent.keyboard('{Enter}')
    const editItem = getByRole('menuitem', {
      name: i18n.t('roles:rowActions.edit'),
    })
    await expect.element(editItem).toBeVisible()
    ;(editItem.element() as HTMLElement).focus()
    await userEvent.keyboard('{Enter}')

    await expect
      .element(getByText(i18n.t('roles:sheet.editTitle')))
      .toBeVisible()
    await expect
      .element(getByText(i18n.t('roles:sheet.viewTitle')))
      .not.toBeInTheDocument()
    await userEvent.click(
      getByRole('button', { name: i18n.t('common:cancel') })
    )
    await expect.element(getByRole('dialog')).not.toBeInTheDocument()

    // Bound custom role disables delete with tooltip hint.
    await userEvent.click(menus.nth(2))
    const deleteItem = getByRole('menuitem', {
      name: i18n.t('roles:rowActions.delete'),
    })
    await expect.element(deleteItem).toHaveAttribute('aria-disabled', 'true')
    const deleteTrigger = deleteItem.element().parentElement
    expect(deleteTrigger).not.toBeNull()
    await userEvent.hover(deleteTrigger as HTMLElement)
    await expect
      .element(
        getByRole('tooltip', {
          name: i18n.t('roles:rowActions.deleteDisabledInUse'),
        })
      )
      .toBeVisible()
  })

  it('keeps delete dialog open on 409 conflict and shrinks page when last item on page > 1 is deleted', async () => {
    let deleted = false
    getMock.mockImplementation((url: string, opts?: { params?: unknown }) => {
      if (url === '/permissions') {
        return Promise.resolve({ data: currentCatalog() })
      }
      if (url === '/menus/tree') {
        return Promise.resolve({ data: currentMenuTree() })
      }
      const reqPage = Number(
        (opts?.params as URLSearchParams | undefined)?.get('page') ?? '1'
      )
      if (!deleted) {
        return Promise.resolve({
          data: {
            items: reqPage === 2 ? [sampleRoles[1]] : sampleRoles,
            total: 21,
            page: reqPage,
            pageSize: 20,
          } satisfies RolesPage,
        })
      }
      return Promise.resolve({
        data: {
          items: reqPage === 1 ? sampleRoles : [],
          total: 20,
          page: reqPage,
          pageSize: 20,
        } satisfies RolesPage,
      })
    })

    deleteMock
      .mockRejectedValueOnce(
        new ApiError({
          status: 409,
          code: 'ROLES_IN_USE',
          message: 'Role is assigned to users',
        })
      )
      .mockImplementationOnce(() => {
        deleted = true
        return Promise.resolve(undefined)
      })

    const searchSnapshots: RolesSearch[] = []
    const { getByRole, getByText } = await renderRoles({
      initialSearch: { page: 2, pageSize: 20 },
      onSearchChange: (s) => searchSnapshots.push(s),
    })

    await expect.element(getByText('Operations Editor')).toBeVisible()

    const menus = getByRole('button', {
      name: i18n.t('roles:rowActions.openMenu'),
    })
    await userEvent.click(menus.first())
    await userEvent.click(
      getByRole('menuitem', { name: i18n.t('roles:rowActions.delete') })
    )

    const confirmBtn = getByRole('button', {
      name: i18n.t('roles:deleteDialog.confirm'),
    })
    await expect.element(confirmBtn).toBeVisible()

    // First delete hits 409: dialog remains open and toast displays error message.
    await userEvent.click(confirmBtn)
    await expect.element(getByText('Role is assigned to users')).toBeVisible()
    await expect.element(confirmBtn).toBeVisible()

    // Second delete succeeds and triggers ensurePageInRange(1).
    await userEvent.click(confirmBtn)
    await vi.waitFor(() => {
      expect(deleteMock).toHaveBeenCalledTimes(2)
    })
    await vi.waitFor(() => {
      expect(searchSnapshots.some((s) => s.page === 1)).toBe(true)
    })
  })

  it('refetches and updates permission matrix copy when switching locale', async () => {
    mockStandardGet()
    const { getByRole, getByText } = await renderRoles()
    await expect.element(getByText('Operations Editor')).toBeVisible()

    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:createRole') })
    )
    await expect.element(getByText('仪表盘', { exact: true })).toBeVisible()
    await expect.element(getByText('查看用户', { exact: true })).toBeVisible()

    await changeAppLanguage('en-US')
    await expect.element(getByText('Dashboard', { exact: true })).toBeVisible()
    await expect.element(getByText('View Users', { exact: true })).toBeVisible()
  })

  it('retries failed permissions catalog load and falls back cleanly when menu tree fails', async () => {
    await changeAppLanguage('en-US')
    let catalogAttempt = 0
    getMock.mockImplementation((url: string) => {
      if (url === '/permissions') {
        catalogAttempt += 1
        if (catalogAttempt === 1) {
          return Promise.reject(
            new ApiError({
              status: 500,
              code: 'INTERNAL_ERROR',
              message: 'Catalog failed',
            })
          )
        }
        return Promise.resolve({ data: catalogEn })
      }
      if (url === '/menus/tree') {
        return Promise.reject(
          new ApiError({
            status: 500,
            code: 'INTERNAL_ERROR',
            message: 'Menu tree failed',
          })
        )
      }
      return Promise.resolve({
        data: {
          items: sampleRoles,
          total: sampleRoles.length,
          page: 1,
          pageSize: 20,
        } satisfies RolesPage,
      })
    })

    const { getByRole, getByText } = await renderRoles()
    await expect.element(getByText('Operations Editor')).toBeVisible()

    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:createRole') })
    )
    await expect
      .element(getByText(i18n.t('roles:sheet.permissionsLoadFailed')))
      .toBeVisible()

    await userEvent.click(getByRole('button', { name: i18n.t('common:retry') }))
    // Falls back to flat catalog groups when /menus/tree errors.
    await expect.element(getByText('View Dashboard')).toBeVisible()
    await expect.element(getByText('Export Audit Logs')).toBeVisible()
  })

  it('renders inline list error without empty row, recovers on retry, and syncs search, filter, sort, and reset', async () => {
    await changeAppLanguage('en-US')
    let listAttempt = 0
    getMock.mockImplementation((url: string, opts?: { params?: unknown }) => {
      if (url === '/permissions') {
        return Promise.resolve({ data: catalogEn })
      }
      if (url === '/menus/tree') {
        return Promise.resolve({ data: menuTreeEn })
      }
      listAttempt += 1
      if (listAttempt === 1) {
        return Promise.reject(
          new ApiError({
            status: 500,
            code: 'INTERNAL_ERROR',
            message: 'raw db failure',
            requestId: 'req-roles-500',
          })
        )
      }
      const sp = opts?.params as URLSearchParams | undefined
      if (sp?.get('q') === 'no-match') {
        return Promise.resolve({
          data: {
            items: [],
            total: 0,
            page: 1,
            pageSize: 20,
          } satisfies RolesPage,
        })
      }
      return Promise.resolve({
        data: {
          items: sampleRoles,
          total: sampleRoles.length,
          page: 1,
          pageSize: 20,
        } satisfies RolesPage,
      })
    })

    const searchSnapshots: RolesSearch[] = []
    const rawSnapshots: Record<string, unknown>[] = []
    const { getByRole, getByText, getByPlaceholder } = await renderRoles({
      initialSearch: { page: 2 },
      onSearchChange: (s) => searchSnapshots.push(s),
      onRawSearchChange: (raw) => rawSnapshots.push(raw),
    })

    const alert = getByRole('alert')
    await expect.element(alert).toBeVisible()
    await expect
      .element(getByText(i18n.t('roles:error.loadFailed')))
      .toBeVisible()
    await expect
      .element(
        getByText(i18n.t('errors:serverError', { requestId: 'req-roles-500' }))
      )
      .toBeVisible()
    // Empty state must not render alongside an error alert.
    await expect
      .element(getByText(i18n.t('roles:empty.noDataTitle')))
      .not.toBeInTheDocument()
    await expect
      .element(getByRole('button', { name: i18n.t('common:refresh') }))
      .toBeVisible()

    await userEvent.click(
      alert.getByRole('button', { name: i18n.t('common:retry') })
    )
    await expect.element(getByText('Operations Editor')).toBeVisible()
    await expect.element(alert).not.toBeInTheDocument()

    // Search input caps at 100 chars and resets page to 1.
    const searchInput = getByPlaceholder(i18n.t('roles:searchPlaceholder'))
    await expect.element(searchInput).toHaveAttribute('maxlength', '100')
    await userEvent.fill(searchInput, 'no-match')
    await expect
      .element(getByText(i18n.t('roles:empty.noMatchTitle')))
      .toBeVisible()
    expect(
      searchSnapshots.some((s) => s.q === 'no-match' && s.page === 1)
    ).toBe(true)

    // Clear filters removes URL search params cleanly.
    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:empty.clearFilters') })
    )
    await expect.element(getByText('Operations Editor')).toBeVisible()
    const resetRaw = rawSnapshots[rawSnapshots.length - 1]
    expect(resetRaw.q).toBeUndefined()
    expect(resetRaw.page).toBeUndefined()

    // Faceted type filter and column sorting sync into search state and aria-sort.
    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:filters.type') })
    )
    await userEvent.click(
      getByText(i18n.t('roles:filters.custom'), { exact: true })
    )
    await vi.waitFor(() => {
      expect(searchSnapshots.some((s) => s.type?.includes('custom'))).toBe(true)
    })

    await userEvent.click(
      getByRole('button', { name: i18n.t('roles:columns.code') })
    )
    await userEvent.click(
      getByRole('menuitem', { name: i18n.t('common:table.asc') })
    )
    await vi.waitFor(() => {
      expect(
        searchSnapshots.some((s) => s.sort === 'code' && s.order === 'asc')
      ).toBe(true)
    })
    const codeHeaderCell = getByRole('button', {
      name: i18n.t('roles:columns.code'),
    })
      .element()
      .closest('th')
    expect(codeHeaderCell?.getAttribute('aria-sort')).toBe('ascending')
  })

  it('shows the empty-data state when no role matches and no filter is active', async () => {
    getMock.mockImplementation((url: string) => {
      if (url === '/permissions') {
        return Promise.resolve({ data: currentCatalog() })
      }
      if (url === '/menus/tree') {
        return Promise.resolve({ data: currentMenuTree() })
      }
      return Promise.resolve({
        data: {
          items: [],
          total: 0,
          page: 1,
          pageSize: 20,
        } satisfies RolesPage,
      })
    })

    const { getByText, container } = await renderRoles()
    await expect
      .element(getByText(i18n.t('roles:empty.noDataTitle')))
      .toBeVisible()
    expect(
      container.textContent?.includes(i18n.t('roles:empty.noMatchTitle'))
    ).toBe(false)
  })
})

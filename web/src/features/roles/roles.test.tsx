import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/styles/index.css'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
import i18n, { changeAppLanguage } from '@/i18n/index'
import { ApiError } from '@/lib/api/error'
import { Button } from '@/components/ui/button'
import { PermissionGuard } from '@/components/shared/permission-guard'
import { buildRolesQueryParams } from './api'
import { buildPermissionTree } from './components/permission-tree-builder'
import { RoleDeleteDialog } from './components/role-delete-dialog'
import { RoleFormSheet, type RoleSheetMode } from './components/role-form-sheet'
import { RolesTable } from './components/roles-table'
import { rolesSearchSchema, type RolesSearch } from './schema'
import type { PermissionsCatalogResponse, RoleItem, RolesPage } from './types'

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
          code: 'users:delete',
          module: 'identity',
          name: '删除用户',
          description: '软删除用户账号',
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
          code: 'users:delete',
          module: 'identity',
          name: 'Delete Users',
          description: 'Soft-delete user accounts',
        },
      ],
    },
  ],
}

const sampleRoles: RoleItem[] = [
  {
    id: '01930000-0000-7000-8000-000000000001',
    code: 'super_admin',
    name: '超级管理员',
    description: '系统内置最高权限角色',
    isSystem: true,
    userCount: 1,
    permissionCount: 3,
    permissions: ['dashboard:read', 'users:read', 'users:delete'],
    createdBy: null,
    updatedBy: null,
    createdAt: '2026-11-26T08:00:00Z',
    updatedAt: '2026-11-26T08:00:00Z',
  },
  {
    id: '01930000-0000-7000-8000-000000000002',
    code: 'ops_editor',
    name: '运营编辑',
    description: '日常运营只读与查看',
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
    name: '已绑定审计员',
    description: '已分配用户的自定义角色',
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

function RolesHarness({
  initialSearch,
  canCreate = true,
  canUpdate = true,
  canDelete = true,
}: {
  initialSearch?: Partial<RolesSearch>
  canCreate?: boolean
  canUpdate?: boolean
  canDelete?: boolean
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
      setSearch((prev) =>
        rolesSearchSchema.parse(
          (
            opts.search as (
              p: Record<string, unknown>
            ) => Record<string, unknown>
          )(prev as unknown as Record<string, unknown>)
        )
      )
    } else if (opts.search !== true) {
      setSearch(rolesSearchSchema.parse(opts.search))
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
        onOpenChange={(open) => {
          if (!open) setSheet((prev) => ({ ...prev, open: false, role: null }))
        }}
      />

      <RoleDeleteDialog
        role={deleting}
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
      />
    </>
  )
}

function renderRoles(props?: {
  initialSearch?: Partial<RolesSearch>
  canCreate?: boolean
  canUpdate?: boolean
  canDelete?: boolean
}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <RolesHarness {...props} />
      </I18nextProvider>
    </QueryClientProvider>
  )
}

function mockStandardGet(roles: RoleItem[] = sampleRoles) {
  getMock.mockImplementation((url: string) => {
    if (url === '/permissions') {
      const data = i18n.language === 'en-US' ? catalogEn : catalogZh
      return Promise.resolve({ data })
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
    await changeAppLanguage('zh-CN')
  })

  it('normalizes search schema and serializes query params cleanly', () => {
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

  it('respects fine-grained create/update/delete permissions in toolbar and row menus', async () => {
    mockStandardGet()
    const { getByRole, getByText } = await renderRoles({
      canCreate: false,
      canUpdate: false,
      canDelete: false,
    })

    await expect.element(getByText('运营编辑')).toBeVisible()
    await expect
      .element(getByRole('button', { name: '新增角色' }))
      .not.toBeInTheDocument()

    const menus = getByRole('button', { name: '打开操作菜单' })
    await userEvent.click(menus.nth(1))

    await expect
      .element(getByRole('menuitem', { name: '查看权限' }))
      .toBeVisible()
    await expect
      .element(getByRole('menuitem', { name: '编辑角色' }))
      .not.toBeInTheDocument()
    await expect
      .element(getByRole('menuitem', { name: '删除角色' }))
      .not.toBeInTheDocument()
  })

  it('creates a custom role via permission matrix sheet and submits selected permissions', async () => {
    mockStandardGet()
    postMock.mockResolvedValueOnce({
      data: {
        ...sampleRoles[1],
        id: '01930000-0000-7000-8000-000000000099',
        code: 'content_mgr',
        name: '内容经理',
        permissions: ['users:read', 'users:delete'],
      },
    })

    const { getByRole, getByLabelText, getByText } = await renderRoles()
    await expect.element(getByText('运营编辑')).toBeVisible()

    await userEvent.click(getByRole('button', { name: '新增角色' }))
    await expect.element(getByText('用户管理')).toBeVisible()

    await userEvent.fill(
      getByLabelText('角色编码', { exact: true }),
      'content_mgr'
    )
    await userEvent.fill(
      getByLabelText('角色名称', { exact: true }),
      '内容经理'
    )
    await userEvent.fill(
      getByLabelText('角色描述', { exact: true }),
      '管理用户目录'
    )

    // Click module-level checkbox to select all permissions under identity.
    await userEvent.click(getByRole('checkbox', { name: '用户管理' }))
    await expect.element(getByText('已选 2 / 3 项')).toBeVisible()

    await userEvent.click(getByRole('button', { name: '创建角色' }))

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
    expect(payload.name).toBe('内容经理')
    expect(payload.description).toBe('管理用户目录')
    expect([...payload.permissions].sort()).toEqual([
      'users:delete',
      'users:read',
    ])
  })

  it('shows persistent conflict alert banner on 409 and clears it on edit', async () => {
    mockStandardGet()
    postMock.mockRejectedValueOnce(
      new ApiError({
        status: 409,
        code: 'ROLES_CODE_CONFLICT',
        message: '角色编码已存在',
      })
    )

    const { getByRole, getByLabelText, getByText } = await renderRoles()
    await expect.element(getByText('运营编辑')).toBeVisible()

    await userEvent.click(getByRole('button', { name: '新增角色' }))
    await userEvent.fill(
      getByLabelText('角色编码', { exact: true }),
      'ops_editor'
    )
    await userEvent.fill(
      getByLabelText('角色名称', { exact: true }),
      '重复角色'
    )
    await userEvent.click(getByRole('button', { name: '创建角色' }))

    const alert = getByRole('alert')
    await expect.element(alert).toBeVisible()
    await expect.element(alert).toHaveTextContent('角色编码已存在')

    // Editing a field clears the stale error banner.
    await userEvent.fill(
      getByLabelText('角色编码', { exact: true }),
      'ops_editor_2'
    )
    await expect.element(getByRole('alert')).not.toBeInTheDocument()
  })

  it('edits custom role metadata and permissions while keeping code disabled', async () => {
    mockStandardGet()
    patchMock.mockResolvedValueOnce({
      data: {
        ...sampleRoles[1],
        name: '高级运营编辑',
        permissions: ['dashboard:read', 'users:read'],
      },
    })

    const { getByRole, getByLabelText, getByText } = await renderRoles()
    await expect.element(getByText('运营编辑')).toBeVisible()

    const menus = getByRole('button', { name: '打开操作菜单' })
    await userEvent.click(menus.nth(1))
    await userEvent.click(getByRole('menuitem', { name: '编辑角色' }))

    const codeInput = getByLabelText('角色编码', { exact: true })
    await expect.element(codeInput).toBeDisabled()

    await userEvent.clear(getByLabelText('角色名称', { exact: true }))
    await userEvent.fill(
      getByLabelText('角色名称', { exact: true }),
      '高级运营编辑'
    )
    await userEvent.click(getByText('查看用户', { exact: true }))
    await userEvent.click(getByRole('button', { name: '保存更改' }))

    await vi.waitFor(() => {
      expect(patchMock).toHaveBeenCalledTimes(1)
    })
    const [url, payload] = patchMock.mock.calls[0] as [
      string,
      { name: string; permissions: string[] },
    ]
    expect(url).toBe(`/roles/${sampleRoles[1].id}`)
    expect(payload.name).toBe('高级运营编辑')
    expect([...payload.permissions].sort()).toEqual([
      'dashboard:read',
      'users:read',
    ])
  })

  it('opens system role in read-only mode and disables delete for bound roles', async () => {
    mockStandardGet()
    const { getByRole, getByText } = await renderRoles()
    await expect.element(getByText('超级管理员')).toBeVisible()

    // Click system role row to open read-only permission sheet.
    await userEvent.click(getByText('超级管理员'))
    await expect.element(getByText('角色权限详情')).toBeVisible()
    await expect
      .element(getByRole('button', { name: '保存更改' }))
      .not.toBeInTheDocument()
    await userEvent.click(getByRole('button', { name: '关闭' }))

    // Bound custom role has delete menu item disabled.
    const menus = getByRole('button', { name: '打开操作菜单' })
    await userEvent.click(menus.nth(2))
    const deleteItem = getByRole('menuitem', { name: '删除角色' })
    await expect.element(deleteItem).toHaveAttribute('aria-disabled', 'true')
  })

  it('deletes an unused custom role via ConfirmDialog', async () => {
    mockStandardGet()
    deleteMock.mockResolvedValueOnce(undefined)

    const { getByRole, getByText } = await renderRoles()
    await expect.element(getByText('运营编辑')).toBeVisible()

    const menus = getByRole('button', { name: '打开操作菜单' })
    await userEvent.click(menus.nth(1))
    await userEvent.click(getByRole('menuitem', { name: '删除角色' }))

    await expect.element(getByText(/确认删除角色「运营编辑」/)).toBeVisible()
    await userEvent.click(getByRole('button', { name: '确认删除' }))

    await vi.waitFor(() => {
      expect(deleteMock).toHaveBeenCalledWith(`/roles/${sampleRoles[1].id}`)
    })
  })

  it('refetches and updates permission matrix copy when switching locale', async () => {
    mockStandardGet()
    const { getByRole, getByText } = await renderRoles()
    await expect.element(getByText('运营编辑')).toBeVisible()

    await userEvent.click(getByRole('button', { name: '新增角色' }))
    await expect.element(getByText('查看仪表盘')).toBeVisible()

    await changeAppLanguage('en-US')
    await expect.element(getByText('View Dashboard')).toBeVisible()
  })

  it('maps 422 field errors back to form fields instead of a global banner', async () => {
    mockStandardGet()
    postMock.mockRejectedValueOnce(
      new ApiError({
        status: 422,
        code: 'VALIDATION_FAILED',
        message: '请求参数校验失败',
        fieldErrors: {
          code: '角色编码已存在',
          permissions: '包含未注册的权限码',
        },
      })
    )

    const { getByRole, getByLabelText, getByText } = await renderRoles()
    await expect.element(getByText('运营编辑')).toBeVisible()

    await userEvent.click(getByRole('button', { name: '新增角色' }))
    await userEvent.fill(
      getByLabelText('角色编码', { exact: true }),
      'ops_editor'
    )
    await userEvent.fill(
      getByLabelText('角色名称', { exact: true }),
      '重复角色'
    )
    await userEvent.click(getByRole('button', { name: '创建角色' }))

    // Field messages render under their own controls; the global banner stays hidden.
    await expect.element(getByText('角色编码已存在')).toBeVisible()
    await expect.element(getByText('包含未注册的权限码')).toBeVisible()
    await expect.element(getByRole('alert')).not.toBeInTheDocument()
  })

  it('renders inline list error with retry and recovers on refetch', async () => {
    mockStandardGet()
    getMock.mockRejectedValueOnce(
      new ApiError({
        status: 500,
        code: 'INTERNAL_ERROR',
        message: '服务器内部错误，请稍后重试',
      })
    )

    const { getByRole, getByText } = await renderRoles()

    const alert = getByRole('alert')
    await expect.element(alert).toBeVisible()
    await expect.element(getByText('加载角色列表失败')).toBeVisible()

    await alert.getByRole('button', { name: '重试' }).click()
    await expect.element(getByText('运营编辑')).toBeVisible()
    await expect.element(alert).not.toBeInTheDocument()
  })

  it('distinguishes empty data from no-match filters and clears filters on request', async () => {
    const emptyPage: RolesPage = { items: [], total: 0, page: 1, pageSize: 20 }
    getMock.mockImplementation((url: string, opts?: { params?: unknown }) => {
      const q = (opts?.params as URLSearchParams | undefined)?.get('q')
      if (url === '/permissions') {
        return Promise.resolve({
          data: i18n.language === 'en-US' ? catalogEn : catalogZh,
        })
      }
      if (q === 'no-match') {
        return Promise.resolve({ data: emptyPage })
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

    const { getByRole, getByText } = await renderRoles({
      initialSearch: { q: 'no-match' },
    })
    await expect.element(getByText('没有符合筛选条件的角色')).toBeVisible()

    await userEvent.click(getByRole('button', { name: '清除筛选' }))
    await expect.element(getByText('运营编辑')).toBeVisible()
  })

  it('shows the empty-data state when no role matches and no filter is active', async () => {
    getMock.mockImplementation((url: string) => {
      if (url === '/permissions') {
        return Promise.resolve({
          data: i18n.language === 'en-US' ? catalogEn : catalogZh,
        })
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
    await expect.element(getByText('暂无角色数据')).toBeVisible()
    // The filtered-empty copy must not leak into the unfiltered empty state.
    expect(container.textContent?.includes('没有符合筛选条件的角色')).toBe(
      false
    )
  })

  it('prunes permissionless menu subtrees like default-accessible Settings from permission tree', () => {
    const tree = buildPermissionTree(
      [
        {
          id: 'dir-general',
          type: 'directory',
          code: 'dir_general',
          title: '通用',
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
          id: 'dir-other',
          type: 'directory',
          code: 'dir_other',
          title: '其他',
          icon: '',
          permissionCode: '',
          children: [
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
      catalogZh.groups,
      '其他权限'
    )

    expect(tree.some((n) => n.id === 'dir-general')).toBe(true)
    expect(tree.some((n) => n.id === 'dir-other')).toBe(false)
  })
})

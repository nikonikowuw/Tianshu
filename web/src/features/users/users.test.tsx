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
import { TooltipProvider } from '@/components/ui/tooltip'
import { PermissionGuard } from '@/components/shared/permission-guard'
import { buildUsersQueryParams } from './api'
import { RoleMultiSelect } from './components/role-multi-select'
import { UserDeleteDialog } from './components/user-delete-dialog'
import { UserFormSheet } from './components/user-form-sheet'
import { UserResetPasswordDialog } from './components/user-reset-password-dialog'
import { UserRowActions } from './components/user-row-actions'
import { UserToggleStatusDialog } from './components/user-toggle-status-dialog'
import { UsersTable } from './components/users-table'
import { usersSearchSchema, type UsersSearch } from './schema'
import type { RoleOptionsResponse, UserItem, UsersPage } from './types'

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

const roleOptionsData: RoleOptionsResponse = {
  items: [
    {
      id: '01950000-0000-7000-8000-000000000001',
      code: 'super_admin',
      name: 'Super Admin',
      isSystem: true,
    },
    {
      id: '01950000-0000-7000-8000-000000000002',
      code: 'viewer',
      name: 'Viewer',
      isSystem: false,
    },
  ],
}

const sampleUsers: UserItem[] = [
  {
    id: '01950000-0000-7000-8000-000000000101',
    username: 'root',
    email: 'root@example.com',
    displayName: 'Root Admin',
    status: 'active',
    lastLoginAt: '2026-11-26T10:00:00Z',
    roles: [
      {
        id: '01950000-0000-7000-8000-000000000001',
        code: 'super_admin',
        name: 'Super Admin',
        isSystem: true,
      },
    ],
    createdBy: null,
    updatedBy: null,
    createdAt: '2026-11-26T00:00:00Z',
    updatedAt: '2026-11-26T00:00:00Z',
  },
  {
    id: '01950000-0000-7000-8000-000000000102',
    username: 'alice',
    email: 'alice@example.com',
    displayName: 'Alice',
    status: 'disabled',
    lastLoginAt: null,
    roles: [
      {
        id: '01950000-0000-7000-8000-000000000002',
        code: 'viewer',
        name: 'Viewer',
        isSystem: false,
      },
    ],
    createdBy: { id: '01950000-0000-7000-8000-000000000101', username: 'root' },
    updatedBy: null,
    createdAt: '2026-11-26T01:00:00Z',
    updatedAt: '2026-11-26T01:00:00Z',
  },
]

function createClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function UsersTableHarness({
  initialSearch = {},
  canUpdate = true,
  canDelete = true,
  onDelete = vi.fn(),
  onSearchChange,
  onRawSearchChange,
}: {
  initialSearch?: Partial<UsersSearch>
  canUpdate?: boolean
  canDelete?: boolean
  onDelete?: (user: UserItem) => void
  onSearchChange?: (next: UsersSearch) => void
  onRawSearchChange?: (raw: Record<string, unknown>) => void
}) {
  const [search, setSearch] = useState<UsersSearch>(() =>
    usersSearchSchema.parse(initialSearch)
  )
  return (
    <UsersTable
      search={search}
      navigate={(opts) => {
        setSearch((prev) => {
          const next =
            typeof opts.search === 'function'
              ? opts.search(prev)
              : opts.search === true
                ? prev
                : opts.search
          onRawSearchChange?.((next ?? {}) as Record<string, unknown>)
          const parsed = usersSearchSchema.parse(next)
          onSearchChange?.(parsed)
          return parsed
        })
      }}
      canUpdate={canUpdate}
      canDelete={canDelete}
      onEditUser={vi.fn()}
      onResetPassword={vi.fn()}
      onToggleStatus={vi.fn()}
      onDeleteUser={onDelete}
    />
  )
}

describe('users feature', () => {
  afterEach(async () => {
    vi.clearAllMocks()
    useAuthStore.getState().clearSession({ broadcast: false })
    await changeAppLanguage('zh-CN')
  })

  it('serializes repeated status and roleId query params and caps q length', () => {
    const sp = buildUsersQueryParams({
      page: 2,
      pageSize: 20,
      q: 'alice',
      status: ['active', 'disabled'],
      roleId: ['r1', 'r2'],
      sort: 'username',
      order: 'asc',
    })
    expect(sp.get('page')).toBe('2')
    expect(sp.get('q')).toBe('alice')
    expect(sp.getAll('status')).toEqual(['active', 'disabled'])
    expect(sp.getAll('roleId')).toEqual(['r1', 'r2'])
    expect(sp.get('sort')).toBe('username')
    expect(sp.get('order')).toBe('asc')

    expect(usersSearchSchema.parse({ q: 'a'.repeat(101) }).q).toBeUndefined()
    expect(usersSearchSchema.parse({ q: 123 }).q).toBe('123')
    expect(usersSearchSchema.parse({ q: true }).q).toBe('true')

    const validRoleIds = Array.from(
      { length: 22 },
      (_, idx) => `01950000-0000-7000-8000-${String(idx + 1).padStart(12, '0')}`
    )
    const parsedRoles = usersSearchSchema.parse({
      roleId: [
        'not-a-uuid',
        '00000000-0000-0000-0000-000000000000',
        validRoleIds[0].toUpperCase(),
        ...validRoleIds,
      ],
    })
    expect(parsedRoles.roleId).toHaveLength(20)
    expect(parsedRoles.roleId?.[0]).toBe(validRoleIds[0])
    expect(parsedRoles.roleId).not.toContain('not-a-uuid')
    expect(parsedRoles.roleId).not.toContain(
      '00000000-0000-0000-0000-000000000000'
    )
  })

  it('renders users table, resets page on search, and distinguishes empty states', async () => {
    await changeAppLanguage('en-US')
    useAuthStore.getState().setSession({
      accessToken: 'tok',
      user: {
        id: '01950000-0000-7000-8000-000000000101',
        username: 'root',
        email: 'root@example.com',
        displayName: 'Root Admin',
      },
      roles: ['super_admin'],
      permissions: [
        'users:read',
        'users:create',
        'users:update',
        'users:delete',
      ],
    })

    getMock.mockImplementation((url: string) => {
      if (url === '/roles/options') {
        return Promise.resolve({ data: roleOptionsData })
      }
      const page: UsersPage = {
        items: sampleUsers,
        total: 40,
        page: 2,
        pageSize: 20,
      }
      return Promise.resolve({ data: page })
    })

    const searchSnapshots: UsersSearch[] = []
    const rawSearchSnapshots: Record<string, unknown>[] = []
    const client = createClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <UsersTableHarness
            initialSearch={{ page: 2 }}
            onSearchChange={(s) => searchSnapshots.push(s)}
            onRawSearchChange={(raw) => rawSearchSnapshots.push(raw)}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect.element(screen.getByText('Root Admin')).toBeVisible()
    await expect.element(screen.getByText('@alice')).toBeVisible()

    const userHeaderCell = screen
      .getByRole('button', { name: i18n.t('users:columns.user') })
      .element()
      .closest('th')
    expect(userHeaderCell?.getAttribute('aria-sort')).toBe('none')

    getMock.mockImplementation((url: string) => {
      if (url === '/roles/options') {
        return Promise.resolve({ data: roleOptionsData })
      }
      return Promise.resolve({
        data: { items: [], total: 0, page: 1, pageSize: 20 },
      })
    })

    const searchInput = screen.getByPlaceholder(
      i18n.t('users:searchPlaceholder')
    )
    await userEvent.fill(searchInput, 'nonexistent')
    await expect
      .element(screen.getByText(i18n.t('users:empty.noResults')))
      .toBeVisible()
    expect(
      searchSnapshots.some((s) => s.q === 'nonexistent' && s.page === 1)
    ).toBe(true)

    const clearFiltersBtn = screen.getByRole('button', {
      name: i18n.t('common:table.clearFilters'),
    })
    await expect.element(clearFiltersBtn).toBeVisible()

    const callsBeforeReset = rawSearchSnapshots.length
    await userEvent.click(clearFiltersBtn)
    expect(rawSearchSnapshots.length).toBe(callsBeforeReset + 1)
    const resetRaw = rawSearchSnapshots[rawSearchSnapshots.length - 1]
    expect(resetRaw.q).toBeUndefined()
    expect(resetRaw.page).toBeUndefined()
  })

  it('shows error alert without empty row on load failure and retries', async () => {
    await changeAppLanguage('en-US')
    getMock.mockImplementation((url: string) => {
      if (url === '/roles/options') {
        return Promise.resolve({ data: roleOptionsData })
      }
      return Promise.reject(
        new ApiError({
          status: 500,
          code: 'INTERNAL_ERROR',
          message: 'Server failed to load users',
          requestId: 'req-users-500',
        })
      )
    })

    const client = createClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <UsersTableHarness />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect
      .element(
        screen.getByText(i18n.t('users:error.loadTitle'), { exact: true })
      )
      .toBeVisible()
    await expect
      .element(
        screen.getByText(
          i18n.t('errors:serverError', { requestId: 'req-users-500' })
        )
      )
      .toBeVisible()
    await expect
      .element(screen.getByText(i18n.t('users:empty.noData')))
      .not.toBeInTheDocument()

    getMock.mockImplementation((url: string) => {
      if (url === '/roles/options') {
        return Promise.resolve({ data: roleOptionsData })
      }
      return Promise.resolve({
        data: { items: sampleUsers, total: 2, page: 1, pageSize: 20 },
      })
    })

    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('users:error.retry') })
    )
    await expect.element(screen.getByText('Root Admin')).toBeVisible()
  })

  it('shows empty.noData when no users exist without active filters', async () => {
    await changeAppLanguage('en-US')
    getMock.mockImplementation((url: string) => {
      if (url === '/roles/options') {
        return Promise.resolve({ data: roleOptionsData })
      }
      return Promise.resolve({
        data: { items: [], total: 0, page: 1, pageSize: 20 },
      })
    })

    const client = createClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <UsersTableHarness />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect
      .element(screen.getByText(i18n.t('users:empty.noData')))
      .toBeVisible()
    await expect
      .element(
        screen.getByRole('button', {
          name: i18n.t('common:table.clearFilters'),
        })
      )
      .not.toBeInTheDocument()
  })

  it('enforces permissions, self-protection, and super_admin protection in row actions', async () => {
    await changeAppLanguage('en-US')
    const client = createClient()

    const noPermScreen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <TooltipProvider>
            <UserRowActions
              user={sampleUsers[1]}
              currentUserId='other-id'
              actorIsSuperAdmin={true}
              canUpdate={false}
              canDelete={false}
              onEdit={vi.fn()}
              onResetPassword={vi.fn()}
              onToggleStatus={vi.fn()}
              onDelete={vi.fn()}
            />
          </TooltipProvider>
        </I18nextProvider>
      </QueryClientProvider>
    )
    await expect
      .element(
        noPermScreen.getByRole('button', {
          name: i18n.t('users:rowActions.openMenu'),
        })
      )
      .not.toBeInTheDocument()
    await noPermScreen.unmount()

    const selfScreen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <TooltipProvider>
            <UserRowActions
              user={sampleUsers[0]}
              currentUserId={sampleUsers[0].id}
              actorIsSuperAdmin={true}
              canUpdate={true}
              canDelete={true}
              onEdit={vi.fn()}
              onResetPassword={vi.fn()}
              onToggleStatus={vi.fn()}
              onDelete={vi.fn()}
            />
          </TooltipProvider>
        </I18nextProvider>
      </QueryClientProvider>
    )
    await userEvent.click(
      selfScreen.getByRole('button', {
        name: i18n.t('users:rowActions.openMenu'),
      })
    )
    await expect
      .element(
        selfScreen.getByRole('menuitem', {
          name: i18n.t('users:rowActions.edit'),
        })
      )
      .toBeEnabled()
    await expect
      .element(
        selfScreen.getByRole('menuitem', {
          name: i18n.t('users:rowActions.disable'),
        })
      )
      .toHaveAttribute('aria-disabled', 'true')
    await expect
      .element(
        selfScreen.getByRole('menuitem', {
          name: i18n.t('users:rowActions.delete'),
        })
      )
      .toHaveAttribute('aria-disabled', 'true')
    await selfScreen.unmount()

    const protectedScreen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <TooltipProvider>
            <UserRowActions
              user={sampleUsers[0]}
              currentUserId={sampleUsers[1].id}
              actorIsSuperAdmin={false}
              canUpdate={true}
              canDelete={true}
              onEdit={vi.fn()}
              onResetPassword={vi.fn()}
              onToggleStatus={vi.fn()}
              onDelete={vi.fn()}
            />
          </TooltipProvider>
        </I18nextProvider>
      </QueryClientProvider>
    )
    await userEvent.click(
      protectedScreen.getByRole('button', {
        name: i18n.t('users:rowActions.openMenu'),
      })
    )
    await expect
      .element(
        protectedScreen.getByRole('menuitem', {
          name: i18n.t('users:rowActions.edit'),
        })
      )
      .toHaveAttribute('aria-disabled', 'true')
    await expect
      .element(
        protectedScreen.getByRole('menuitem', {
          name: i18n.t('users:rowActions.resetPassword'),
        })
      )
      .toHaveAttribute('aria-disabled', 'true')
    await expect
      .element(
        protectedScreen.getByRole('menuitem', {
          name: i18n.t('users:rowActions.delete'),
        })
      )
      .toHaveAttribute('aria-disabled', 'true')
  })

  it('hides create button without users:create permission', async () => {
    const client = createClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <PermissionGuard allowed={false}>
            <Button type='button'>{i18n.t('users:createUser')}</Button>
          </PermissionGuard>
        </I18nextProvider>
      </QueryClientProvider>
    )
    await expect
      .element(screen.getByText(i18n.t('users:createUser')))
      .not.toBeInTheDocument()
  })

  it('maps 409 username conflict and 422 field errors onto form fields', async () => {
    await changeAppLanguage('en-US')
    useAuthStore.getState().setSession({
      accessToken: 'tok',
      user: {
        id: '01950000-0000-7000-8000-000000000101',
        username: 'root',
        email: 'root@example.com',
        displayName: 'Root Admin',
      },
      roles: ['super_admin'],
      permissions: ['users:read', 'users:create'],
    })

    getMock.mockResolvedValue({ data: roleOptionsData })
    postMock
      .mockRejectedValueOnce(
        new ApiError({
          status: 409,
          code: 'USERS_USERNAME_CONFLICT',
          message: 'Username conflict err',
        })
      )
      .mockRejectedValueOnce(
        new ApiError({
          status: 422,
          code: 'VALIDATION_FAILED',
          message: 'Validation failed',
          fieldErrors: {
            email: 'Email invalid from server',
            status: 'Status invalid from server',
          },
        })
      )
      .mockRejectedValueOnce(
        new ApiError({
          status: 422,
          code: 'VALIDATION_FAILED',
          message: 'Unmapped validation failure',
          fieldErrors: { unknownField: 'Unknown field error' },
        })
      )

    const client = createClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <UserFormSheet
            open={true}
            mode='create'
            user={null}
            onOpenChange={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await userEvent.fill(
      screen.getByPlaceholder(i18n.t('users:form.usernamePlaceholder')),
      'alice'
    )
    await userEvent.fill(
      screen.getByPlaceholder(i18n.t('users:form.emailPlaceholder')),
      'alice@example.com'
    )
    await userEvent.fill(
      screen.getByPlaceholder(i18n.t('users:form.passwordPlaceholder')),
      'password123'
    )

    const submitBtn = screen.getByRole('button', {
      name: i18n.t('users:form.submitCreate'),
    })
    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText('Username conflict err'))
      .toBeVisible()

    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText('Email invalid from server'))
      .toBeVisible()
    await expect
      .element(screen.getByText('Status invalid from server'))
      .toBeVisible()

    await userEvent.click(submitBtn)
    await expect
      .element(screen.getByText('Unmapped validation failure'))
      .toBeVisible()
  })

  it('edits user and surfaces role options error with retry', async () => {
    await changeAppLanguage('en-US')
    useAuthStore.getState().setSession({
      accessToken: 'tok',
      user: {
        id: '01950000-0000-7000-8000-000000000101',
        username: 'root',
        email: 'root@example.com',
        displayName: 'Root Admin',
      },
      roles: ['super_admin'],
      permissions: ['users:read', 'users:update'],
    })

    getMock
      .mockRejectedValueOnce(new Error('role options error'))
      .mockResolvedValueOnce({ data: roleOptionsData })
    patchMock.mockResolvedValueOnce({ data: sampleUsers[1] })

    const onOpenChange = vi.fn()
    const client = createClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <UserFormSheet
            open={true}
            mode='edit'
            user={sampleUsers[1]}
            onOpenChange={onOpenChange}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect
      .element(screen.getByText(i18n.t('users:form.rolesLoadError')))
      .toBeVisible()
    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('common:retry') })
    )
    await expect
      .element(screen.getByText(i18n.t('users:form.rolesLoadError')))
      .not.toBeInTheDocument()

    const usernameInput = screen.getByPlaceholder(
      i18n.t('users:form.usernamePlaceholder')
    )
    await expect.element(usernameInput).toBeDisabled()

    const displayNameInput = screen.getByPlaceholder(
      i18n.t('users:form.displayNamePlaceholder')
    )
    await userEvent.clear(displayNameInput)
    await userEvent.fill(displayNameInput, 'Alice Updated')
    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('users:form.submitSave') })
    )

    expect(patchMock).toHaveBeenCalledWith(`/users/${sampleUsers[1].id}`, {
      email: 'alice@example.com',
      displayName: 'Alice Updated',
      status: 'disabled',
      roleIds: ['01950000-0000-7000-8000-000000000002'],
    })
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('toggles user status and keeps dialog open on error', async () => {
    await changeAppLanguage('en-US')
    patchMock
      .mockRejectedValueOnce(
        new ApiError({
          status: 403,
          code: 'FORBIDDEN',
          message: 'Cannot disable user',
        })
      )
      .mockResolvedValueOnce({
        data: { ...sampleUsers[0], status: 'disabled' },
      })

    const onOpenChange = vi.fn()
    const client = createClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <UserToggleStatusDialog
            user={sampleUsers[0]}
            open={true}
            onOpenChange={onOpenChange}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    const confirmBtn = screen.getByRole('button', {
      name: i18n.t('users:toggleStatusDialog.disableConfirm'),
    })
    await userEvent.click(confirmBtn)
    expect(onOpenChange).not.toHaveBeenCalledWith(false)

    await userEvent.click(confirmBtn)
    expect(patchMock).toHaveBeenLastCalledWith(`/users/${sampleUsers[0].id}`, {
      status: 'disabled',
    })
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('clears reset password banner error on cancel and handles delete error', async () => {
    await changeAppLanguage('en-US')
    patchMock.mockRejectedValueOnce(
      new ApiError({
        status: 500,
        code: 'INTERNAL_ERROR',
        message: 'Reset failed banner',
        requestId: 'req-reset-500',
      })
    )

    function ResetHarness() {
      const [open, setOpen] = useState(true)
      return (
        <>
          <button type='button' onClick={() => setOpen(true)}>
            reopen-reset
          </button>
          <UserResetPasswordDialog
            user={sampleUsers[1]}
            open={open}
            onOpenChange={setOpen}
          />
        </>
      )
    }

    const client = createClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <ResetHarness />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await userEvent.fill(
      screen.getByLabelText(i18n.t('users:resetPasswordDialog.newPassword'), {
        exact: true,
      }),
      'newpassword123'
    )
    await userEvent.fill(
      screen.getByLabelText(
        i18n.t('users:resetPasswordDialog.confirmPassword')
      ),
      'newpassword123'
    )
    await userEvent.click(
      screen.getByRole('button', {
        name: i18n.t('users:resetPasswordDialog.confirm'),
      })
    )
    const expectedBanner = i18n.t('errors:serverError', {
      requestId: 'req-reset-500',
    })
    await expect.element(screen.getByText(expectedBanner)).toBeVisible()

    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('common:cancel') })
    )
    await userEvent.click(screen.getByRole('button', { name: 'reopen-reset' }))
    await expect
      .element(screen.getByText(expectedBanner))
      .not.toBeInTheDocument()

    deleteMock.mockRejectedValueOnce(
      new ApiError({
        status: 403,
        code: 'FORBIDDEN',
        message: 'Delete failed',
      })
    )
    const onDeleteClose = vi.fn()
    const delFailScreen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <UserDeleteDialog
            user={sampleUsers[1]}
            open={true}
            onOpenChange={onDeleteClose}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )
    await userEvent.click(
      delFailScreen.getByRole('button', {
        name: i18n.t('users:deleteDialog.confirm'),
      })
    )
    expect(deleteMock).toHaveBeenCalledWith(`/users/${sampleUsers[1].id}`)
    expect(onDeleteClose).not.toHaveBeenCalledWith(false)
  })

  it('resets password and calls deleteUser on confirm', async () => {
    await changeAppLanguage('en-US')
    patchMock.mockResolvedValueOnce({ data: sampleUsers[1] })
    const onResetClose = vi.fn()
    const client = createClient()

    const resetScreen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <UserResetPasswordDialog
            user={sampleUsers[1]}
            open={true}
            onOpenChange={onResetClose}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await userEvent.fill(
      resetScreen.getByLabelText(
        i18n.t('users:resetPasswordDialog.newPassword'),
        { exact: true }
      ),
      'newpassword123'
    )
    await userEvent.fill(
      resetScreen.getByLabelText(
        i18n.t('users:resetPasswordDialog.confirmPassword')
      ),
      'newpassword123'
    )
    await userEvent.click(
      resetScreen.getByRole('button', {
        name: i18n.t('users:resetPasswordDialog.confirm'),
      })
    )
    expect(patchMock).toHaveBeenCalledWith(`/users/${sampleUsers[1].id}`, {
      password: 'newpassword123',
    })

    deleteMock.mockResolvedValueOnce({ data: undefined })
    const delScreen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <UserDeleteDialog
            user={sampleUsers[1]}
            open={true}
            onOpenChange={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await userEvent.click(
      delScreen.getByRole('button', {
        name: i18n.t('users:deleteDialog.confirm'),
      })
    )
    expect(deleteMock).toHaveBeenCalledWith(`/users/${sampleUsers[1].id}`)
  })

  it('validates client-side schemas and clears bannerError on switch/external close', async () => {
    await changeAppLanguage('en-US')
    useAuthStore.getState().setSession({
      accessToken: 'tok',
      user: {
        id: '01950000-0000-7000-8000-000000000101',
        username: 'root',
        email: 'root@example.com',
        displayName: 'Root Admin',
      },
      roles: ['super_admin'],
      permissions: ['users:read', 'users:create', 'users:update'],
    })

    getMock.mockResolvedValue({ data: roleOptionsData })
    postMock.mockRejectedValueOnce(
      new ApiError({
        status: 500,
        code: 'INTERNAL_ERROR',
        message: 'Create failed',
        requestId: 'req-create-500',
      })
    )

    function SheetHarness({ open }: { open: boolean }) {
      const [isOpen, setIsOpen] = useState(open)
      const [prevProp, setPrevProp] = useState(open)
      if (open !== prevProp) {
        setPrevProp(open)
        setIsOpen(open)
      }
      return (
        <UserFormSheet
          open={isOpen}
          mode='create'
          user={null}
          onOpenChange={setIsOpen}
        />
      )
    }

    const client = createClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <SheetHarness open={true} />
        </I18nextProvider>
      </QueryClientProvider>
    )

    const usernameInput = screen.getByPlaceholder(
      i18n.t('users:form.usernamePlaceholder')
    )
    const emailInput = screen.getByPlaceholder(
      i18n.t('users:form.emailPlaceholder')
    )
    const passwordInput = screen.getByPlaceholder(
      i18n.t('users:form.passwordPlaceholder')
    )
    const submitBtn = screen.getByRole('button', {
      name: i18n.t('users:form.submitCreate'),
    })

    await userEvent.fill(usernameInput, 'bob')
    await userEvent.fill(emailInput, 'not-an-email')
    await userEvent.fill(passwordInput, 'short')
    await userEvent.click(submitBtn)

    await expect
      .element(screen.getByText(i18n.t('validation:invalidEmail')))
      .toBeVisible()
    await expect
      .element(screen.getByText(i18n.t('validation:passwordMin')))
      .toBeVisible()
    expect(postMock).not.toHaveBeenCalled()

    await userEvent.clear(emailInput)
    await userEvent.fill(emailInput, 'bob@example.com')
    await userEvent.clear(passwordInput)
    await userEvent.fill(passwordInput, 'password123')
    await userEvent.click(submitBtn)

    const createBanner = i18n.t('errors:serverError', {
      requestId: 'req-create-500',
    })
    await expect.element(screen.getByText(createBanner)).toBeVisible()

    // Toggling Radix Switch should clear bannerError even though it does not fire native form onChange.
    await userEvent.click(screen.getByRole('switch'))
    await expect.element(screen.getByText(createBanner)).not.toBeInTheDocument()

    // Re-trigger bannerError and close via external prop without handleOpenChange.
    postMock.mockRejectedValueOnce(
      new ApiError({
        status: 500,
        code: 'INTERNAL_ERROR',
        message: 'Create failed again',
        requestId: 'req-create-501',
      })
    )
    await userEvent.click(submitBtn)
    const secondBanner = i18n.t('errors:serverError', {
      requestId: 'req-create-501',
    })
    await expect.element(screen.getByText(secondBanner)).toBeVisible()
    await screen.rerender(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <SheetHarness open={false} />
        </I18nextProvider>
      </QueryClientProvider>
    )
    await screen.rerender(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <SheetHarness open={true} />
        </I18nextProvider>
      </QueryClientProvider>
    )
    await expect.element(screen.getByText(secondBanner)).not.toBeInTheDocument()
    await screen.unmount()

    const resetScreen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <UserResetPasswordDialog
            user={sampleUsers[1]}
            open={true}
            onOpenChange={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )
    await userEvent.fill(
      resetScreen.getByLabelText(
        i18n.t('users:resetPasswordDialog.newPassword'),
        { exact: true }
      ),
      'newpassword123'
    )
    await userEvent.fill(
      resetScreen.getByLabelText(
        i18n.t('users:resetPasswordDialog.confirmPassword')
      ),
      'different123'
    )
    await userEvent.click(
      resetScreen.getByRole('button', {
        name: i18n.t('users:resetPasswordDialog.confirm'),
      })
    )
    await expect
      .element(resetScreen.getByText(i18n.t('validation:passwordMismatch')))
      .toBeVisible()
    expect(patchMock).not.toHaveBeenCalled()
  })

  it('locks super_admin in RoleMultiSelect for non-super-admins and self', async () => {
    await changeAppLanguage('en-US')
    const client = createClient()

    const nonSuperOnChange = vi.fn()
    const nonSuperScreen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <RoleMultiSelect
            options={roleOptionsData.items}
            value={[roleOptionsData.items[0].id, roleOptionsData.items[1].id]}
            onChange={nonSuperOnChange}
            actorIsSuperAdmin={false}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect
      .element(
        nonSuperScreen.getByRole('button', {
          name: i18n.t('users:form.removeRole', { name: 'Super Admin' }),
        })
      )
      .not.toBeInTheDocument()
    await userEvent.click(
      nonSuperScreen.getByRole('button', {
        name: i18n.t('users:form.removeRole', { name: 'Viewer' }),
      })
    )
    expect(nonSuperOnChange).toHaveBeenCalledWith([roleOptionsData.items[0].id])
    await nonSuperScreen.unmount()

    const selfOnChange = vi.fn()
    const selfScreen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <RoleMultiSelect
            options={roleOptionsData.items}
            value={[roleOptionsData.items[0].id]}
            onChange={selfOnChange}
            actorIsSuperAdmin={true}
            isSelf={true}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect
      .element(
        selfScreen.getByRole('button', {
          name: i18n.t('users:form.removeRole', { name: 'Super Admin' }),
        })
      )
      .not.toBeInTheDocument()
    await userEvent.click(selfScreen.getByRole('combobox'))
    await expect
      .element(selfScreen.getByRole('option', { name: 'Super Admin' }))
      .toHaveAttribute('aria-disabled', 'true')
  })

  it('syncs current profile and invalidates profile/menus/dashboard when updating self', async () => {
    await changeAppLanguage('en-US')
    useAuthStore.getState().setSession({
      accessToken: 'tok',
      user: {
        id: sampleUsers[0].id,
        username: 'root',
        email: 'root@example.com',
        displayName: 'Root Admin',
      },
      roles: ['super_admin'],
      permissions: ['users:read', 'users:update'],
    })

    getMock.mockImplementation((url: string) => {
      if (url === '/roles/options') {
        return Promise.resolve({ data: roleOptionsData })
      }
      if (url === '/auth/me') {
        return Promise.resolve({
          data: {
            user: {
              id: sampleUsers[0].id,
              username: 'root',
              email: 'root@example.com',
              displayName: 'Root Renamed',
            },
            roles: ['super_admin'],
            permissions: ['users:read', 'users:update'],
          },
        })
      }
      return Promise.resolve({ data: {} })
    })
    patchMock.mockResolvedValueOnce({
      data: { ...sampleUsers[0], displayName: 'Root Renamed' },
    })

    const client = createClient()
    const invalidateSpy = vi.spyOn(client, 'invalidateQueries')

    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <UserFormSheet
            open={true}
            mode='edit'
            user={sampleUsers[0]}
            onOpenChange={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    const displayNameInput = screen.getByPlaceholder(
      i18n.t('users:form.displayNamePlaceholder')
    )
    await userEvent.clear(displayNameInput)
    await userEvent.fill(displayNameInput, 'Root Renamed')
    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('users:form.submitSave') })
    )

    await expect
      .poll(() => useAuthStore.getState().user?.displayName)
      .toBe('Root Renamed')
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['dashboard'] })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['profile', 'me'] })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['menus', 'user'] })
  })
})

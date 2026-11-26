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
import { PermissionGuard } from '@/components/shared/permission-guard'
import { buildUsersQueryParams } from './api'
import { UserDeleteDialog } from './components/user-delete-dialog'
import { UserFormSheet } from './components/user-form-sheet'
import { UserResetPasswordDialog } from './components/user-reset-password-dialog'
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
}: {
  initialSearch?: Partial<UsersSearch>
  canUpdate?: boolean
  canDelete?: boolean
  onDelete?: (user: UserItem) => void
  onSearchChange?: (next: UsersSearch) => void
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
    const client = createClient()
    const screen = await render(
      <QueryClientProvider client={client}>
        <I18nextProvider i18n={i18n}>
          <UsersTableHarness
            initialSearch={{ page: 2 }}
            onSearchChange={(s) => searchSnapshots.push(s)}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect.element(screen.getByText('Root Admin')).toBeVisible()
    await expect.element(screen.getByText('@alice')).toBeVisible()

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
          fieldErrors: { email: 'Email invalid from server' },
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
})

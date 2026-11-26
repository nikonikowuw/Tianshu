import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { Plus } from 'lucide-react'
import { usePermission } from '@/lib/auth/use-permission'
import { type NavigateFn } from '@/hooks/use-table-url-state'
import { Button } from '@/components/ui/button'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { LanguageSwitch } from '@/components/shared/language-switch'
import { PermissionGuard } from '@/components/shared/permission-guard'
import { Search } from '@/components/shared/search'
import { ThemeSwitch } from '@/components/shared/theme-switch'
import { UserDeleteDialog } from './components/user-delete-dialog'
import { UserFormSheet } from './components/user-form-sheet'
import { UserResetPasswordDialog } from './components/user-reset-password-dialog'
import { UserToggleStatusDialog } from './components/user-toggle-status-dialog'
import { UsersTable } from './components/users-table'
import type { UsersSearch } from './schema'
import type { UserItem } from './types'

const route = getRouteApi('/_authenticated/users')

export function Users() {
  const { t } = useTranslation('users')
  const queryClient = useQueryClient()
  const search = route.useSearch()
  const routeNavigate = route.useNavigate()

  const canCreate = usePermission('users:create')
  const canUpdate = usePermission('users:update')
  const canDelete = usePermission('users:delete')

  const [formState, setFormState] = useState<{
    open: boolean
    mode: 'create' | 'edit'
    user: UserItem | null
  }>({
    open: false,
    mode: 'create',
    user: null,
  })
  const [resetPasswordUser, setResetPasswordUser] = useState<UserItem | null>(
    null
  )
  const [toggleStatusUser, setToggleStatusUser] = useState<UserItem | null>(
    null
  )
  const [deletingUser, setDeletingUser] = useState<UserItem | null>(null)

  const navigate: NavigateFn = useCallback(
    (opts) => {
      void routeNavigate({
        replace: opts.replace,
        search:
          opts.search === true
            ? true
            : typeof opts.search === 'function'
              ? (prev) =>
                  (
                    opts.search as (
                      p: Record<string, unknown>
                    ) => Partial<UsersSearch>
                  )(prev as Record<string, unknown>) as UsersSearch
              : (opts.search as UsersSearch),
      })
    },
    [routeNavigate]
  )

  const handleOpenCreate = useCallback(() => {
    setFormState({ open: true, mode: 'create', user: null })
  }, [])

  const handleEditUser = useCallback((user: UserItem) => {
    setFormState({ open: true, mode: 'edit', user })
  }, [])

  return (
    <>
      <Header fixed>
        <Search />
        <div className='ms-auto flex items-center space-x-2'>
          <LanguageSwitch
            onLanguageChange={() => {
              void queryClient.invalidateQueries()
            }}
          />
          <ThemeSwitch />
        </div>
      </Header>

      <Main fluid className='flex flex-1 flex-col gap-4 sm:gap-6'>
        <div className='flex flex-wrap items-end justify-between gap-2'>
          <div>
            <h1 className='text-2xl font-bold tracking-tight'>{t('title')}</h1>
            <p className='text-muted-foreground'>{t('description')}</p>
          </div>
          <PermissionGuard allowed={canCreate}>
            <Button type='button' onClick={handleOpenCreate}>
              <Plus className='size-4' />
              {t('createUser')}
            </Button>
          </PermissionGuard>
        </div>

        <UsersTable
          search={search}
          navigate={navigate}
          canUpdate={canUpdate}
          canDelete={canDelete}
          onEditUser={handleEditUser}
          onResetPassword={setResetPasswordUser}
          onToggleStatus={setToggleStatusUser}
          onDeleteUser={setDeletingUser}
        />
      </Main>

      <UserFormSheet
        open={formState.open}
        mode={formState.mode}
        user={formState.user}
        onOpenChange={(open) => {
          if (!open) {
            setFormState((prev) => ({ ...prev, open: false, user: null }))
          }
        }}
      />

      <UserResetPasswordDialog
        user={resetPasswordUser}
        open={resetPasswordUser !== null}
        onOpenChange={(open) => {
          if (!open) setResetPasswordUser(null)
        }}
      />

      <UserToggleStatusDialog
        user={toggleStatusUser}
        open={toggleStatusUser !== null}
        onOpenChange={(open) => {
          if (!open) setToggleStatusUser(null)
        }}
      />

      <UserDeleteDialog
        user={deletingUser}
        open={deletingUser !== null}
        onOpenChange={(open) => {
          if (!open) setDeletingUser(null)
        }}
      />
    </>
  )
}

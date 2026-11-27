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
import { RoleDeleteDialog } from './components/role-delete-dialog'
import { RoleFormSheet, type RoleSheetMode } from './components/role-form-sheet'
import { RolesTable } from './components/roles-table'
import type { RolesSearch } from './schema'
import type { RoleItem } from './types'

const route = getRouteApi('/_authenticated/roles')

export function Roles() {
  const { t } = useTranslation('roles')
  const queryClient = useQueryClient()
  const search = route.useSearch()
  const routeNavigate = route.useNavigate()
  // Subscribe to the in-memory permission list so a 403-triggered /auth/me sync
  // refreshes toolbar and row actions without a manual reload.
  const canCreate = usePermission('roles:create')
  const canUpdate = usePermission('roles:update')
  const canDelete = usePermission('roles:delete')

  const [sheetState, setSheetState] = useState<{
    open: boolean
    mode: RoleSheetMode
    role: RoleItem | null
  }>({
    open: false,
    mode: 'create',
    role: null,
  })

  const [deletingRole, setDeletingRole] = useState<RoleItem | null>(null)

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
                    ) => Partial<RolesSearch>
                  )(prev as Record<string, unknown>) as RolesSearch
              : (opts.search as RolesSearch),
      })
    },
    [routeNavigate]
  )

  const handleOpenCreate = useCallback(() => {
    setSheetState({ open: true, mode: 'create', role: null })
  }, [])

  const handleViewRole = useCallback((role: RoleItem) => {
    setSheetState({ open: true, mode: 'view', role })
  }, [])

  const handleEditRole = useCallback((role: RoleItem) => {
    setSheetState({ open: true, mode: 'edit', role })
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
              {t('createRole')}
            </Button>
          </PermissionGuard>
        </div>

        <RolesTable
          search={search}
          navigate={navigate}
          canUpdate={canUpdate}
          canDelete={canDelete}
          onViewRole={handleViewRole}
          onEditRole={handleEditRole}
          onDeleteRole={setDeletingRole}
        />
      </Main>

      <RoleFormSheet
        open={sheetState.open}
        mode={sheetState.mode}
        role={sheetState.role}
        canUpdate={canUpdate}
        onOpenChange={(open) => {
          if (!open) {
            setSheetState((prev) => ({ ...prev, open: false }))
          }
        }}
      />

      <RoleDeleteDialog
        role={deletingRole}
        open={deletingRole !== null}
        onOpenChange={(open) => {
          if (!open) {
            setDeletingRole(null)
          }
        }}
      />
    </>
  )
}

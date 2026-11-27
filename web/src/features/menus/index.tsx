import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
  ChevronsDownUp,
  ChevronsUpDown,
  Plus,
  RefreshCw,
} from 'lucide-react'
import { toast } from 'sonner'
import { usePermission } from '@/lib/auth/use-permission'
import { formatServerErrorMessage } from '@/lib/handle-server-error'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { LanguageSwitch } from '@/components/shared/language-switch'
import { PermissionGuard } from '@/components/shared/permission-guard'
import { Search } from '@/components/shared/search'
import { ThemeSwitch } from '@/components/shared/theme-switch'
import { MenuDeleteDialog } from './components/menu-delete-dialog'
import { MenuFormSheet } from './components/menu-form-sheet'
import { MenuTreeTable } from './components/menu-tree-table'
import { menuTreeQueryOptions, useReorderMenusMutation } from './queries'
import type { MenuItem } from './types'

function collectParentIds(nodes: MenuItem[]): string[] {
  const ids: string[] = []
  for (const n of nodes) {
    if (n.children.length > 0) {
      ids.push(n.id, ...collectParentIds(n.children))
    }
  }
  return ids
}

export function Menus() {
  const { t, i18n } = useTranslation('menus')
  const queryClient = useQueryClient()

  const canCreate = usePermission('menus:create')
  const canUpdate = usePermission('menus:update')
  const canDelete = usePermission('menus:delete')

  const { data, isLoading, isError, error, refetch } = useQuery(
    menuTreeQueryOptions(i18n.language)
  )
  const items = useMemo(() => data?.items ?? [], [data])

  const [collapsedIds, setCollapsedIds] = useState<Set<string>>(new Set())
  const allParentIds = useMemo(() => collectParentIds(items), [items])
  const hasExpandableParents = allParentIds.length > 0
  const expandedIds = useMemo(
    () => new Set(allParentIds.filter((id) => !collapsedIds.has(id))),
    [allParentIds, collapsedIds]
  )

  const [sheetState, setSheetState] = useState<{
    open: boolean
    mode: 'create' | 'edit'
    menu: MenuItem | null
    defaultParentId: string | null
  }>({
    open: false,
    mode: 'create',
    menu: null,
    defaultParentId: null,
  })

  const [deletingMenu, setDeletingMenu] = useState<MenuItem | null>(null)
  const reorderMutation = useReorderMenusMutation()

  const handleToggleExpand = useCallback((id: string) => {
    setCollapsedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }, [])

  const handleExpandAll = useCallback(() => {
    setCollapsedIds(new Set())
  }, [])

  const handleCollapseAll = useCallback(() => {
    setCollapsedIds(new Set(allParentIds))
  }, [allParentIds])

  const handleMoveSibling = useCallback(
    async (siblings: MenuItem[], index: number, direction: 'up' | 'down') => {
      const targetIndex = direction === 'up' ? index - 1 : index + 1
      if (targetIndex < 0 || targetIndex >= siblings.length) return

      const reordered = [...siblings]
      const [moved] = reordered.splice(index, 1)
      reordered.splice(targetIndex, 0, moved)

      const payload = reordered.map((item, idx) => ({
        id: item.id,
        parentId: item.parentId,
        sortOrder: (idx + 1) * 10,
      }))

      try {
        await reorderMutation.mutateAsync(payload)
        toast.success(t('toast.reordered'))
      } catch (err) {
        toast.error(formatServerErrorMessage(err))
      }
    },
    [reorderMutation, t]
  )

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
          <div className='flex items-center gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={isLoading || !hasExpandableParents}
              onClick={handleExpandAll}
            >
              <ChevronsUpDown className='size-4' />
              {t('expandAll')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={isLoading || !hasExpandableParents}
              onClick={handleCollapseAll}
            >
              <ChevronsDownUp className='size-4' />
              {t('collapseAll')}
            </Button>
            <PermissionGuard allowed={canCreate}>
              <Button
                type='button'
                onClick={() =>
                  setSheetState({
                    open: true,
                    mode: 'create',
                    menu: null,
                    defaultParentId: null,
                  })
                }
              >
                <Plus className='size-4' />
                {t('createMenu')}
              </Button>
            </PermissionGuard>
          </div>
        </div>

        {isError && (
          <Alert variant='destructive'>
            <AlertCircle className='size-4' />
            <AlertTitle>{t('error.loadTitle')}</AlertTitle>
            <AlertDescription className='flex items-center justify-between gap-4'>
              <span>{formatServerErrorMessage(error)}</span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => void refetch()}
              >
                <RefreshCw className='size-3.5' />
                {t('error.retry')}
              </Button>
            </AlertDescription>
          </Alert>
        )}

        {(!isError || Boolean(data)) && (
          <MenuTreeTable
            items={items}
            isLoading={isLoading}
            expandedIds={expandedIds}
            onToggleExpand={handleToggleExpand}
            canCreate={canCreate}
            canUpdate={canUpdate}
            canDelete={canDelete}
            isReordering={reorderMutation.isPending}
            onMoveSibling={(siblings, idx, dir) =>
              void handleMoveSibling(siblings, idx, dir)
            }
            onAddChild={(parent) =>
              setSheetState({
                open: true,
                mode: 'create',
                menu: null,
                defaultParentId: parent.id,
              })
            }
            onEdit={(menu) =>
              setSheetState({
                open: true,
                mode: 'edit',
                menu,
                defaultParentId: menu.parentId,
              })
            }
            onDelete={setDeletingMenu}
          />
        )}
      </Main>

      <MenuFormSheet
        open={sheetState.open}
        mode={sheetState.mode}
        menu={sheetState.menu}
        defaultParentId={sheetState.defaultParentId}
        treeItems={items}
        onOpenChange={(open) => {
          if (!open) {
            setSheetState((prev) => ({
              ...prev,
              open: false,
              menu: null,
              defaultParentId: null,
            }))
          }
        }}
      />

      <MenuDeleteDialog
        menu={deletingMenu}
        open={deletingMenu !== null}
        onOpenChange={(open) => {
          if (!open) setDeletingMenu(null)
        }}
      />
    </>
  )
}

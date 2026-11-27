import { Fragment } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ChevronRight,
  MoreHorizontal,
  Pencil,
  Plus,
  Trash2,
} from 'lucide-react'
import { resolveMenuIcon } from '@/lib/icons'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import type { MenuItem } from '../types'

type MenuTreeTableProps = {
  items: MenuItem[]
  isLoading: boolean
  expandedIds: Set<string>
  onToggleExpand: (id: string) => void
  canCreate: boolean
  canUpdate: boolean
  canDelete: boolean
  isReordering: boolean
  onMoveSibling: (
    siblings: MenuItem[],
    index: number,
    direction: 'up' | 'down'
  ) => void
  onAddChild: (parent: MenuItem) => void
  onEdit: (menu: MenuItem) => void
  onDelete: (menu: MenuItem) => void
}

export function MenuTreeTable({
  items,
  isLoading,
  expandedIds,
  onToggleExpand,
  canCreate,
  canUpdate,
  canDelete,
  isReordering,
  onMoveSibling,
  onAddChild,
  onEdit,
  onDelete,
}: MenuTreeTableProps) {
  const { t } = useTranslation('menus')

  const renderRows = (nodes: MenuItem[], depth: number): React.ReactNode => {
    return nodes.map((node, idx) => {
      const hasChildren = node.children.length > 0
      const isExpanded = expandedIds.has(node.id)
      const IconComp = resolveMenuIcon(node.icon)

      const showAddChild = canCreate && node.type !== 'button'
      const showEdit = canUpdate
      const showDelete = canDelete
      const hasPrimaryAction = showAddChild || showEdit
      const hasActions = hasPrimaryAction || showDelete
      const deleteDisabledReason = node.isSystem
        ? t('actions.systemImmutableHint')
        : hasChildren
          ? t('actions.hasChildrenHint')
          : null

      const deleteMenuItem = (
        <DropdownMenuItem
          disabled={deleteDisabledReason !== null}
          onSelect={() => onDelete(node)}
          className='text-destructive focus:text-destructive'
        >
          <Trash2 className='size-4' />
          {t('actions.delete')}
        </DropdownMenuItem>
      )

      return (
        <Fragment key={node.id}>
          <TableRow className='group/row'>
            <TableCell>
              <div
                className='flex items-center gap-1.5'
                style={{ paddingInlineStart: `${depth * 20}px` }}
              >
                {hasChildren ? (
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    className='size-6 shrink-0'
                    onClick={() => onToggleExpand(node.id)}
                    aria-expanded={isExpanded}
                    aria-label={
                      isExpanded
                        ? t('actions.collapseNode', { title: node.title })
                        : t('actions.expandNode', { title: node.title })
                    }
                  >
                    {isExpanded ? (
                      <ChevronDown className='size-3.5' />
                    ) : (
                      <ChevronRight className='size-3.5' />
                    )}
                  </Button>
                ) : (
                  <span className='inline-block size-6 shrink-0' />
                )}
                {IconComp && (
                  <IconComp className='size-4 shrink-0 text-muted-foreground' />
                )}
                <span className='font-medium'>{node.title}</span>
                {node.isSystem && (
                  <Badge
                    variant='outline'
                    className='text-[10px] font-normal text-muted-foreground'
                  >
                    {t('systemBadge')}
                  </Badge>
                )}
              </div>
            </TableCell>
            <TableCell>
              <Badge
                variant={
                  node.type === 'directory'
                    ? 'secondary'
                    : node.type === 'menu'
                      ? 'outline'
                      : 'default'
                }
              >
                {t(`types.${node.type}`)}
              </Badge>
            </TableCell>
            <TableCell>
              <span className='font-mono text-xs text-muted-foreground'>
                {node.code}
              </span>
            </TableCell>
            <TableCell>
              <span className='font-mono text-xs text-muted-foreground'>
                {node.path || '—'}
              </span>
            </TableCell>
            <TableCell>
              {node.permissionCode ? (
                <Badge variant='outline' className='font-mono text-xs'>
                  {node.permissionCode}
                </Badge>
              ) : (
                <span className='text-xs text-muted-foreground'>—</span>
              )}
            </TableCell>
            <TableCell>
              <div className='flex items-center gap-1'>
                <span className='w-8 font-mono text-xs tabular-nums'>
                  {node.sortOrder}
                </span>
                {canUpdate && (
                  <div className='flex items-center'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      className='size-6'
                      disabled={idx === 0 || isReordering}
                      onClick={() => onMoveSibling(nodes, idx, 'up')}
                      aria-label={t('actions.moveUpItem', {
                        title: node.title,
                      })}
                    >
                      <ArrowUp className='size-3.5' />
                    </Button>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      className='size-6'
                      disabled={idx === nodes.length - 1 || isReordering}
                      onClick={() => onMoveSibling(nodes, idx, 'down')}
                      aria-label={t('actions.moveDownItem', {
                        title: node.title,
                      })}
                    >
                      <ArrowDown className='size-3.5' />
                    </Button>
                  </div>
                )}
              </div>
            </TableCell>
            <TableCell>
              <Badge variant={node.visible ? 'outline' : 'secondary'}>
                {node.visible ? t('visibility.shown') : t('visibility.hidden')}
              </Badge>
            </TableCell>
            <TableCell>
              <Badge
                variant={node.status === 'active' ? 'success' : 'secondary'}
              >
                {node.status === 'active'
                  ? t('status.active')
                  : t('status.disabled')}
              </Badge>
            </TableCell>
            <TableCell
              className={cn(
                'sticky end-0 z-10 w-12 bg-background px-2.5 text-center transition-colors group-hover/row:bg-[color-mix(in_srgb,var(--muted)_50%,var(--background))] last:pe-2.5 has-[button[data-state=open]]:bg-muted'
              )}
            >
              {hasActions && (
                <DropdownMenu modal={false}>
                  <DropdownMenuTrigger asChild>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      className='size-7 text-muted-foreground hover:bg-muted hover:text-foreground'
                    >
                      <MoreHorizontal className='size-4' />
                      <span className='sr-only'>{t('actions.openMenu')}</span>
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align='end' className='w-40'>
                    {showAddChild && (
                      <DropdownMenuItem onSelect={() => onAddChild(node)}>
                        <Plus className='size-4' />
                        {t('actions.addChild')}
                      </DropdownMenuItem>
                    )}
                    {showEdit && (
                      <DropdownMenuItem onSelect={() => onEdit(node)}>
                        <Pencil className='size-4' />
                        {t('actions.edit')}
                      </DropdownMenuItem>
                    )}
                    {showDelete && (
                      <>
                        {hasPrimaryAction && <DropdownMenuSeparator />}
                        {deleteDisabledReason ? (
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <div>{deleteMenuItem}</div>
                            </TooltipTrigger>
                            <TooltipContent side='left'>
                              {deleteDisabledReason}
                            </TooltipContent>
                          </Tooltip>
                        ) : (
                          deleteMenuItem
                        )}
                      </>
                    )}
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </TableCell>
          </TableRow>
          {hasChildren && isExpanded && renderRows(node.children, depth + 1)}
        </Fragment>
      )
    })
  }

  return (
    <div className='relative overflow-hidden rounded-md border'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('columns.title')}</TableHead>
            <TableHead>{t('columns.type')}</TableHead>
            <TableHead>{t('columns.code')}</TableHead>
            <TableHead>{t('columns.path')}</TableHead>
            <TableHead>{t('columns.permission')}</TableHead>
            <TableHead>{t('columns.sortOrder')}</TableHead>
            <TableHead>{t('columns.visible')}</TableHead>
            <TableHead>{t('columns.status')}</TableHead>
            <TableHead className='sticky end-0 z-10 w-12 bg-background px-2.5 last:pe-2.5'>
              <span className='sr-only'>{t('actions.openMenu')}</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {isLoading ? (
            Array.from({ length: 6 }).map((_, idx) => (
              <TableRow key={`skeleton-${idx}`}>
                {Array.from({ length: 9 }).map((__, colIdx) => (
                  <TableCell key={colIdx}>
                    <Skeleton className='h-4 w-full max-w-28' />
                  </TableCell>
                ))}
              </TableRow>
            ))
          ) : items.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={9}
                className='h-32 text-center text-muted-foreground'
              >
                {t('empty')}
              </TableCell>
            </TableRow>
          ) : (
            renderRows(items, 0)
          )}
        </TableBody>
      </Table>
    </div>
  )
}

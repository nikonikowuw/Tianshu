import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { resolveMenuIcon } from '@/lib/icons'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import type { PermissionGroup, RoleMenuTreeNode } from '../types'
import {
  buildPermissionTree,
  collectCodes,
  collectExpandableIds,
  type PermissionTreeNode,
} from './permission-tree-builder'

type RolePermissionTreeProps = {
  menuTree: RoleMenuTreeNode[]
  groups: PermissionGroup[]
  selectedPermissions: string[]
  disabled?: boolean
  onChange: (nextPermissions: string[]) => void
}

export function RolePermissionTree({
  menuTree,
  groups,
  selectedPermissions,
  disabled = false,
  onChange,
}: RolePermissionTreeProps) {
  const { t } = useTranslation('roles')
  const [collapsedIds, setCollapsedIds] = useState<Set<string>>(new Set())

  const treeNodes = useMemo(
    () => buildPermissionTree(menuTree, groups, t('sheet.otherPermissions')),
    [menuTree, groups, t]
  )

  const expandableIds = useMemo(
    () => collectExpandableIds(treeNodes),
    [treeNodes]
  )
  const expandedIds = useMemo(
    () => new Set(expandableIds.filter((id) => !collapsedIds.has(id))),
    [expandableIds, collapsedIds]
  )

  const selectedSet = useMemo(
    () => new Set(selectedPermissions),
    [selectedPermissions]
  )

  const toggleExpand = (id: string) => {
    setCollapsedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  const toggleNode = (node: PermissionTreeNode, checked: boolean) => {
    const targetCodes = collectCodes(node)
    if (targetCodes.length === 0) return
    const next = new Set(selectedPermissions)
    for (const code of targetCodes) {
      if (checked) {
        next.add(code)
      } else {
        next.delete(code)
      }
    }
    onChange(Array.from(next).sort())
  }

  const renderNodes = (nodes: PermissionTreeNode[], depth: number) =>
    nodes.map((node) => {
      const hasChildren = node.children.length > 0
      const isExpanded = expandedIds.has(node.id)
      const nodeCodes = collectCodes(node)
      const selectedCount = nodeCodes.filter((c) => selectedSet.has(c)).length
      const checkState: boolean | 'indeterminate' =
        nodeCodes.length === 0
          ? false
          : selectedCount === nodeCodes.length
            ? true
            : selectedCount > 0
              ? 'indeterminate'
              : false
      const IconComp = resolveMenuIcon(node.icon)

      return (
        <div key={node.id}>
          <div
            className='flex items-center gap-2 rounded-md px-2 py-1.5 transition-colors hover:bg-muted/50'
            style={{ paddingInlineStart: `${depth * 20 + 8}px` }}
          >
            {hasChildren ? (
              <button
                type='button'
                onClick={() => toggleExpand(node.id)}
                className='flex size-5 shrink-0 items-center justify-center rounded hover:bg-muted'
                aria-label={node.label}
              >
                {isExpanded ? (
                  <ChevronDown className='size-3.5 text-muted-foreground' />
                ) : (
                  <ChevronRight className='size-3.5 text-muted-foreground' />
                )}
              </button>
            ) : (
              <span className='inline-block size-5 shrink-0' />
            )}

            <Checkbox
              checked={checkState}
              disabled={disabled || nodeCodes.length === 0}
              aria-label={node.label}
              onCheckedChange={(val) => toggleNode(node, val === true)}
            />

            <button
              type='button'
              disabled={disabled || nodeCodes.length === 0}
              onClick={() => toggleNode(node, checkState !== true)}
              className='flex min-w-0 flex-1 items-center gap-2 text-start disabled:cursor-not-allowed'
            >
              {IconComp && (
                <IconComp className='size-3.5 shrink-0 text-muted-foreground' />
              )}
              <span className='truncate text-sm font-medium'>{node.label}</span>
              <Badge
                variant='secondary'
                className='h-4 px-1.5 text-[10px] font-normal'
              >
                {t(`sheet.nodeTypes.${node.type}`)}
              </Badge>
              {node.permissionCode && (
                <span className='ms-auto shrink-0 font-mono text-[11px] text-muted-foreground'>
                  {node.permissionCode}
                </span>
              )}
            </button>
          </div>
          {hasChildren && isExpanded && renderNodes(node.children, depth + 1)}
        </div>
      )
    })

  return (
    <div className='rounded-lg border'>
      {expandableIds.length > 0 && (
        <div className='flex items-center justify-end gap-1 border-b bg-muted/30 px-2 py-1'>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='h-6 px-2 text-xs'
            onClick={() => setCollapsedIds(new Set())}
          >
            {t('sheet.expandAll')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='h-6 px-2 text-xs'
            onClick={() => setCollapsedIds(new Set(expandableIds))}
          >
            {t('sheet.collapseAll')}
          </Button>
        </div>
      )}
      <div className='divide-y divide-border/40 p-1.5'>
        {renderNodes(treeNodes, 0)}
      </div>
    </div>
  )
}

import { useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { resolveMenuIcon } from '@/lib/icons'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import type { PermissionGroup, RoleMenuTreeNode } from '../types'
import {
  buildPermissionTree,
  collectCodes,
  collectDescendantCodesMap,
  collectExpandableIds,
  type PermissionTreeNode,
} from './permission-tree-builder'

type RolePermissionTreeProps = {
  id?: string
  ariaLabelledBy?: string
  ariaDescribedBy?: string
  ariaInvalid?: boolean
  menuTree: RoleMenuTreeNode[]
  groups: PermissionGroup[]
  selectedPermissions: string[]
  disabled?: boolean
  onChange: (nextPermissions: string[]) => void
}

export function RolePermissionTree({
  id,
  ariaLabelledBy,
  ariaDescribedBy,
  ariaInvalid,
  menuTree,
  groups,
  selectedPermissions,
  disabled = false,
  onChange,
}: RolePermissionTreeProps) {
  const { t } = useTranslation('roles')
  const treeId = useId()
  const [collapsedIds, setCollapsedIds] = useState<Set<string>>(new Set())

  const treeNodes = useMemo(
    () => buildPermissionTree(menuTree, groups, t('sheet.otherPermissions')),
    [menuTree, groups, t]
  )

  const codesByNodeId = useMemo(
    () => collectDescendantCodesMap(treeNodes),
    [treeNodes]
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
    const targetCodes = codesByNodeId.get(node.id) ?? collectCodes(node)
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
      const nodeCodes = codesByNodeId.get(node.id) ?? []
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
      const isNodeDisabled = disabled || nodeCodes.length === 0
      const checkboxId = `${treeId}-${node.id}`

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
                aria-expanded={isExpanded}
                aria-label={t(
                  isExpanded ? 'sheet.collapseNode' : 'sheet.expandNode',
                  { label: node.label }
                )}
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
              id={checkboxId}
              checked={checkState}
              disabled={isNodeDisabled}
              aria-label={node.label}
              onCheckedChange={(val) => toggleNode(node, val === true)}
            />

            <label
              htmlFor={checkboxId}
              className={cn(
                'flex min-w-0 flex-1 items-center gap-2 text-start select-none',
                isNodeDisabled
                  ? 'cursor-not-allowed opacity-70'
                  : 'cursor-pointer'
              )}
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
            </label>
          </div>
          {hasChildren && isExpanded && renderNodes(node.children, depth + 1)}
        </div>
      )
    })

  return (
    <div
      id={id}
      role='group'
      aria-labelledby={ariaLabelledBy}
      aria-describedby={ariaDescribedBy}
      aria-invalid={ariaInvalid || undefined}
      className={cn('rounded-lg border', ariaInvalid && 'border-destructive')}
    >
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

import type { PermissionGroup, RoleMenuTreeNode } from '../types'

export type PermissionTreeNode = {
  id: string
  label: string
  type: 'directory' | 'menu' | 'button'
  icon?: string
  permissionCode?: string
  children: PermissionTreeNode[]
}

export function collectCodes(node: PermissionTreeNode): string[] {
  const codes: string[] = []
  if (node.permissionCode) {
    codes.push(node.permissionCode)
  }
  for (const child of node.children) {
    codes.push(...collectCodes(child))
  }
  return codes
}

function mapMenuNode(node: RoleMenuTreeNode): PermissionTreeNode {
  return {
    id: node.id,
    label: node.title || node.code,
    type: node.type,
    icon: node.icon,
    permissionCode: node.permissionCode || undefined,
    children: (node.children ?? []).map(mapMenuNode),
  }
}

function prunePermissionlessNodes(
  nodes: PermissionTreeNode[]
): PermissionTreeNode[] {
  const out: PermissionTreeNode[] = []
  for (const node of nodes) {
    const children = prunePermissionlessNodes(node.children)
    if (!node.permissionCode && children.length === 0) {
      continue
    }
    out.push({ ...node, children })
  }
  return out
}

export function collectExpandableIds(nodes: PermissionTreeNode[]): string[] {
  const ids: string[] = []
  for (const n of nodes) {
    if (n.children.length > 0) {
      ids.push(n.id, ...collectExpandableIds(n.children))
    }
  }
  return ids
}

export function buildPermissionTree(
  menuTree: RoleMenuTreeNode[],
  groups: PermissionGroup[],
  otherLabel: string
): PermissionTreeNode[] {
  if (menuTree.length === 0 || !('type' in menuTree[0])) {
    return groups.map((g) => ({
      id: `group:${g.module}`,
      label: g.label,
      type: 'directory',
      children: g.permissions.map((p) => ({
        id: `perm:${p.code}`,
        label: p.name,
        type: 'button',
        permissionCode: p.code,
        children: [],
      })),
    }))
  }

  const tree = prunePermissionlessNodes(menuTree.map(mapMenuNode))
  const boundCodes = new Set<string>()
  for (const root of tree) {
    for (const c of collectCodes(root)) {
      boundCodes.add(c)
    }
  }

  const unboundNodes: PermissionTreeNode[] = []
  for (const g of groups) {
    for (const p of g.permissions) {
      if (!boundCodes.has(p.code)) {
        unboundNodes.push({
          id: `perm:${p.code}`,
          label: p.name,
          type: 'button',
          permissionCode: p.code,
          children: [],
        })
      }
    }
  }

  if (unboundNodes.length > 0) {
    tree.push({
      id: 'group:__unbound__',
      label: otherLabel,
      type: 'directory',
      children: unboundNodes,
    })
  }
  return tree
}

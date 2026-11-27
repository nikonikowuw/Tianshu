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

function normalizePermissionBranches(
  nodes: PermissionTreeNode[],
  permissionLabelByCode: Map<string, string>
): PermissionTreeNode[] {
  return nodes.map((node) => {
    const children = normalizePermissionBranches(
      node.children,
      permissionLabelByCode
    )
    if (children.length > 0 && node.permissionCode) {
      const childCodes = new Set(children.flatMap(collectCodes))
      const nextChildren = childCodes.has(node.permissionCode)
        ? children
        : [
            {
              id: `${node.id}::self`,
              label:
                permissionLabelByCode.get(node.permissionCode) ?? node.label,
              type: node.type,
              icon: node.icon,
              permissionCode: node.permissionCode,
              children: [],
            },
            ...children,
          ]
      // Split non-leaf menu permissions into an explicit child leaf so read
      // permissions can be toggled independently from button actions.
      return {
        ...node,
        permissionCode: undefined,
        children: nextChildren,
      }
    }
    return { ...node, children }
  })
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
  if (menuTree.length === 0) {
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

  const permissionLabelByCode = new Map<string, string>()
  for (const g of groups) {
    for (const p of g.permissions) {
      permissionLabelByCode.set(p.code, p.name)
    }
  }

  const tree = normalizePermissionBranches(
    prunePermissionlessNodes(menuTree.map(mapMenuNode)),
    permissionLabelByCode
  )
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

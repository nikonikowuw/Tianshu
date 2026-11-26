import { type TFunction } from 'i18next'
import {
  LayoutDashboard,
  ListTree,
  ScrollText,
  Shield,
  Users,
} from 'lucide-react'
import { resolveMenuIcon } from '@/lib/icons'
import {
  type NavGroup,
  type NavItem,
  type NavLink,
} from '@/components/layout/types'

export type DynamicMenuNode = {
  id: string
  type: 'directory' | 'menu' | 'button'
  title: string
  path: string
  icon: string
  children: DynamicMenuNode[]
}

export function buildNavGroups(
  t: TFunction<'common'>,
  hasPermission: (code: string) => boolean
): NavGroup[] {
  const rawGroups: NavGroup[] = [
    {
      title: t('nav.general'),
      items: [
        {
          title: t('nav.dashboard'),
          url: '/',
          icon: LayoutDashboard,
          permission: 'dashboard:read',
        },
      ],
    },
    {
      title: t('nav.system'),
      items: [
        {
          title: t('nav.users'),
          url: '/users',
          icon: Users,
          permission: 'users:read',
        },
        {
          title: t('nav.roles'),
          url: '/roles',
          icon: Shield,
          permission: 'roles:read',
        },
        {
          title: t('nav.menus'),
          url: '/menus',
          icon: ListTree,
          permission: 'menus:read',
        },
        {
          title: t('nav.auditLogs'),
          url: '/audit-logs',
          icon: ScrollText,
          permission: 'audit:read',
        },
      ],
    },
  ]

  return rawGroups
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => isItemAllowed(item, hasPermission)),
    }))
    .filter((group) => group.items.length > 0)
}

function toNavItem(node: DynamicMenuNode): NavItem | null {
  if (node.type === 'button') return null
  const icon = resolveMenuIcon(node.icon)
  const subItems: NavLink[] = node.children
    .filter((ch) => ch.type !== 'button' && Boolean(ch.path))
    .map((ch) => ({
      title: ch.title,
      url: ch.path,
      icon: resolveMenuIcon(ch.icon),
    }))

  if (subItems.length > 0) {
    return {
      title: node.title,
      icon,
      items: subItems,
    }
  }
  if (!node.path) return null
  return {
    title: node.title,
    url: node.path,
    icon,
  }
}

export function buildNavGroupsFromMenus(nodes: DynamicMenuNode[]): NavGroup[] {
  const groups: NavGroup[] = []
  const orphanItems: NavItem[] = []

  for (const node of nodes) {
    if (node.type === 'directory') {
      const items = node.children
        .map(toNavItem)
        .filter((item): item is NavItem => item !== null)
      if (items.length > 0) {
        groups.push({
          title: node.title,
          items,
        })
      }
      continue
    }
    const item = toNavItem(node)
    if (item) {
      orphanItems.push(item)
    }
  }

  if (orphanItems.length > 0) {
    groups.unshift({
      title: '',
      items: orphanItems,
    })
  }
  return groups
}

function isItemAllowed(
  item: NavItem,
  hasPermission: (code: string) => boolean
): boolean {
  if (item.permission && !hasPermission(item.permission)) {
    return false
  }
  return true
}

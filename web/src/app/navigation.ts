import { type TFunction } from 'i18next'
import { LayoutDashboard } from 'lucide-react'
import { type NavGroup, type NavItem } from '@/components/layout/types'

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
  ]

  return rawGroups
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => isItemAllowed(item, hasPermission)),
    }))
    .filter((group) => group.items.length > 0)
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

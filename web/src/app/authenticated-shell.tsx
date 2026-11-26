import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { apiClient } from '@/lib/api/client'
import { AuthenticatedLayout } from '@/components/layout/authenticated-layout'
import {
  buildNavGroups,
  buildNavGroupsFromMenus,
  type DynamicMenuNode,
} from './navigation'
import { authGateway } from './router'

// Adapter bridging auth-store and dynamic navigation menus into the decoupled AuthenticatedLayout.
export function AuthenticatedShell() {
  const { t, i18n } = useTranslation('common')
  const navigate = useNavigate()
  const user = useAuthStore((s) => s.user)
  const permissions = useAuthStore((s) => s.permissions)
  const status = useAuthStore((s) => s.status)

  const { data: userMenusData } = useQuery({
    queryKey: ['menus', 'user', i18n.language],
    queryFn: async ({ signal }) => {
      const res = await apiClient.get<{ items: DynamicMenuNode[] }>(
        '/menus/user',
        { signal }
      )
      return res.data
    },
    enabled: status === 'authenticated',
    staleTime: 60 * 1000,
    meta: { silent: true },
  })

  const navGroups = useMemo(() => {
    if (userMenusData?.items && userMenusData.items.length > 0) {
      return buildNavGroupsFromMenus(userMenusData.items)
    }
    const permSet = new Set(permissions)
    return buildNavGroups(t, (code) => permSet.has(code))
  }, [userMenusData, permissions, t])

  const handleSignOut = async () => {
    await authGateway.signOut()
    await navigate({ to: '/sign-in', replace: true })
  }

  return (
    <AuthenticatedLayout
      appName={t('appName')}
      appSubtitle={t('appSubtitle')}
      navGroups={navGroups}
      user={{
        name: user?.displayName || user?.username || 'User',
        email: user?.email ?? '',
        avatar: user?.avatarUrl,
      }}
      onSignOut={handleSignOut}
    />
  )
}

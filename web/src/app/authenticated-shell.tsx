import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { AuthenticatedLayout } from '@/components/layout/authenticated-layout'
import { buildNavGroups } from './navigation'
import { authGateway } from './router'

// Adapter bridging auth-store and navigation permissions into the decoupled AuthenticatedLayout.
export function AuthenticatedShell() {
  const { t } = useTranslation('common')
  const navigate = useNavigate()
  const user = useAuthStore((s) => s.user)
  const permissions = useAuthStore((s) => s.permissions)

  const navGroups = useMemo(() => {
    const permSet = new Set(permissions)
    return buildNavGroups(t, (code) => permSet.has(code))
  }, [permissions, t])

  const handleSignOut = async () => {
    await authGateway.signOut()
    await navigate({ to: '/sign-in', replace: true })
  }

  return (
    <AuthenticatedLayout
      appName={t('appName')}
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

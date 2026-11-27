import { QueryCache, QueryClient } from '@tanstack/react-query'
import { createRouter } from '@tanstack/react-router'
import { routeTree } from '@/routeTree.gen'
import { toast } from 'sonner'
import i18n from '@/i18n/index'
import { useAuthStore } from '@/stores/auth-store'
import { configureApiClient } from '@/lib/api/client'
import { normalizeApiError } from '@/lib/api/error'
import { createAuthGateway, type AuthGateway } from '@/lib/auth/gateway'
import { handleServerError } from '@/lib/handle-server-error'

export type RouterContext = {
  queryClient: QueryClient
  auth: AuthGateway
}

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (failureCount, error) => {
        if (import.meta.env.DEV) return false
        const normalized = normalizeApiError(error)
        if ([400, 401, 403, 404, 422].includes(normalized.status)) {
          return false
        }
        return failureCount < 2
      },
      refetchOnWindowFocus: import.meta.env.PROD,
      staleTime: 10_000,
    },
  },
  queryCache: new QueryCache({
    onError: (error, query) => {
      if (query.meta?.silent) return
      const normalized = normalizeApiError(error)
      if (
        normalized.status === 401 ||
        normalized.code === 'AUTH_ACCOUNT_DISABLED'
      ) {
        return
      }
      handleServerError(error)
    },
  }),
})

export const authGateway = createAuthGateway(() => {
  queryClient.clear()
})

export const router = createRouter({
  routeTree,
  context: {
    queryClient,
    auth: authGateway,
  },
  defaultPreload: 'intent',
  defaultPreloadStaleTime: 0,
})

configureApiClient({
  getAccessToken: () => useAuthStore.getState().accessToken,
  refreshAccessToken: () => authGateway.refreshAccessToken(),
  onSessionExpired: () => {
    const pathname = router.state.location.pathname
    if (pathname === '/sign-in' || pathname === '/setup') {
      return
    }
    toast.error(i18n.t('errors:sessionExpired'), { id: 'session-expired' })
    void router.navigate({
      to: '/sign-in',
      search: { redirect: router.state.location.href },
      replace: true,
    })
  },
  onAccountDisabled: () => {
    useAuthStore.getState().clearSession({ broadcast: true })
    queryClient.clear()
    toast.error(i18n.t('errors:accountDisabled'), { id: 'account-disabled' })
    void router.navigate({ to: '/sign-in', replace: true })
  },
  onForbiddenRefresh: async () => {
    try {
      await authGateway.syncProfile()
    } catch {
      // Handled by caller or subsequent auth state check.
    }
  },
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

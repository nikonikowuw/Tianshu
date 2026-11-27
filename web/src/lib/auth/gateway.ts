import {
  useAuthStore,
  type AuthStatus,
  type AuthUser,
} from '@/stores/auth-store'
import { apiClient } from '@/lib/api/client'

export type AuthGateway = {
  getStatus: () => AuthStatus
  ensureSession: () => Promise<boolean>
  refreshAccessToken: () => Promise<string | null>
  syncProfile: () => Promise<void>
  signOut: () => Promise<void>
  hasPermission: (code: string) => boolean
}

type RefreshResponse = {
  accessToken: string
}

type MeResponse = {
  user: AuthUser
  roles: string[]
  permissions: string[]
}

let inFlightRefresh: Promise<string | null> | null = null

function hasControlChars(value: string): boolean {
  for (let i = 0; i < value.length; i += 1) {
    const code = value.charCodeAt(i)
    if (code <= 0x1f || code === 0x7f) {
      return true
    }
  }
  return false
}

export function isSafeRedirect(target: string | undefined): target is string {
  if (!target) return false
  if (!target.startsWith('/')) return false
  if (target.startsWith('//')) return false
  if (target.includes('\\') || target.includes('://')) return false
  if (hasControlChars(target)) return false
  try {
    const parsed = new URL(target, 'https://tianshu.invalid')
    return parsed.origin === 'https://tianshu.invalid'
  } catch {
    return false
  }
}

export function sanitizeRedirect(target: string | undefined): string {
  return isSafeRedirect(target) ? target : '/'
}

async function performSingleFlightRefresh(): Promise<string | null> {
  if (inFlightRefresh) {
    return inFlightRefresh
  }

  inFlightRefresh = (async () => {
    try {
      const res = await apiClient.post<RefreshResponse>('/auth/refresh')
      const token = res.data?.accessToken ?? ''
      if (!token) {
        useAuthStore.getState().clearSession({ broadcast: false })
        return null
      }
      useAuthStore.getState().setAccessToken(token)
      return token
    } catch {
      useAuthStore.getState().clearSession({ broadcast: false })
      return null
    } finally {
      inFlightRefresh = null
    }
  })()

  return inFlightRefresh
}

export async function syncCurrentProfile(): Promise<void> {
  const token = useAuthStore.getState().accessToken
  if (!token) return
  const res = await apiClient.get<MeResponse>('/auth/me')
  useAuthStore.getState().setSession({
    accessToken: token,
    user: res.data.user,
    roles: res.data.roles ?? [],
    permissions: res.data.permissions ?? [],
  })
}

export async function syncCurrentProfileIfHasRole(
  roleCode: string
): Promise<void> {
  const { accessToken, roles } = useAuthStore.getState()
  if (!accessToken || !roles.includes(roleCode)) return
  await syncCurrentProfile()
}

export function createAuthGateway(onSessionCleared: () => void): AuthGateway {
  return {
    getStatus: () => useAuthStore.getState().status,

    refreshAccessToken: async () => {
      const token = await performSingleFlightRefresh()
      if (!token) {
        onSessionCleared()
      }
      return token
    },

    ensureSession: async () => {
      const state = useAuthStore.getState()
      if (state.status === 'authenticated' && state.accessToken) {
        return true
      }

      const token = await performSingleFlightRefresh()
      if (!token) {
        return false
      }

      try {
        await syncCurrentProfile()
        return true
      } catch {
        useAuthStore.getState().clearSession({ broadcast: false })
        onSessionCleared()
        return false
      }
    },

    syncProfile: syncCurrentProfile,

    signOut: async () => {
      try {
        await apiClient.post('/auth/logout')
      } catch {
        // Always clear local session even if backend logout fails.
      } finally {
        useAuthStore.getState().clearSession({ broadcast: true })
        onSessionCleared()
      }
    },

    hasPermission: (code: string) =>
      useAuthStore.getState().hasPermission(code),
  }
}

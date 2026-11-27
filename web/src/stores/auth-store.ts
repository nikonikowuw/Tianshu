import { create } from 'zustand'

export type AuthStatus = 'unknown' | 'authenticated' | 'unauthenticated'

export type AuthUser = {
  id: string
  username: string
  email: string
  displayName: string
  avatarFileId?: string | null
  avatarUrl?: string | null
}

type AuthSession = {
  accessToken: string
  user: AuthUser
  roles: string[]
  permissions: string[]
}

type AuthStoreState = {
  status: AuthStatus
  accessToken: string
  user: AuthUser | null
  roles: string[]
  permissions: string[]
  setSession: (session: AuthSession) => void
  updateUser: (patch: Partial<AuthUser>) => void
  setAccessToken: (token: string) => void
  clearSession: (options?: { broadcast?: boolean }) => void
  hasPermission: (code: string) => boolean
}

const AUTH_CHANNEL_NAME = 'tianshu-auth'

function getAuthChannel(): BroadcastChannel | null {
  if (
    typeof window === 'undefined' ||
    typeof BroadcastChannel === 'undefined'
  ) {
    return null
  }
  return new BroadcastChannel(AUTH_CHANNEL_NAME)
}

// Access tokens and permissions stay strictly in memory (never written to localStorage or JS cookies).
export const useAuthStore = create<AuthStoreState>((set, get) => ({
  status: 'unknown',
  accessToken: '',
  user: null,
  roles: [],
  permissions: [],

  setSession: (session) =>
    set({
      status: 'authenticated',
      accessToken: session.accessToken,
      user: session.user,
      roles: [...session.roles],
      permissions: [...session.permissions],
    }),

  updateUser: (patch) =>
    set((state) => (state.user ? { user: { ...state.user, ...patch } } : {})),

  setAccessToken: (token) => set({ accessToken: token }),

  clearSession: (options) => {
    set({
      status: 'unauthenticated',
      accessToken: '',
      user: null,
      roles: [],
      permissions: [],
    })
    if (options?.broadcast) {
      const ch = getAuthChannel()
      if (ch) {
        ch.postMessage({ type: 'LOGOUT' })
        ch.close()
      }
    }
  },

  hasPermission: (code) => get().permissions.includes(code),
}))

export function subscribeCrossTabLogout(onLogout: () => void): () => void {
  const ch = getAuthChannel()
  if (!ch) return () => {}

  ch.onmessage = (event: MessageEvent<{ type?: string }>) => {
    if (event.data?.type === 'LOGOUT') {
      useAuthStore.getState().clearSession({ broadcast: false })
      onLogout()
    }
  }
  return () => ch.close()
}

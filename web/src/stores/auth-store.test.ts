import { beforeEach, describe, expect, it } from 'vitest'
import { useAuthStore } from './auth-store'

describe('useAuthStore', () => {
  beforeEach(() => {
    useAuthStore.getState().clearSession({ broadcast: false })
  })

  it('stores session strictly in memory and checks permissions', () => {
    useAuthStore.getState().setSession({
      accessToken: 'mem-token',
      user: {
        id: 'u-1',
        username: 'admin',
        email: 'admin@example.com',
        displayName: 'Admin',
      },
      roles: ['super_admin'],
      permissions: ['dashboard:read', 'users:read'],
    })

    const state = useAuthStore.getState()
    expect(state.status).toBe('authenticated')
    expect(state.accessToken).toBe('mem-token')
    expect(state.hasPermission('users:read')).toBe(true)
    expect(state.hasPermission('users:delete')).toBe(false)
    expect(window.localStorage.getItem('accessToken')).toBeNull()
  })
})

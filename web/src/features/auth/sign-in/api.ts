import { useMutation } from '@tanstack/react-query'
import { useAuthStore, type AuthUser } from '@/stores/auth-store'
import { apiClient } from '@/lib/api/client'

export type LoginPayload = {
  identifier: string
  password: string
}

export type LoginResponse = {
  accessToken: string
  expiresAt: string
  user: AuthUser
  roles: string[]
  permissions: string[]
}

export function useLoginMutation() {
  return useMutation({
    mutationFn: async (payload: LoginPayload) => {
      const res = await apiClient.post<LoginResponse>('/auth/login', payload)
      return res.data
    },
    onSuccess: (data) => {
      useAuthStore.getState().setSession({
        accessToken: data.accessToken,
        user: data.user,
        roles: data.roles ?? [],
        permissions: data.permissions ?? [],
      })
    },
  })
}

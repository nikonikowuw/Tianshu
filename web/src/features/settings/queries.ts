import {
  queryOptions,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import { useAuthStore } from '@/stores/auth-store'
import {
  changePassword,
  fetchActiveSessions,
  fetchAvatarPolicy,
  fetchProfile,
  revokeOtherProfileSessions,
  revokeProfileSession,
  updateProfile,
  uploadAvatarFile,
} from './api'
import type { ChangePasswordPayload, UpdateProfilePayload } from './types'

export const profileKeys = {
  all: ['profile'] as const,
  meRoot: ['profile', 'me'] as const,
  me: (lang: string) => ['profile', 'me', lang] as const,
  sessions: ['profile', 'sessions'] as const,
  avatarPolicy: ['profile', 'avatar-policy'] as const,
}

export function profileQueryOptions(lang: string) {
  return queryOptions({
    queryKey: profileKeys.me(lang),
    queryFn: ({ signal }) => fetchProfile(signal),
    staleTime: 60_000,
    meta: { silent: true },
  })
}

export function avatarPolicyQueryOptions() {
  return queryOptions({
    queryKey: profileKeys.avatarPolicy,
    queryFn: ({ signal }) => fetchAvatarPolicy(signal),
    staleTime: 5 * 60_000,
    meta: { silent: true },
  })
}

export function activeSessionsQueryOptions() {
  return queryOptions({
    queryKey: profileKeys.sessions,
    queryFn: ({ signal }) => fetchActiveSessions(signal),
    staleTime: 10_000,
    meta: { silent: true },
  })
}

export function useUpdateProfileMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload: UpdateProfilePayload) => updateProfile(payload),
    meta: { silent: true },
    onSuccess: (updated) => {
      useAuthStore.getState().updateUser({
        displayName: updated.displayName,
        email: updated.email,
        avatarFileId: updated.avatarFileId,
        avatarUrl: updated.avatarUrl,
      })
      void queryClient.invalidateQueries({ queryKey: profileKeys.meRoot })
      void queryClient.invalidateQueries({ queryKey: ['users'] })
    },
  })
}

export function useUploadAvatarMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (file: File) => uploadAvatarFile(file),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['files'] })
    },
  })
}

export function useChangePasswordMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload: ChangePasswordPayload) => changePassword(payload),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: profileKeys.sessions })
    },
  })
}

export function useRevokeSessionMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (sessionId: string) => revokeProfileSession(sessionId),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: profileKeys.sessions })
    },
  })
}

export function useRevokeOtherSessionsMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => revokeOtherProfileSessions(),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: profileKeys.sessions })
    },
  })
}

import { apiClient, resolveApiUrl } from '@/lib/api/client'
import type {
  ActiveSessionsResponse,
  AvatarUploadPolicy,
  ChangePasswordPayload,
  ChangePasswordResponse,
  ProfileItem,
  RevokeOtherSessionsResponse,
  UpdateProfilePayload,
  UploadedAvatarFile,
} from './types'

const UPLOAD_TIMEOUT_MS = 60_000

function normalizeProfile(item: ProfileItem): ProfileItem {
  return {
    ...item,
    avatarUrl: item.avatarUrl ? resolveApiUrl(item.avatarUrl) : null,
  }
}

export async function fetchProfile(signal?: AbortSignal): Promise<ProfileItem> {
  const { data } = await apiClient.get<ProfileItem>('/profile', { signal })
  return normalizeProfile(data)
}

export async function updateProfile(
  payload: UpdateProfilePayload
): Promise<ProfileItem> {
  const { data } = await apiClient.patch<ProfileItem>('/profile', payload)
  return normalizeProfile(data)
}

export async function fetchAvatarPolicy(
  signal?: AbortSignal
): Promise<AvatarUploadPolicy> {
  const { data } = await apiClient.get<AvatarUploadPolicy>('/files/config', {
    signal,
  })
  return data
}

export async function uploadAvatarFile(
  file: File,
  signal?: AbortSignal
): Promise<UploadedAvatarFile> {
  const form = new FormData()
  form.append('file', file)
  const { data } = await apiClient.post<UploadedAvatarFile>(
    '/files/avatar',
    form,
    {
      timeout: UPLOAD_TIMEOUT_MS,
      signal,
    }
  )
  return data
}

export async function changePassword(
  payload: ChangePasswordPayload
): Promise<ChangePasswordResponse> {
  const { data } = await apiClient.put<ChangePasswordResponse>(
    '/profile/password',
    payload
  )
  return data
}

export async function fetchActiveSessions(
  signal?: AbortSignal
): Promise<ActiveSessionsResponse> {
  const { data } = await apiClient.get<ActiveSessionsResponse>(
    '/profile/sessions',
    { signal }
  )
  return data
}

export async function revokeProfileSession(sessionId: string): Promise<void> {
  await apiClient.delete(`/profile/sessions/${sessionId}`)
}

export async function revokeOtherProfileSessions(): Promise<RevokeOtherSessionsResponse> {
  const { data } =
    await apiClient.delete<RevokeOtherSessionsResponse>('/profile/sessions')
  return data
}

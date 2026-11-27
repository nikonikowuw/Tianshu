export type ProfileRoleRef = {
  id: string
  code: string
  name: string
  isSystem: boolean
}

export type ProfileItem = {
  id: string
  username: string
  email: string
  displayName: string
  avatarFileId: string | null
  avatarUrl: string | null
  status: 'active' | 'disabled'
  lastLoginAt: string | null
  roles: ProfileRoleRef[]
  createdAt: string
  updatedAt: string
}

export type UpdateProfilePayload = {
  displayName?: string
  email?: string
  avatarFileId?: string | null
}

export type ChangePasswordPayload = {
  currentPassword: string
  newPassword: string
  revokeOtherSessions: boolean
}

export type ChangePasswordResponse = {
  revokedSessions: number
}

export type ActiveSessionItem = {
  id: string
  ipAddress: string
  userAgent: string
  isCurrent: boolean
  lastUsedAt: string
  createdAt: string
  expiresAt: string
}

export type ActiveSessionsResponse = {
  items: ActiveSessionItem[]
}

export type RevokeOtherSessionsResponse = {
  revokedSessions: number
}

export type AvatarUploadPolicy = {
  maxFileSizeBytes: number
  maxAvatarSizeBytes: number
  allowedExtensions: string[]
}

export type UploadedAvatarFile = {
  id: string
  originalName: string
  mimeType: string
  sizeBytes: number
}

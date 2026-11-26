import type { Paginated } from '@/lib/api/types'

export type UserStatus = 'active' | 'disabled'

export type UserRoleRef = {
  id: string
  code: string
  name: string
  isSystem: boolean
}

export type UserActorRef = {
  id: string
  username: string
}

export type UserItem = {
  id: string
  username: string
  email: string
  displayName: string
  status: UserStatus
  lastLoginAt: string | null
  roles: UserRoleRef[]
  createdBy: UserActorRef | null
  updatedBy: UserActorRef | null
  createdAt: string
  updatedAt: string
}

export type UsersListParams = {
  page: number
  pageSize: number
  q?: string
  status?: UserStatus[]
  roleId?: string[]
  sort?: 'created_at' | 'updated_at' | 'username' | 'last_login_at'
  order?: 'asc' | 'desc'
}

export type UsersPage = Paginated<UserItem>

export type RoleOption = {
  id: string
  code: string
  name: string
  isSystem: boolean
}

export type RoleOptionsResponse = {
  items: RoleOption[]
}

export type CreateUserPayload = {
  username: string
  email: string
  displayName?: string
  password: string
  status: UserStatus
  roleIds: string[]
}

export type UpdateUserPayload = {
  email?: string
  displayName?: string
  status?: UserStatus
  password?: string
  roleIds?: string[]
}

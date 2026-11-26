import type { Paginated } from '@/lib/api/types'

export type PermissionItem = {
  code: string
  module: string
  name: string
  description: string
}

export type PermissionGroup = {
  module: string
  label: string
  permissions: PermissionItem[]
}

export type PermissionsCatalogResponse = {
  groups: PermissionGroup[]
}

export type RoleItem = {
  id: string
  code: string
  name: string
  description: string
  isSystem: boolean
  userCount: number
  permissionCount: number
  permissions: string[]
  createdBy: string | null
  updatedBy: string | null
  createdAt: string
  updatedAt: string
}

export type RolesListParams = {
  page: number
  pageSize: number
  q?: string
  isSystem?: boolean
  sort?: 'created_at' | 'updated_at' | 'code' | 'name'
  order?: 'asc' | 'desc'
}

export type RolesPage = Paginated<RoleItem>

export type CreateRolePayload = {
  code: string
  name: string
  description: string
  permissions: string[]
}

export type UpdateRolePayload = {
  name?: string
  description?: string
  permissions?: string[]
}

export type RoleMenuTreeNode = {
  id: string
  type: 'directory' | 'menu' | 'button'
  code: string
  title: string
  icon: string
  permissionCode: string
  children: RoleMenuTreeNode[]
}

export type RoleMenuTreeResponse = {
  items: RoleMenuTreeNode[]
}

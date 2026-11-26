import { apiClient } from '@/lib/api/client'
import type {
  CreateRolePayload,
  PermissionsCatalogResponse,
  RoleItem,
  RoleMenuTreeResponse,
  RolesListParams,
  RolesPage,
  UpdateRolePayload,
} from './types'

export function buildRolesQueryParams(
  params: RolesListParams
): URLSearchParams {
  const sp = new URLSearchParams()
  sp.set('page', String(params.page))
  sp.set('pageSize', String(params.pageSize))

  if (params.q) {
    sp.set('q', params.q)
  }
  if (typeof params.isSystem === 'boolean') {
    sp.set('isSystem', String(params.isSystem))
  }
  if (params.sort) {
    sp.set('sort', params.sort)
  }
  if (params.order) {
    sp.set('order', params.order)
  }
  return sp
}

export async function fetchPermissionsCatalog(
  signal?: AbortSignal
): Promise<PermissionsCatalogResponse> {
  const response = await apiClient.get<PermissionsCatalogResponse>(
    '/permissions',
    { signal }
  )
  return response.data
}

export async function fetchRoleMenuTree(
  signal?: AbortSignal
): Promise<RoleMenuTreeResponse> {
  const response = await apiClient.get<RoleMenuTreeResponse>('/menus/tree', {
    signal,
  })
  return response.data
}

export async function fetchRoles(
  params: RolesListParams,
  signal?: AbortSignal
): Promise<RolesPage> {
  const searchParams = buildRolesQueryParams(params)
  const response = await apiClient.get<RolesPage>('/roles', {
    params: searchParams,
    signal,
  })
  return response.data
}

export async function createRole(
  payload: CreateRolePayload
): Promise<RoleItem> {
  const response = await apiClient.post<RoleItem>('/roles', payload)
  return response.data
}

export async function updateRole(
  id: string,
  payload: UpdateRolePayload
): Promise<RoleItem> {
  const response = await apiClient.patch<RoleItem>(`/roles/${id}`, payload)
  return response.data
}

export async function deleteRole(id: string): Promise<void> {
  await apiClient.delete(`/roles/${id}`)
}

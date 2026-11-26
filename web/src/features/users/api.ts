import { apiClient } from '@/lib/api/client'
import type {
  CreateUserPayload,
  RoleOptionsResponse,
  UpdateUserPayload,
  UserItem,
  UsersListParams,
  UsersPage,
} from './types'

export function buildUsersQueryParams(
  params: UsersListParams
): URLSearchParams {
  const sp = new URLSearchParams()
  sp.set('page', String(params.page))
  sp.set('pageSize', String(params.pageSize))

  if (params.q) {
    sp.set('q', params.q)
  }
  if (params.status && params.status.length > 0) {
    for (const s of params.status) {
      sp.append('status', s)
    }
  }
  if (params.roleId && params.roleId.length > 0) {
    for (const id of params.roleId) {
      sp.append('roleId', id)
    }
  }
  if (params.sort) {
    sp.set('sort', params.sort)
  }
  if (params.order) {
    sp.set('order', params.order)
  }
  return sp
}

export async function fetchUsers(
  params: UsersListParams,
  signal?: AbortSignal
): Promise<UsersPage> {
  const searchParams = buildUsersQueryParams(params)
  const response = await apiClient.get<UsersPage>('/users', {
    params: searchParams,
    signal,
  })
  return response.data
}

export async function fetchRoleOptions(
  signal?: AbortSignal
): Promise<RoleOptionsResponse> {
  const response = await apiClient.get<RoleOptionsResponse>('/roles/options', {
    signal,
  })
  return response.data
}

export async function createUser(
  payload: CreateUserPayload
): Promise<UserItem> {
  const response = await apiClient.post<UserItem>('/users', payload)
  return response.data
}

export async function updateUser(
  id: string,
  payload: UpdateUserPayload
): Promise<UserItem> {
  const response = await apiClient.patch<UserItem>(`/users/${id}`, payload)
  return response.data
}

export async function deleteUser(id: string): Promise<void> {
  await apiClient.delete(`/users/${id}`)
}

import { apiClient } from '@/lib/api/client'
import type {
  CreateMenuPayload,
  MenuItem,
  MenuTreeResponse,
  ReorderMenuItemPayload,
  UpdateMenuPayload,
} from './types'

export async function fetchMenuTree(
  signal?: AbortSignal
): Promise<MenuTreeResponse> {
  const response = await apiClient.get<MenuTreeResponse>('/menus/tree', {
    signal,
  })
  return response.data
}

export async function fetchUserMenus(
  signal?: AbortSignal
): Promise<MenuTreeResponse> {
  const response = await apiClient.get<MenuTreeResponse>('/menus/user', {
    signal,
  })
  return response.data
}

export async function createMenu(
  payload: CreateMenuPayload
): Promise<MenuItem> {
  const response = await apiClient.post<MenuItem>('/menus', payload)
  return response.data
}

export async function updateMenu(
  id: string,
  payload: UpdateMenuPayload
): Promise<MenuItem> {
  const response = await apiClient.patch<MenuItem>(`/menus/${id}`, payload)
  return response.data
}

export async function reorderMenus(
  items: ReorderMenuItemPayload[]
): Promise<MenuTreeResponse> {
  const response = await apiClient.put<MenuTreeResponse>('/menus/reorder', {
    items,
  })
  return response.data
}

export async function deleteMenu(id: string): Promise<void> {
  await apiClient.delete(`/menus/${id}`)
}

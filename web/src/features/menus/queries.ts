import {
  queryOptions,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import { apiClient } from '@/lib/api/client'
import {
  createMenu,
  deleteMenu,
  fetchMenuTree,
  reorderMenus,
  updateMenu,
} from './api'
import type {
  CreateMenuPayload,
  ReorderMenuItemPayload,
  UpdateMenuPayload,
} from './types'

export type PermissionCatalogResponse = {
  groups: Array<{
    module: string
    label: string
    permissions: Array<{ code: string; name: string }>
  }>
}

export const menusKeys = {
  all: ['menus'] as const,
  tree: (lang: string) => ['menus', 'tree', lang] as const,
  user: (lang: string) => ['menus', 'user', lang] as const,
  permissions: (lang: string) => ['menus', 'permissions', lang] as const,
}

export function menuTreeQueryOptions(lang: string) {
  return queryOptions({
    queryKey: menusKeys.tree(lang),
    queryFn: ({ signal }) => fetchMenuTree(signal),
    meta: { silent: true },
  })
}

export function menuPermissionsQueryOptions(lang: string) {
  return queryOptions({
    queryKey: menusKeys.permissions(lang),
    queryFn: async ({ signal }) => {
      const res = await apiClient.get<PermissionCatalogResponse>(
        '/permissions',
        { signal }
      )
      return res.data
    },
    staleTime: 5 * 60 * 1000,
    meta: { silent: true },
  })
}

export function useCreateMenuMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload: CreateMenuPayload) => createMenu(payload),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: menusKeys.all })
    },
  })
}

export function useUpdateMenuMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: UpdateMenuPayload }) =>
      updateMenu(id, payload),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: menusKeys.all })
    },
  })
}

export function useReorderMenusMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (items: ReorderMenuItemPayload[]) => reorderMenus(items),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: menusKeys.all })
    },
  })
}

export function useDeleteMenuMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => deleteMenu(id),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: menusKeys.all })
    },
  })
}

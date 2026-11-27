import {
  queryOptions,
  useMutation,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query'
import {
  createMenu,
  deleteMenu,
  fetchMenuPermissionsCatalog,
  fetchMenuTree,
  reorderMenus,
  updateMenu,
} from './api'
import type {
  CreateMenuPayload,
  ReorderMenuItemPayload,
  UpdateMenuPayload,
} from './types'

export const menusKeys = {
  // Invalidating ['menus'] also refreshes ['menus', 'user', lang] in AuthenticatedShell.
  all: ['menus'] as const,
  tree: (lang: string) => ['menus', 'tree', lang] as const,
  permissions: (lang: string) => ['menus', 'permissions', lang] as const,
}

function invalidateMenuCaches(queryClient: QueryClient) {
  void queryClient.invalidateQueries({ queryKey: menusKeys.all })
  void queryClient.invalidateQueries({ queryKey: ['roles', 'menu-tree'] })
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
    queryFn: ({ signal }) => fetchMenuPermissionsCatalog(signal),
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
      invalidateMenuCaches(queryClient)
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
      invalidateMenuCaches(queryClient)
    },
  })
}

export function useReorderMenusMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (items: ReorderMenuItemPayload[]) => reorderMenus(items),
    meta: { silent: true },
    onSuccess: () => {
      invalidateMenuCaches(queryClient)
    },
  })
}

export function useDeleteMenuMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => deleteMenu(id),
    meta: { silent: true },
    onSuccess: () => {
      invalidateMenuCaches(queryClient)
    },
  })
}

import { useAuthStore, type AuthUser } from '@/stores/auth-store'

// Subscribes reactively to the in-memory permission list while keeping
// features decoupled from direct store imports.
export function usePermission(code: string): boolean {
  return useAuthStore((state) => state.permissions.includes(code))
}

export function useCurrentUser(): AuthUser | null {
  return useAuthStore((state) => state.user)
}

export function useIsSuperAdmin(): boolean {
  return useAuthStore((state) => state.roles.includes('super_admin'))
}

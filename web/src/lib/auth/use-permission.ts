import { useAuthStore } from '@/stores/auth-store'

// Subscribes reactively to the in-memory permission list while keeping
// features decoupled from direct store imports.
export function usePermission(code: string): boolean {
  return useAuthStore((state) => state.permissions.includes(code))
}

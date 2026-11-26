import { type ReactNode } from 'react'

type PermissionGuardProps = {
  allowed: boolean
  fallback?: ReactNode
  children: ReactNode
}

// Pure permission presentation guard; callers pass permission check result from app/auth layer.
export function PermissionGuard({
  allowed,
  fallback = null,
  children,
}: PermissionGuardProps) {
  return allowed ? children : fallback
}

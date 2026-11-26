import {
  Activity,
  Bell,
  Box,
  Database,
  File,
  FileText,
  Folder,
  KeyRound,
  Layers,
  LayoutDashboard,
  ListTree,
  Lock,
  type LucideIcon,
  ScrollText,
  Settings,
  Shield,
  Tag,
  Users,
} from 'lucide-react'

const ICON_REGISTRY: Record<string, LucideIcon> = {
  LayoutDashboard,
  Users,
  Shield,
  ListTree,
  ScrollText,
  Folder,
  FileText,
  Settings,
  KeyRound,
  Lock,
  Layers,
  Database,
  Box,
  Tag,
  Activity,
  Bell,
  File,
}

export const AVAILABLE_MENU_ICONS = Object.keys(ICON_REGISTRY)

export function resolveMenuIcon(name?: string): LucideIcon | undefined {
  if (!name) return undefined
  return ICON_REGISTRY[name]
}

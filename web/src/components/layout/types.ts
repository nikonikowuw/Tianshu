import { type LinkProps } from '@tanstack/react-router'

export type LayoutUser = {
  name: string
  email: string
  avatar?: string
}

type BaseNavItem = {
  title: string
  badge?: string
  icon?: React.ElementType
  permission?: string
}

export type NavLink = BaseNavItem & {
  url: LinkProps['to'] | (string & {})
  items?: never
}

export type NavCollapsible = BaseNavItem & {
  items: (BaseNavItem & { url: LinkProps['to'] | (string & {}) })[]
  url?: never
}

export type NavItem = NavCollapsible | NavLink

export type NavGroup = {
  title: string
  items: NavItem[]
}

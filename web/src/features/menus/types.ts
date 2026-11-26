export type MenuNodeType = 'directory' | 'menu' | 'button'

export type MenuStatus = 'active' | 'disabled'

export type MenuItem = {
  id: string
  parentId: string | null
  type: MenuNodeType
  code: string
  title: string
  path: string
  icon: string
  permissionCode: string
  sortOrder: number
  visible: boolean
  status: MenuStatus
  isSystem: boolean
  children: MenuItem[]
  createdBy: string | null
  updatedBy: string | null
  createdAt: string
  updatedAt: string
}

export type MenuTreeResponse = {
  items: MenuItem[]
}

export type CreateMenuPayload = {
  parentId: string | null
  type: MenuNodeType
  code: string
  title: string
  path: string
  icon: string
  permissionCode: string
  sortOrder: number
  visible: boolean
  status: MenuStatus
}

export type UpdateMenuPayload = {
  parentId?: string | null
  type?: MenuNodeType
  title?: string
  path?: string
  icon?: string
  permissionCode?: string
  sortOrder?: number
  visible?: boolean
  status?: MenuStatus
}

export type ReorderMenuItemPayload = {
  id: string
  parentId: string | null
  sortOrder: number
}

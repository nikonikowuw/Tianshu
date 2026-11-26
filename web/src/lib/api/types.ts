export type Paginated<T> = {
  items: T[]
  total: number
  page: number
  pageSize: number
}

export type ListParams = {
  page: number
  pageSize: number
  q?: string
  sort?: string
  order?: 'asc' | 'desc'
  [filter: string]: string | string[] | number | undefined
}

export type ApiFieldError = {
  field: string
  message: string
}

export type ApiErrorBody = {
  code: string
  message: string
  errors?: ApiFieldError[]
}

export type DashboardRangeDays = 7 | 14 | 30

export type DashboardUserStats = {
  total: number
  active: number
  disabled: number
  newLast7Days: number
}

export type DashboardRoleStats = {
  total: number
  system: number
  custom: number
}

export type DashboardFileStats = {
  total: number
  totalSizeBytes: number
  images: number
  documents: number
}

export type DashboardSessionStats = {
  active: number
}

export type DashboardAuditStats = {
  totalInRange: number
  loginsInRange: number
  failuresInRange: number
}

export type DailyActivityPoint = {
  date: string
  logins: number
  operations: number
  failures: number
}

export type RecentActivityItem = {
  id: string
  actorUsername: string
  action: string
  resource: string
  resourceId: string
  status: 'success' | 'failure' | string
  ipAddress: string
  createdAt: string
}

export type DashboardSummary = {
  generatedAt: string
  rangeDays: DashboardRangeDays
  users: DashboardUserStats
  roles: DashboardRoleStats
  files: DashboardFileStats
  sessions: DashboardSessionStats
  audit: DashboardAuditStats
  activityTrend: DailyActivityPoint[]
  recentActivity: RecentActivityItem[]
}

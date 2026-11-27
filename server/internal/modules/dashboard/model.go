package dashboard

import (
	"time"

	"github.com/google/uuid"
)

// SummaryInput configures a dashboard summary query.
// A nil Days means "use default (7)"; a non-nil value must match the allowed whitelist.
type SummaryInput struct {
	Days *int
}

// Summary aggregates read-only system statistics, trends, and recent activity.
type Summary struct {
	GeneratedAt    time.Time
	RangeDays      int
	Users          UserStats
	Roles          RoleStats
	Files          FileStats
	Sessions       SessionStats
	Audit          AuditStats
	ActivityTrend  []DailyActivityPoint
	RecentActivity []RecentActivityItem
}

// UserStats holds non-deleted user counts and recent signups.
type UserStats struct {
	Total        int64
	Active       int64
	Disabled     int64
	NewLast7Days int64
}

// RoleStats holds total, system, and custom role counts.
type RoleStats struct {
	Total  int64
	System int64
	Custom int64
}

// FileStats holds ready file totals, byte usage, and category counts.
type FileStats struct {
	Total          int64
	TotalSizeBytes int64
	Images         int64
	Documents      int64
}

// SessionStats holds currently active user session counts.
type SessionStats struct {
	Active int64
}

// AuditStats summarizes audit events within the selected time window.
type AuditStats struct {
	TotalInRange    int64
	LoginsInRange   int64
	FailuresInRange int64
}

// DailyActivityPoint represents aggregated audit counts for one UTC day.
type DailyActivityPoint struct {
	Date       string
	Logins     int64
	Operations int64
	Failures   int64
}

// RecentActivityItem represents a minimal, redacted audit entry shown on the dashboard.
type RecentActivityItem struct {
	ID            uuid.UUID
	ActorUsername string
	Action        string
	Resource      string
	ResourceID    string
	Status        string
	IPAddress     string
	CreatedAt     time.Time
}

package dashboard

import "time"

type summaryResponse struct {
	GeneratedAt    string                   `json:"generatedAt"`
	RangeDays      int                      `json:"rangeDays"`
	Users          userStatsResponse        `json:"users"`
	Roles          roleStatsResponse        `json:"roles"`
	Files          fileStatsResponse        `json:"files"`
	Sessions       sessionStatsResponse     `json:"sessions"`
	Audit          auditStatsResponse       `json:"audit"`
	ActivityTrend  []dailyActivityResponse  `json:"activityTrend"`
	RecentActivity []recentActivityResponse `json:"recentActivity"`
}

type userStatsResponse struct {
	Total        int64 `json:"total"`
	Active       int64 `json:"active"`
	Disabled     int64 `json:"disabled"`
	NewLast7Days int64 `json:"newLast7Days"`
}

type roleStatsResponse struct {
	Total  int64 `json:"total"`
	System int64 `json:"system"`
	Custom int64 `json:"custom"`
}

type fileStatsResponse struct {
	Total          int64 `json:"total"`
	TotalSizeBytes int64 `json:"totalSizeBytes"`
	Images         int64 `json:"images"`
	Documents      int64 `json:"documents"`
}

type sessionStatsResponse struct {
	Active int64 `json:"active"`
}

type auditStatsResponse struct {
	TotalInRange    int64 `json:"totalInRange"`
	LoginsInRange   int64 `json:"loginsInRange"`
	FailuresInRange int64 `json:"failuresInRange"`
}

type dailyActivityResponse struct {
	Date       string `json:"date"`
	Logins     int64  `json:"logins"`
	Operations int64  `json:"operations"`
	Failures   int64  `json:"failures"`
}

type recentActivityResponse struct {
	ID            string `json:"id"`
	ActorUsername string `json:"actorUsername"`
	Action        string `json:"action"`
	Resource      string `json:"resource"`
	ResourceID    string `json:"resourceId"`
	Status        string `json:"status"`
	IPAddress     string `json:"ipAddress"`
	CreatedAt     string `json:"createdAt"`
}

func toSummaryResponse(s Summary) summaryResponse {
	trend := make([]dailyActivityResponse, 0, len(s.ActivityTrend))
	for _, point := range s.ActivityTrend {
		trend = append(trend, dailyActivityResponse(point))
	}

	recent := make([]recentActivityResponse, 0, len(s.RecentActivity))
	for _, item := range s.RecentActivity {
		recent = append(recent, recentActivityResponse{
			ID:            item.ID.String(),
			ActorUsername: item.ActorUsername,
			Action:        item.Action,
			Resource:      item.Resource,
			ResourceID:    item.ResourceID,
			Status:        item.Status,
			IPAddress:     item.IPAddress,
			CreatedAt:     item.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	return summaryResponse{
		GeneratedAt:    s.GeneratedAt.UTC().Format(time.RFC3339),
		RangeDays:      s.RangeDays,
		Users:          userStatsResponse(s.Users),
		Roles:          roleStatsResponse(s.Roles),
		Files:          fileStatsResponse(s.Files),
		Sessions:       sessionStatsResponse(s.Sessions),
		Audit:          auditStatsResponse(s.Audit),
		ActivityTrend:  trend,
		RecentActivity: recent,
	}
}

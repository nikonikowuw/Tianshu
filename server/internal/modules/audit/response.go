package audit

import (
	"time"

	"tianshu/internal/shared/pagination"
)

type auditLogResponse struct {
	ID            string         `json:"id"`
	ActorID       *string        `json:"actorId"`
	ActorUsername string         `json:"actorUsername"`
	Action        string         `json:"action"`
	Resource      string         `json:"resource"`
	ResourceID    string         `json:"resourceId"`
	Status        string         `json:"status"`
	IPAddress     string         `json:"ipAddress"`
	UserAgent     string         `json:"userAgent"`
	RequestID     string         `json:"requestId"`
	Metadata      map[string]any `json:"metadata"`
	CreatedAt     string         `json:"createdAt"`
}

func toAuditLogResponse(entry Entry) auditLogResponse {
	var actorID *string
	if entry.ActorID != nil {
		s := entry.ActorID.String()
		actorID = &s
	}
	return auditLogResponse{
		ID:            entry.ID.String(),
		ActorID:       actorID,
		ActorUsername: entry.ActorUsername,
		Action:        entry.Action,
		Resource:      entry.Resource,
		ResourceID:    entry.ResourceID,
		Status:        entry.Status,
		IPAddress:     entry.IPAddress,
		UserAgent:     entry.UserAgent,
		RequestID:     entry.RequestID,
		Metadata:      cloneMetadata(entry.Metadata),
		CreatedAt:     entry.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func toListAuditLogsResponse(res pagination.Result[Entry]) pagination.Result[auditLogResponse] {
	items := make([]auditLogResponse, 0, len(res.Items))
	for _, item := range res.Items {
		items = append(items, toAuditLogResponse(item))
	}
	return pagination.NewResult(items, res.Total, pagination.Params{
		Page:     res.Page,
		PageSize: res.PageSize,
	})
}

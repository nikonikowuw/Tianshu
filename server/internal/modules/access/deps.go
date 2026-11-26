package access

import (
	"context"

	"github.com/google/uuid"
)

// AuditRecorder persists security/business audit events without importing the audit module.
type AuditRecorder interface {
	Record(ctx context.Context, entry AuditEntry) error
}

// AuditEntry is the minimal audit payload produced by the access module.
type AuditEntry struct {
	ActorID       *uuid.UUID
	ActorUsername string
	Action        string
	Resource      string
	ResourceID    string
	Status        string
	IPAddress     string
	UserAgent     string
	Metadata      map[string]any
}

// ActorResolver resolves a user's username for audit snapshots without importing the identity module.
type ActorResolver interface {
	GetUsername(ctx context.Context, userID uuid.UUID) (string, error)
}

package access

import (
	"time"

	"github.com/google/uuid"

	"tianshu/internal/shared/pagination"
)

// UserAccess represents the resolved roles and permissions for a single user.
type UserAccess struct {
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

// PermissionDef describes a declarative permission entry and its i18n keys.
type PermissionDef struct {
	Code           string
	Module         string
	NameKey        string
	DescriptionKey string
}

// PermissionGroupDef describes a module grouping of declarative permissions.
type PermissionGroupDef struct {
	Module      string
	LabelKey    string
	Permissions []PermissionDef
}

// SystemRoleDef describes a built-in role seeded at startup.
type SystemRoleDef struct {
	Code           string
	NameKey        string
	DescriptionKey string
	AllPermissions bool
}

// Role represents a domain role aggregate with counts and assigned permission codes.
type Role struct {
	ID              uuid.UUID
	Code            string
	Name            string
	Description     string
	NameKey         string
	DescriptionKey  string
	IsSystem        bool
	UserCount       int64
	PermissionCount int64
	Permissions     []string
	CreatedBy       *uuid.UUID
	UpdatedBy       *uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// RoleListFilter defines query constraints for paginated role listing.
type RoleListFilter struct {
	Pagination pagination.Params
	Q          string
	IsSystem   *bool
	Sort       string
	Order      string
}

// RequestMeta carries caller network metadata for audit records.
type RequestMeta struct {
	IPAddress string
	UserAgent string
}

// CreateRoleInput defines the parameters for creating a custom role.
type CreateRoleInput struct {
	Code        string
	Name        string
	Description string
	Permissions []string
	ActorID     *uuid.UUID
	Meta        RequestMeta
}

// UpdateRoleInput defines partial update fields for a custom role.
// Nil pointer fields indicate omitted inputs that preserve existing values.
type UpdateRoleInput struct {
	RoleID      uuid.UUID
	Name        *string
	Description *string
	Permissions *[]string
	ActorID     *uuid.UUID
	Meta        RequestMeta
}

// DeleteRoleInput defines parameters for deleting an unused custom role.
type DeleteRoleInput struct {
	RoleID  uuid.UUID
	ActorID *uuid.UUID
	Meta    RequestMeta
}

// RoleRef is a lightweight role projection used for options and user role summaries.
type RoleRef struct {
	ID       uuid.UUID
	Code     string
	Name     string
	NameKey  string
	IsSystem bool
}

// Menu node types and statuses matching the PostgreSQL CHECK constraints on menus.
const (
	MenuTypeDirectory = "directory"
	MenuTypeMenu      = "menu"
	MenuTypeButton    = "button"

	MenuStatusActive   = "active"
	MenuStatusDisabled = "disabled"
)

// SystemMenuDef describes a built-in navigation menu node seeded at startup.
type SystemMenuDef struct {
	ParentCode     string
	Code           string
	Type           string
	TitleKey       string
	Path           string
	Icon           string
	PermissionCode string
	SortOrder      int32
}

// Menu represents a menu/permission tree node.
type Menu struct {
	ID             uuid.UUID
	ParentID       *uuid.UUID
	Type           string
	Code           string
	Title          string
	TitleKey       string
	Path           string
	Icon           string
	PermissionCode string
	SortOrder      int32
	Visible        bool
	Status         string
	IsSystem       bool
	Children       []Menu
	CreatedBy      *uuid.UUID
	UpdatedBy      *uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateMenuInput defines the parameters for creating a menu node.
type CreateMenuInput struct {
	ParentID       *uuid.UUID
	Type           string
	Code           string
	Title          string
	Path           string
	Icon           string
	PermissionCode string
	SortOrder      int32
	Visible        bool
	Status         string
	ActorID        *uuid.UUID
	Meta           RequestMeta
}

// UpdateMenuInput defines partial update fields for a menu node.
type UpdateMenuInput struct {
	MenuID         uuid.UUID
	ParentID       *uuid.UUID
	ClearParent    bool
	Code           *string
	Type           *string
	Title          *string
	Path           *string
	Icon           *string
	PermissionCode *string
	SortOrder      *int32
	Visible        *bool
	Status         *string
	ActorID        *uuid.UUID
	Meta           RequestMeta
}

// ReorderMenuItem describes a single menu node's new parent and sort order.
type ReorderMenuItem struct {
	ID        uuid.UUID
	ParentID  *uuid.UUID
	SortOrder int32
}

// ReorderMenusInput defines batch reorder parameters for menu nodes.
type ReorderMenusInput struct {
	Items   []ReorderMenuItem
	ActorID *uuid.UUID
	Meta    RequestMeta
}

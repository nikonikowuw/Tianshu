package access

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

// SystemRoleDef describes a built-in role seeded at startup.
type SystemRoleDef struct {
	Code           string
	NameKey        string
	DescriptionKey string
	AllPermissions bool
}

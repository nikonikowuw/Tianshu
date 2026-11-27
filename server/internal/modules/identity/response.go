package identity

import (
	"time"

	"tianshu/internal/platform/i18n"
	"tianshu/internal/shared/pagination"
)

type roleRefResponse struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	IsSystem bool   `json:"isSystem"`
}

type actorRefResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type userResponse struct {
	ID          string            `json:"id"`
	Username    string            `json:"username"`
	Email       string            `json:"email"`
	DisplayName string            `json:"displayName"`
	Status      string            `json:"status"`
	LastLoginAt *string           `json:"lastLoginAt"`
	Roles       []roleRefResponse `json:"roles"`
	CreatedBy   *actorRefResponse `json:"createdBy"`
	UpdatedBy   *actorRefResponse `json:"updatedBy"`
	CreatedAt   string            `json:"createdAt"`
	UpdatedAt   string            `json:"updatedAt"`
}

type profileResponse struct {
	ID           string            `json:"id"`
	Username     string            `json:"username"`
	Email        string            `json:"email"`
	DisplayName  string            `json:"displayName"`
	AvatarFileID *string           `json:"avatarFileId"`
	AvatarURL    *string           `json:"avatarUrl"`
	Status       string            `json:"status"`
	LastLoginAt  *string           `json:"lastLoginAt"`
	Roles        []roleRefResponse `json:"roles"`
	CreatedAt    string            `json:"createdAt"`
	UpdatedAt    string            `json:"updatedAt"`
}

type changePasswordResponse struct {
	RevokedSessions int64 `json:"revokedSessions"`
}

type sessionResponse struct {
	ID         string `json:"id"`
	IPAddress  string `json:"ipAddress"`
	UserAgent  string `json:"userAgent"`
	IsCurrent  bool   `json:"isCurrent"`
	LastUsedAt string `json:"lastUsedAt"`
	CreatedAt  string `json:"createdAt"`
	ExpiresAt  string `json:"expiresAt"`
}

type sessionsListResponse struct {
	Items []sessionResponse `json:"items"`
}

type revokeOtherSessionsResponse struct {
	RevokedSessions int64 `json:"revokedSessions"`
}

func toRoleRefResponses(in []RoleRef, locale string, bundle *i18n.Bundle) []roleRefResponse {
	roles := make([]roleRefResponse, 0, len(in))
	for _, r := range in {
		name := r.Name
		if r.IsSystem && r.NameKey != "" {
			name = translateKey(bundle, locale, r.NameKey, r.Code)
		}
		roles = append(roles, roleRefResponse{
			ID:       r.ID.String(),
			Code:     r.Code,
			Name:     name,
			IsSystem: r.IsSystem,
		})
	}
	return roles
}

func toUserResponse(u UserDetail, locale string, bundle *i18n.Bundle) userResponse {
	roles := toRoleRefResponses(u.Roles, locale, bundle)

	var lastLogin *string
	if u.LastLoginAt != nil {
		s := u.LastLoginAt.UTC().Format(time.RFC3339)
		lastLogin = &s
	}

	return userResponse{
		ID:          u.ID.String(),
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Status:      u.Status,
		LastLoginAt: lastLogin,
		Roles:       roles,
		CreatedBy:   toActorRefResponse(u.CreatedBy),
		UpdatedBy:   toActorRefResponse(u.UpdatedBy),
		CreatedAt:   u.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   u.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toProfileResponse(p Profile, locale string, bundle *i18n.Bundle) profileResponse {
	var avatarFileID *string
	if p.AvatarFileID != nil {
		s := p.AvatarFileID.String()
		avatarFileID = &s
	}
	var lastLogin *string
	if p.LastLoginAt != nil {
		s := p.LastLoginAt.UTC().Format(time.RFC3339)
		lastLogin = &s
	}
	return profileResponse{
		ID:           p.ID.String(),
		Username:     p.Username,
		Email:        p.Email,
		DisplayName:  p.DisplayName,
		AvatarFileID: avatarFileID,
		AvatarURL:    p.AvatarURL,
		Status:       p.Status,
		LastLoginAt:  lastLogin,
		Roles:        toRoleRefResponses(p.Roles, locale, bundle),
		CreatedAt:    p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toSessionsListResponse(sessions []UserSession) sessionsListResponse {
	items := make([]sessionResponse, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, sessionResponse{
			ID:         s.ID.String(),
			IPAddress:  s.IPAddress,
			UserAgent:  s.UserAgent,
			IsCurrent:  s.IsCurrent,
			LastUsedAt: s.LastUsedAt.UTC().Format(time.RFC3339),
			CreatedAt:  s.CreatedAt.UTC().Format(time.RFC3339),
			ExpiresAt:  s.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
	return sessionsListResponse{Items: items}
}

func toListUsersResponse(res pagination.Result[UserDetail], locale string, bundle *i18n.Bundle) pagination.Result[userResponse] {
	items := make([]userResponse, 0, len(res.Items))
	for _, u := range res.Items {
		items = append(items, toUserResponse(u, locale, bundle))
	}
	return pagination.NewResult(items, res.Total, pagination.Params{
		Page:     res.Page,
		PageSize: res.PageSize,
	})
}

func toActorRefResponse(ref *ActorRef) *actorRefResponse {
	if ref == nil {
		return nil
	}
	return &actorRefResponse{
		ID:       ref.ID.String(),
		Username: ref.Username,
	}
}

func translateKey(bundle *i18n.Bundle, locale, key, fallback string) string {
	if bundle == nil || key == "" {
		return fallback
	}
	translated := bundle.Translate(locale, key, nil)
	if translated == key && fallback != "" {
		return fallback
	}
	return translated
}

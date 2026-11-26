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

func toUserResponse(u UserDetail, locale string, bundle *i18n.Bundle) userResponse {
	roles := make([]roleRefResponse, 0, len(u.Roles))
	for _, r := range u.Roles {
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

package access

import (
	"time"

	"github.com/google/uuid"

	"tianshu/internal/platform/i18n"
	"tianshu/internal/shared/pagination"
)

type permissionResponse struct {
	Code        string `json:"code"`
	Module      string `json:"module"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type permissionGroupResponse struct {
	Module      string               `json:"module"`
	Label       string               `json:"label"`
	Permissions []permissionResponse `json:"permissions"`
}

type listPermissionsResponse struct {
	Groups []permissionGroupResponse `json:"groups"`
}

type roleOptionResponse struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	IsSystem bool   `json:"isSystem"`
}

type listRoleOptionsResponse struct {
	Items []roleOptionResponse `json:"items"`
}

type menuResponse struct {
	ID             string         `json:"id"`
	ParentID       *string        `json:"parentId"`
	Type           string         `json:"type"`
	Code           string         `json:"code"`
	Title          string         `json:"title"`
	Path           string         `json:"path"`
	Icon           string         `json:"icon"`
	PermissionCode string         `json:"permissionCode"`
	SortOrder      int32          `json:"sortOrder"`
	Visible        bool           `json:"visible"`
	Status         string         `json:"status"`
	IsSystem       bool           `json:"isSystem"`
	Children       []menuResponse `json:"children"`
	CreatedBy      *string        `json:"createdBy"`
	UpdatedBy      *string        `json:"updatedBy"`
	CreatedAt      string         `json:"createdAt"`
	UpdatedAt      string         `json:"updatedAt"`
}

type listMenuTreeResponse struct {
	Items []menuResponse `json:"items"`
}

type roleResponse struct {
	ID              string   `json:"id"`
	Code            string   `json:"code"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	IsSystem        bool     `json:"isSystem"`
	UserCount       int64    `json:"userCount"`
	PermissionCount int64    `json:"permissionCount"`
	Permissions     []string `json:"permissions"`
	CreatedBy       *string  `json:"createdBy"`
	UpdatedBy       *string  `json:"updatedBy"`
	CreatedAt       string   `json:"createdAt"`
	UpdatedAt       string   `json:"updatedAt"`
}

func toListPermissionsResponse(groups []PermissionGroupDef, locale string, bundle *i18n.Bundle) listPermissionsResponse {
	out := make([]permissionGroupResponse, 0, len(groups))
	for _, g := range groups {
		perms := make([]permissionResponse, 0, len(g.Permissions))
		for _, p := range g.Permissions {
			perms = append(perms, permissionResponse{
				Code:        p.Code,
				Module:      p.Module,
				Name:        translateKey(bundle, locale, p.NameKey, p.Code),
				Description: translateKey(bundle, locale, p.DescriptionKey, ""),
			})
		}
		out = append(out, permissionGroupResponse{
			Module:      g.Module,
			Label:       translateKey(bundle, locale, g.LabelKey, g.Module),
			Permissions: perms,
		})
	}
	return listPermissionsResponse{Groups: out}
}

func toRoleResponse(role Role, locale string, bundle *i18n.Bundle) roleResponse {
	name := role.Name
	desc := role.Description
	if role.IsSystem {
		if role.NameKey != "" {
			name = translateKey(bundle, locale, role.NameKey, role.Code)
		}
		if role.DescriptionKey != "" {
			desc = translateKey(bundle, locale, role.DescriptionKey, "")
		}
	}

	perms := role.Permissions
	if perms == nil {
		perms = make([]string, 0)
	}

	return roleResponse{
		ID:              role.ID.String(),
		Code:            role.Code,
		Name:            name,
		Description:     desc,
		IsSystem:        role.IsSystem,
		UserCount:       role.UserCount,
		PermissionCount: role.PermissionCount,
		Permissions:     perms,
		CreatedBy:       uuidPtrToStringPtr(role.CreatedBy),
		UpdatedBy:       uuidPtrToStringPtr(role.UpdatedBy),
		CreatedAt:       role.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       role.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toListRolesResponse(res pagination.Result[Role], locale string, bundle *i18n.Bundle) pagination.Result[roleResponse] {
	items := make([]roleResponse, 0, len(res.Items))
	for _, item := range res.Items {
		items = append(items, toRoleResponse(item, locale, bundle))
	}
	return pagination.NewResult(items, res.Total, pagination.Params{
		Page:     res.Page,
		PageSize: res.PageSize,
	})
}

func toListRoleOptionsResponse(refs []RoleRef, locale string, bundle *i18n.Bundle) listRoleOptionsResponse {
	items := make([]roleOptionResponse, 0, len(refs))
	for _, r := range refs {
		name := r.Name
		if r.IsSystem && r.NameKey != "" {
			name = translateKey(bundle, locale, r.NameKey, r.Code)
		}
		items = append(items, roleOptionResponse{
			ID:       r.ID.String(),
			Code:     r.Code,
			Name:     name,
			IsSystem: r.IsSystem,
		})
	}
	return listRoleOptionsResponse{Items: items}
}

func toMenuResponse(m Menu, locale string, bundle *i18n.Bundle) menuResponse {
	title := m.Title
	if title == "" && m.TitleKey != "" {
		title = translateKey(bundle, locale, m.TitleKey, m.Code)
	}
	children := make([]menuResponse, 0, len(m.Children))
	for _, ch := range m.Children {
		children = append(children, toMenuResponse(ch, locale, bundle))
	}
	return menuResponse{
		ID:             m.ID.String(),
		ParentID:       uuidPtrToStringPtr(m.ParentID),
		Type:           m.Type,
		Code:           m.Code,
		Title:          title,
		Path:           m.Path,
		Icon:           m.Icon,
		PermissionCode: m.PermissionCode,
		SortOrder:      m.SortOrder,
		Visible:        m.Visible,
		Status:         m.Status,
		IsSystem:       m.IsSystem,
		Children:       children,
		CreatedBy:      uuidPtrToStringPtr(m.CreatedBy),
		UpdatedBy:      uuidPtrToStringPtr(m.UpdatedBy),
		CreatedAt:      m.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      m.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toMenuTreeResponse(menus []Menu, locale string, bundle *i18n.Bundle) listMenuTreeResponse {
	items := make([]menuResponse, 0, len(menus))
	for _, m := range menus {
		items = append(items, toMenuResponse(m, locale, bundle))
	}
	return listMenuTreeResponse{Items: items}
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

func uuidPtrToStringPtr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

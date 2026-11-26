package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"tianshu/internal/platform/database"
	"tianshu/internal/shared/apperr"
)

const (
	// menuTreeAdvisoryLockKey serializes concurrent menu tree structure mutations.
	menuTreeAdvisoryLockKey = int64(0x7469616e73687503)

	auditResourceMenu      = "menu"
	auditActionMenuCreate  = "menu.create"
	auditActionMenuUpdate  = "menu.update"
	auditActionMenuReorder = "menu.reorder"
	auditActionMenuDelete  = "menu.delete"

	maxMenuTitleRunes = 100
	maxMenuPathLen    = 255
)

func (s *Service) syncSystemMenus(ctx context.Context) error {
	idByCode := make(map[string]uuid.UUID)
	for _, def := range SystemMenus() {
		newID, err := uuid.NewV7()
		if err != nil {
			return apperr.Internal(fmt.Errorf("generate menu uuid: %w", err))
		}
		var parentID *uuid.UUID
		if def.ParentCode != "" {
			if pid, ok := idByCode[def.ParentCode]; ok {
				parentID = &pid
			}
		}
		savedID, err := s.store.EnsureSystemMenu(ctx, newID, parentID, def)
		if err != nil {
			return apperr.Internal(fmt.Errorf("ensure system menu %s: %w", def.Code, err))
		}
		idByCode[def.Code] = savedID
	}
	return nil
}

// ListMenuTree returns the full administrative menu tree sorted by sortOrder and createdAt.
func (s *Service) ListMenuTree(ctx context.Context) ([]Menu, error) {
	items, err := s.store.ListMenus(ctx)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("list menus: %w", err))
	}
	return buildMenuTree(items), nil
}

// ListUserMenuTree returns the active, visible navigation tree filtered by userID's permissions.
func (s *Service) ListUserMenuTree(ctx context.Context, userID uuid.UUID) ([]Menu, error) {
	ua, err := s.GetUserAccess(ctx, userID)
	if err != nil {
		return nil, err
	}
	permSet := make(map[string]struct{}, len(ua.Permissions))
	for _, p := range ua.Permissions {
		permSet[p] = struct{}{}
	}

	items, err := s.store.ListMenus(ctx)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("list menus for user: %w", err))
	}
	tree := buildMenuTree(items)
	return filterUserMenuTree(tree, permSet), nil
}

// CreateMenu creates a custom directory, menu, or button node and writes an audit record.
func (s *Service) CreateMenu(ctx context.Context, input CreateMenuInput) (Menu, error) {
	code := strings.ToLower(strings.TrimSpace(input.Code))
	title := strings.TrimSpace(input.Title)
	path := strings.TrimSpace(input.Path)
	icon := strings.TrimSpace(input.Icon)
	permCode := strings.TrimSpace(input.PermissionCode)
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = MenuStatusActive
	}

	var fieldErrs []apperr.FieldError
	if len(code) < minRoleCodeLen || len(code) > maxRoleCodeLen || !roleCodePattern.MatchString(code) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "code", Message: ErrCodeInvalidRoleCode})
	}
	if !isValidMenuType(input.Type) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "type", Message: ErrCodeInvalidMenuType})
	}
	if n := utf8.RuneCountInString(title); n < 1 || n > maxMenuTitleRunes {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "title", Message: ErrCodeInvalidMenuTitle})
	}
	if input.Type == MenuTypeMenu && (path == "" || !strings.HasPrefix(path, "/") || len(path) > maxMenuPathLen) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "path", Message: ErrCodeInvalidMenuPath})
	} else if path != "" && (!strings.HasPrefix(path, "/") || len(path) > maxMenuPathLen) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "path", Message: ErrCodeInvalidMenuPath})
	}
	if status != MenuStatusActive && status != MenuStatusDisabled {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "status", Message: ErrCodeInvalidMenuStatus})
	}
	if permCode != "" && !IsValidPermissionCode(permCode) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "permissionCode", Message: ErrCodeInvalidPermissionCode})
	}
	if len(fieldErrs) > 0 {
		return Menu{}, apperr.Validation(sortAndDedupeFieldErrors(fieldErrs))
	}

	menuID, err := uuid.NewV7()
	if err != nil {
		return Menu{}, apperr.Internal(fmt.Errorf("generate menu uuid: %w", err))
	}

	var created Menu
	err = s.withinTx(ctx, func(txCtx context.Context) error {
		if lockErr := database.AcquireAdvisoryXactLock(txCtx, menuTreeAdvisoryLockKey); lockErr != nil {
			return apperr.Internal(lockErr)
		}
		if input.ParentID != nil && *input.ParentID != uuid.Nil {
			parent, pErr := s.store.GetMenuByID(txCtx, *input.ParentID)
			if errors.Is(pErr, errMenuNotFound) {
				return apperr.Validation([]apperr.FieldError{{Field: "parentId", Message: ErrCodeMenuInvalidParent}})
			}
			if pErr != nil {
				return apperr.Internal(fmt.Errorf("lookup parent menu: %w", pErr))
			}
			if !isParentTypeCompatible(parent.Type, input.Type) {
				return apperr.Validation([]apperr.FieldError{{Field: "parentId", Message: ErrCodeMenuInvalidParent}})
			}
		}

		saved, cErr := s.store.CreateMenu(txCtx, createMenuRecord{
			ID:             menuID,
			ParentID:       normalizeOptionalUUID(input.ParentID),
			Type:           input.Type,
			Code:           code,
			Title:          title,
			Path:           path,
			Icon:           icon,
			PermissionCode: permCode,
			SortOrder:      input.SortOrder,
			Visible:        input.Visible,
			Status:         status,
			ActorID:        input.ActorID,
		})
		if errors.Is(cErr, errMenuCodeConflict) {
			return menuCodeConflictError()
		}
		if cErr != nil {
			return apperr.Internal(fmt.Errorf("create menu %s: %w", code, cErr))
		}
		created = saved
		return s.recordMenuAudit(txCtx, input.ActorID, input.Meta, auditActionMenuCreate, saved.ID.String(), map[string]any{
			"code": saved.Code,
			"type": saved.Type,
		})
	})
	if err != nil {
		return Menu{}, err
	}
	return created, nil
}

// UpdateMenu updates a menu node's metadata, parent, sort order, visibility, or status.
func (s *Service) UpdateMenu(ctx context.Context, input UpdateMenuInput) (Menu, error) {
	var fieldErrs []apperr.FieldError
	if input.Type != nil && !isValidMenuType(*input.Type) {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "type", Message: ErrCodeInvalidMenuType})
	}
	if input.Title != nil {
		if n := utf8.RuneCountInString(strings.TrimSpace(*input.Title)); n < 1 || n > maxMenuTitleRunes {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "title", Message: ErrCodeInvalidMenuTitle})
		}
	}
	if input.Path != nil {
		p := strings.TrimSpace(*input.Path)
		if p != "" && (!strings.HasPrefix(p, "/") || len(p) > maxMenuPathLen) {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "path", Message: ErrCodeInvalidMenuPath})
		}
	}
	if input.Status != nil && *input.Status != MenuStatusActive && *input.Status != MenuStatusDisabled {
		fieldErrs = append(fieldErrs, apperr.FieldError{Field: "status", Message: ErrCodeInvalidMenuStatus})
	}
	if input.PermissionCode != nil {
		pc := strings.TrimSpace(*input.PermissionCode)
		if pc != "" && !IsValidPermissionCode(pc) {
			fieldErrs = append(fieldErrs, apperr.FieldError{Field: "permissionCode", Message: ErrCodeInvalidPermissionCode})
		}
	}
	if len(fieldErrs) > 0 {
		return Menu{}, apperr.Validation(sortAndDedupeFieldErrors(fieldErrs))
	}

	var updated Menu
	err := s.withinTx(ctx, func(txCtx context.Context) error {
		if lockErr := database.AcquireAdvisoryXactLock(txCtx, menuTreeAdvisoryLockKey); lockErr != nil {
			return apperr.Internal(lockErr)
		}
		existing, lockErr := s.store.GetMenuByIDForUpdate(txCtx, input.MenuID)
		if errors.Is(lockErr, errMenuNotFound) {
			return menuNotFoundError()
		}
		if lockErr != nil {
			return apperr.Internal(fmt.Errorf("lock menu %s: %w", input.MenuID, lockErr))
		}
		if existing.IsSystem && input.Type != nil && *input.Type != existing.Type {
			return menuSystemImmutableError()
		}

		finalType := existing.Type
		if input.Type != nil {
			finalType = *input.Type
		}
		finalTitle := existing.Title
		if input.Title != nil {
			finalTitle = strings.TrimSpace(*input.Title)
		}
		finalPath := existing.Path
		if input.Path != nil {
			finalPath = strings.TrimSpace(*input.Path)
		}
		if finalType == MenuTypeMenu && (finalPath == "" || !strings.HasPrefix(finalPath, "/")) {
			return apperr.Validation([]apperr.FieldError{{Field: "path", Message: ErrCodeInvalidMenuPath}})
		}
		finalIcon := existing.Icon
		if input.Icon != nil {
			finalIcon = strings.TrimSpace(*input.Icon)
		}
		finalPerm := existing.PermissionCode
		if input.PermissionCode != nil {
			finalPerm = strings.TrimSpace(*input.PermissionCode)
		}
		finalSort := existing.SortOrder
		if input.SortOrder != nil {
			finalSort = *input.SortOrder
		}
		finalVisible := existing.Visible
		if input.Visible != nil {
			finalVisible = *input.Visible
		}
		finalStatus := existing.Status
		if input.Status != nil {
			finalStatus = *input.Status
		}

		finalParent := existing.ParentID
		if input.ClearParent {
			finalParent = nil
		} else if input.ParentID != nil {
			finalParent = normalizeOptionalUUID(input.ParentID)
		}

		if finalParent != nil || finalType != existing.Type {
			all, listErr := s.store.ListMenus(txCtx)
			if listErr != nil {
				return apperr.Internal(fmt.Errorf("list menus for tree validation: %w", listErr))
			}
			byID := indexMenusByID(all)

			if finalType != existing.Type {
				for _, child := range all {
					if child.ParentID != nil && *child.ParentID == existing.ID && !isParentTypeCompatible(finalType, child.Type) {
						return apperr.Validation([]apperr.FieldError{{Field: "type", Message: ErrCodeMenuInvalidParent}})
					}
				}
			}

			if finalParent != nil {
				if *finalParent == existing.ID {
					return menuCycleDetectedError()
				}
				parentNode, ok := byID[*finalParent]
				if !ok {
					return apperr.Validation([]apperr.FieldError{{Field: "parentId", Message: ErrCodeMenuInvalidParent}})
				}
				if createsCycle(existing.ID, *finalParent, byID) {
					return menuCycleDetectedError()
				}
				if !isParentTypeCompatible(parentNode.Type, finalType) {
					return apperr.Validation([]apperr.FieldError{{Field: "parentId", Message: ErrCodeMenuInvalidParent}})
				}
			}
		}

		saved, uErr := s.store.UpdateMenu(txCtx, updateMenuRecord{
			ID:             existing.ID,
			ParentID:       finalParent,
			Type:           finalType,
			Title:          finalTitle,
			Path:           finalPath,
			Icon:           finalIcon,
			PermissionCode: finalPerm,
			SortOrder:      finalSort,
			Visible:        finalVisible,
			Status:         finalStatus,
			ActorID:        input.ActorID,
		})
		if uErr != nil {
			return apperr.Internal(fmt.Errorf("update menu %s: %w", existing.ID, uErr))
		}
		updated = saved
		return s.recordMenuAudit(txCtx, input.ActorID, input.Meta, auditActionMenuUpdate, saved.ID.String(), map[string]any{
			"code": saved.Code,
		})
	})
	if err != nil {
		return Menu{}, err
	}
	return updated, nil
}

// ReorderMenus batch-updates parent_id and sort_order for menu nodes inside one transaction.
func (s *Service) ReorderMenus(ctx context.Context, input ReorderMenusInput) ([]Menu, error) {
	if len(input.Items) == 0 {
		return s.ListMenuTree(ctx)
	}

	var result []Menu
	err := s.withinTx(ctx, func(txCtx context.Context) error {
		if lockErr := database.AcquireAdvisoryXactLock(txCtx, menuTreeAdvisoryLockKey); lockErr != nil {
			return apperr.Internal(lockErr)
		}
		all, err := s.store.ListMenus(txCtx)
		if err != nil {
			return apperr.Internal(fmt.Errorf("list menus for reorder: %w", err))
		}
		byID := indexMenusByID(all)
		seenIDs := make(map[uuid.UUID]struct{}, len(input.Items))

		for _, item := range input.Items {
			if _, dup := seenIDs[item.ID]; dup {
				return apperr.Validation([]apperr.FieldError{{Field: "items", Message: ErrCodeMenuInvalidParent}})
			}
			seenIDs[item.ID] = struct{}{}
			node, ok := byID[item.ID]
			if !ok {
				return menuNotFoundError()
			}
			pid := normalizeOptionalUUID(item.ParentID)
			if pid != nil {
				if *pid == item.ID {
					return menuCycleDetectedError()
				}
				pNode, pOK := byID[*pid]
				if !pOK || !isParentTypeCompatible(pNode.Type, node.Type) {
					return apperr.Validation([]apperr.FieldError{{Field: "items", Message: ErrCodeMenuInvalidParent}})
				}
			}
			node.ParentID = pid
			node.SortOrder = item.SortOrder
			byID[item.ID] = node
		}

		for id := range byID {
			if createsCycle(id, derefUUID(byID[id].ParentID), byID) {
				return menuCycleDetectedError()
			}
		}

		for _, item := range input.Items {
			if err := s.store.UpdateMenuOrder(txCtx, item.ID, normalizeOptionalUUID(item.ParentID), item.SortOrder, input.ActorID); err != nil {
				return apperr.Internal(fmt.Errorf("reorder menu %s: %w", item.ID, err))
			}
		}

		if err := s.recordMenuAudit(txCtx, input.ActorID, input.Meta, auditActionMenuReorder, "", map[string]any{
			"count": len(input.Items),
		}); err != nil {
			return err
		}

		updatedAll, err := s.store.ListMenus(txCtx)
		if err != nil {
			return apperr.Internal(fmt.Errorf("reload menu tree: %w", err))
		}
		result = buildMenuTree(updatedAll)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteMenu deletes a custom leaf menu node after checking system immutability and child count.
func (s *Service) DeleteMenu(ctx context.Context, menuID uuid.UUID, actorID *uuid.UUID, meta RequestMeta) error {
	return s.withinTx(ctx, func(txCtx context.Context) error {
		if lockErr := database.AcquireAdvisoryXactLock(txCtx, menuTreeAdvisoryLockKey); lockErr != nil {
			return apperr.Internal(lockErr)
		}
		existing, lockErr := s.store.GetMenuByIDForUpdate(txCtx, menuID)
		if errors.Is(lockErr, errMenuNotFound) {
			return menuNotFoundError()
		}
		if lockErr != nil {
			return apperr.Internal(fmt.Errorf("lock menu %s for delete: %w", menuID, lockErr))
		}
		if existing.IsSystem {
			return menuSystemImmutableError()
		}
		children, countErr := s.store.CountMenuChildren(txCtx, existing.ID)
		if countErr != nil {
			return apperr.Internal(fmt.Errorf("count menu children %s: %w", existing.ID, countErr))
		}
		if children > 0 {
			return menuHasChildrenError()
		}
		if err := s.store.DeleteMenu(txCtx, existing.ID); err != nil {
			return apperr.Internal(fmt.Errorf("delete menu %s: %w", existing.ID, err))
		}
		return s.recordMenuAudit(txCtx, actorID, meta, auditActionMenuDelete, existing.ID.String(), map[string]any{
			"code": existing.Code,
		})
	})
}

func (s *Service) recordMenuAudit(
	ctx context.Context,
	actorID *uuid.UUID,
	meta RequestMeta,
	action string,
	resourceID string,
	metadata map[string]any,
) error {
	if s.audit == nil {
		return nil
	}
	actorUsername := ""
	if actorID != nil && *actorID != uuid.Nil && s.actors != nil {
		if username, err := s.actors.GetUsername(ctx, *actorID); err == nil {
			actorUsername = username
		}
	}
	if err := s.audit.Record(ctx, AuditEntry{
		ActorID:       actorID,
		ActorUsername: actorUsername,
		Action:        action,
		Resource:      auditResourceMenu,
		ResourceID:    resourceID,
		Status:        auditStatusSuccess,
		IPAddress:     meta.IPAddress,
		UserAgent:     meta.UserAgent,
		Metadata:      metadata,
	}); err != nil {
		return apperr.Internal(fmt.Errorf("record menu audit %s: %w", action, err))
	}
	return nil
}

func buildMenuTree(flat []Menu) []Menu {
	byID := make(map[uuid.UUID]*Menu, len(flat))
	order := make([]uuid.UUID, 0, len(flat))
	for i := range flat {
		m := flat[i]
		m.Children = make([]Menu, 0)
		byID[m.ID] = &m
		order = append(order, m.ID)
	}

	roots := make([]Menu, 0)
	childrenMap := make(map[uuid.UUID][]Menu)
	for _, id := range order {
		node := byID[id]
		if node.ParentID != nil {
			if _, ok := byID[*node.ParentID]; ok {
				childrenMap[*node.ParentID] = append(childrenMap[*node.ParentID], *node)
				continue
			}
		}
		roots = append(roots, *node)
	}

	var populate func(n Menu) Menu
	populate = func(n Menu) Menu {
		kids := childrenMap[n.ID]
		outKids := make([]Menu, 0, len(kids))
		for _, k := range kids {
			outKids = append(outKids, populate(k))
		}
		n.Children = outKids
		return n
	}

	out := make([]Menu, 0, len(roots))
	for _, r := range roots {
		out = append(out, populate(r))
	}
	return out
}

func filterUserMenuTree(nodes []Menu, permSet map[string]struct{}) []Menu {
	out := make([]Menu, 0, len(nodes))
	for _, n := range nodes {
		if n.Status != MenuStatusActive || !n.Visible || n.Type == MenuTypeButton {
			continue
		}
		if n.PermissionCode != "" {
			if _, ok := permSet[n.PermissionCode]; !ok {
				continue
			}
		}
		filteredChildren := filterUserMenuTree(n.Children, permSet)
		if n.Type == MenuTypeDirectory && len(filteredChildren) == 0 {
			continue
		}
		n.Children = filteredChildren
		out = append(out, n)
	}
	return out
}

func isValidMenuType(t string) bool {
	return t == MenuTypeDirectory || t == MenuTypeMenu || t == MenuTypeButton
}

func isParentTypeCompatible(parentType, childType string) bool {
	switch parentType {
	case MenuTypeDirectory:
		return childType == MenuTypeDirectory || childType == MenuTypeMenu
	case MenuTypeMenu:
		return childType == MenuTypeMenu || childType == MenuTypeButton
	default:
		return false
	}
}

func indexMenusByID(items []Menu) map[uuid.UUID]Menu {
	m := make(map[uuid.UUID]Menu, len(items))
	for _, item := range items {
		m[item.ID] = item
	}
	return m
}

func createsCycle(nodeID, parentID uuid.UUID, byID map[uuid.UUID]Menu) bool {
	if parentID == uuid.Nil {
		return false
	}
	visited := map[uuid.UUID]struct{}{nodeID: {}}
	curr := parentID
	for curr != uuid.Nil {
		if _, seen := visited[curr]; seen {
			return true
		}
		visited[curr] = struct{}{}
		parent, ok := byID[curr]
		if !ok || parent.ParentID == nil {
			return false
		}
		curr = *parent.ParentID
	}
	return false
}

func normalizeOptionalUUID(id *uuid.UUID) *uuid.UUID {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	v := *id
	return &v
}

func derefUUID(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

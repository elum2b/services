package admin

import (
	"context"
	"strings"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/control/repository"
	"github.com/google/uuid"
)

func (a *Admin) CreateAccount(ctx context.Context, id, displayName string) (AccountModel, error) {
	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		MethodKey:  "control.account.create",
		TargetType: "account",
		TargetID:   strings.TrimSpace(id),
	})
	defer cancel()
	account, err := a.repository.CreateAccount(mergedCtx, strings.TrimSpace(id), strings.TrimSpace(displayName))
	return mapAccount(account), err
}

func (a *Admin) CreateWorkspace(ctx context.Context, params CreateWorkspaceParams) (WorkspaceModel, error) {
	if strings.TrimSpace(params.ID) == "" {
		params.ID = uuid.NewString()
	}
	if err := services.ValidateWorkspaceID(params.ID); err != nil {
		return WorkspaceModel{}, err
	}

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		WorkspaceID: params.ID,
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   "control.workspace.create",
		TargetType:  "workspace",
		TargetID:    params.ID,
	})
	defer cancel()
	workspace, err := a.repository.CreateWorkspace(
		mergedCtx,
		params.ID,
		strings.ToLower(strings.TrimSpace(params.Slug)),
		strings.TrimSpace(params.Title),
		strings.TrimSpace(params.ActorID),
	)
	return mapWorkspace(workspace), err
}

func (a *Admin) GetWorkspace(ctx context.Context, workspaceID string) (WorkspaceModel, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return WorkspaceModel{}, err
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	workspace, err := a.repository.GetWorkspace(mergedCtx, workspaceID)
	return mapWorkspace(workspace), err
}

func (a *Admin) UpdateWorkspace(ctx context.Context, params UpdateWorkspaceParams) (int64, error) {
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return 0, err
	}

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		WorkspaceID: params.WorkspaceID,
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   "control.workspace.update",
		TargetType:  "workspace",
		TargetID:    params.WorkspaceID,
	})
	defer cancel()
	return a.repository.UpdateWorkspace(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		params.WorkspaceID,
		strings.ToLower(strings.TrimSpace(params.Slug)),
		strings.TrimSpace(params.Title),
		strings.TrimSpace(params.Status),
	)
}

func (a *Admin) ListWorkspaces(ctx context.Context, accountID string, page Page) ([]WorkspaceModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	limit, offset := normalizePage(page)
	items, err := a.repository.ListWorkspaces(mergedCtx, strings.TrimSpace(accountID), limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]WorkspaceModel, 0, len(items))
	for _, item := range items {
		result = append(result, mapWorkspace(item))
	}
	return result, nil
}

func (a *Admin) CreateRole(ctx context.Context, params CreateRoleParams) (RoleModel, error) {
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return RoleModel{}, err
	}

	if strings.TrimSpace(params.ID) == "" {
		params.ID = uuid.NewString()
	}
	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		WorkspaceID: params.WorkspaceID,
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   "control.role.create",
		TargetType:  "role",
		TargetID:    params.ID,
	})
	defer cancel()
	role, err := a.repository.CreateRole(mergedCtx, strings.TrimSpace(params.ActorID), repository.Role{
		ID:          params.ID,
		WorkspaceID: params.WorkspaceID,
		Code:        strings.ToLower(strings.TrimSpace(params.Code)),
		Title:       strings.TrimSpace(params.Title),
		Description: strings.TrimSpace(params.Description),
		Position:    params.Position,
	})
	return mapRole(role), err
}

func (a *Admin) ListRoles(ctx context.Context, workspaceID string) ([]RoleModel, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	items, err := a.repository.ListRoles(mergedCtx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make([]RoleModel, 0, len(items))
	for _, item := range items {
		result = append(result, mapRole(item))
	}
	return result, nil
}

func (a *Admin) UpdateRole(ctx context.Context, params UpdateRoleParams) (int64, error) {
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return 0, err
	}

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		WorkspaceID: params.WorkspaceID,
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   "control.role.update",
		TargetType:  "role",
		TargetID:    strings.TrimSpace(params.ID),
	})
	defer cancel()
	return a.repository.UpdateRole(mergedCtx, strings.TrimSpace(params.ActorID), repository.Role{
		ID:          strings.TrimSpace(params.ID),
		WorkspaceID: params.WorkspaceID,
		Title:       strings.TrimSpace(params.Title),
		Description: strings.TrimSpace(params.Description),
		Position:    params.Position,
	})
}

func (a *Admin) DeleteRole(ctx context.Context, actorID, workspaceID, roleID string) (int64, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return 0, err
	}

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		WorkspaceID: workspaceID,
		ActorID:     strings.TrimSpace(actorID),
		MethodKey:   "control.role.delete",
		TargetType:  "role",
		TargetID:    strings.TrimSpace(roleID),
	})
	defer cancel()
	return a.repository.DeleteRole(
		mergedCtx,
		strings.TrimSpace(actorID),
		workspaceID,
		strings.TrimSpace(roleID),
	)
}

func (a *Admin) SetRoleMember(ctx context.Context, params SetRoleMemberParams) error {
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return err
	}

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		WorkspaceID: params.WorkspaceID,
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   "control.role_member.set",
		TargetType:  "account",
		TargetID:    strings.TrimSpace(params.AccountID),
	})
	defer cancel()
	return a.repository.AssignRole(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		params.WorkspaceID,
		strings.TrimSpace(params.AccountID),
		strings.TrimSpace(params.RoleID),
	)
}

func (a *Admin) RemoveRoleMember(ctx context.Context, params SetRoleMemberParams) (int64, error) {
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return 0, err
	}

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		WorkspaceID: params.WorkspaceID,
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   "control.role_member.remove",
		TargetType:  "account",
		TargetID:    strings.TrimSpace(params.AccountID),
	})
	defer cancel()
	return a.repository.RemoveRole(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		params.WorkspaceID,
		strings.TrimSpace(params.AccountID),
		strings.TrimSpace(params.RoleID),
	)
}

func (a *Admin) ListMembers(ctx context.Context, workspaceID string, page Page) ([]MemberModel, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	limit, offset := normalizePage(page)
	items, err := a.repository.ListMembers(mergedCtx, workspaceID, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]MemberModel, 0, len(items))
	for _, item := range items {
		result = append(result, MemberModel{
			WorkspaceID: item.WorkspaceID,
			AccountID:   item.AccountID,
			DisplayName: item.DisplayName,
			Position:    item.Position,
			JoinedAt:    item.JoinedAt,
			UpdatedAt:   item.UpdatedAt,
		})
	}
	return result, nil
}

func (a *Admin) RemoveMember(ctx context.Context, actorID, workspaceID, accountID string) (int64, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return 0, err
	}

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		WorkspaceID: workspaceID,
		ActorID:     strings.TrimSpace(actorID),
		MethodKey:   "control.member.remove",
		TargetType:  "account",
		TargetID:    strings.TrimSpace(accountID),
	})
	defer cancel()
	return a.repository.RemoveMember(
		mergedCtx,
		strings.TrimSpace(actorID),
		workspaceID,
		strings.TrimSpace(accountID),
	)
}

func (a *Admin) CreateInvite(ctx context.Context, params CreateInviteParams) (InviteModel, string, error) {
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return InviteModel{}, "", err
	}

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		WorkspaceID: params.WorkspaceID,
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   "control.invite.create",
		TargetType:  "invite",
	})
	defer cancel()
	item, token, err := a.repository.CreateInvite(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		params.WorkspaceID,
		params.RoleIDs,
		params.ExpiresAt,
		params.MaxUses,
	)
	return mapInvite(item), token, err
}

func (a *Admin) AcceptInvite(ctx context.Context, accountID, token string) (InviteModel, error) {
	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		ActorID:    strings.TrimSpace(accountID),
		MethodKey:  "control.invite.accept",
		TargetType: "invite",
	})
	defer cancel()
	item, err := a.repository.AcceptInvite(mergedCtx, strings.TrimSpace(accountID), strings.TrimSpace(token))
	return mapInvite(item), err
}

func (a *Admin) ListInvites(ctx context.Context, workspaceID string, page Page) ([]InviteModel, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	limit, offset := normalizePage(page)
	items, err := a.repository.ListInvites(mergedCtx, workspaceID, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]InviteModel, 0, len(items))
	for _, item := range items {
		result = append(result, mapInvite(item))
	}
	return result, nil
}

func (a *Admin) RevokeInvite(ctx context.Context, actorID, workspaceID, inviteID string) (int64, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return 0, err
	}

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		WorkspaceID: workspaceID,
		ActorID:     strings.TrimSpace(actorID),
		MethodKey:   "control.invite.revoke",
		TargetType:  "invite",
		TargetID:    strings.TrimSpace(inviteID),
	})
	defer cancel()
	return a.repository.RevokeInvite(
		mergedCtx,
		strings.TrimSpace(actorID),
		workspaceID,
		strings.TrimSpace(inviteID),
	)
}

func (a *Admin) SetRolePermission(ctx context.Context, params SetRolePermissionParams) error {
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return err
	}

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		WorkspaceID: params.WorkspaceID,
		ActorID:     strings.TrimSpace(params.ActorID),
		MethodKey:   "control.role_permission.set",
		TargetType:  "role",
		TargetID:    strings.TrimSpace(params.RoleID),
	})
	defer cancel()
	return a.repository.SetPermission(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		params.WorkspaceID,
		strings.TrimSpace(params.RoleID),
		strings.TrimSpace(params.MethodKey),
		params.Enabled,
	)
}

func (a *Admin) ListRolePermissions(ctx context.Context, workspaceID, roleID string) ([]string, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.ListPermissions(mergedCtx, workspaceID, strings.TrimSpace(roleID))
}

func (a *Admin) ClearRolePermissions(ctx context.Context, actorID, workspaceID, roleID string) (int64, error) {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return 0, err
	}

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		WorkspaceID: workspaceID,
		ActorID:     strings.TrimSpace(actorID),
		MethodKey:   "control.role_permission.clear",
		TargetType:  "role",
		TargetID:    strings.TrimSpace(roleID),
	})
	defer cancel()
	return a.repository.ClearPermissions(
		mergedCtx,
		strings.TrimSpace(actorID),
		workspaceID,
		strings.TrimSpace(roleID),
	)
}

func (a *Admin) ListMethods(ctx context.Context) ([]MethodModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	items, err := a.repository.ListMethods(mergedCtx)
	if err != nil {
		return nil, err
	}
	result := make([]MethodModel, 0, len(items))
	for _, item := range items {
		result = append(result, mapMethod(item))
	}
	return result, nil
}

func (a *Admin) GetMethod(ctx context.Context, methodKey string) (MethodModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetMethod(mergedCtx, strings.TrimSpace(methodKey))
	return mapMethod(value), err
}

func (a *Admin) ListAccess(ctx context.Context, locale string) ([]AccessGroupModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	rows, err := a.repository.ListAccessCatalog(mergedCtx, strings.TrimSpace(locale))
	if err != nil {
		return nil, err
	}
	services := make([]AccessGroupModel, 0)
	for _, row := range rows {
		if len(services) == 0 || services[len(services)-1].Service != row.Service {
			services = append(
				services,
				AccessGroupModel{Service: row.Service, Title: row.ServiceTitle, Description: row.ServiceDescription},
			)
		}
		serviceIndex := len(services) - 1
		if len(services[serviceIndex].Groups) == 0 ||
			services[serviceIndex].Groups[len(services[serviceIndex].Groups)-1].Key != row.GroupKey {
			services[serviceIndex].Groups = append(services[serviceIndex].Groups, AccessGroups{
				Key:         row.GroupKey,
				Title:       row.GroupTitle,
				Description: row.GroupDescription,
			})
		}
		groupIndex := len(services[serviceIndex].Groups) - 1
		services[serviceIndex].Groups[groupIndex].Accesses = append(
			services[serviceIndex].Groups[groupIndex].Accesses,
			AccessModel{
				Key:   row.Key,
				Title: row.Title,
				Desc:  row.Desc,
			},
		)
	}
	return services, nil
}

func normalizePage(page Page) (int32, int32) {
	if page.Limit <= 0 {
		page.Limit = 100
	}
	if page.Limit > 1000 {
		page.Limit = 1000
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	return page.Limit, page.Offset
}

func mapAccount(value repository.Account) AccountModel {
	return AccountModel{
		ID:          value.ID,
		DisplayName: value.DisplayName,
		Status:      value.Status,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}
}

func mapSession(value repository.Session) SessionModel {
	return SessionModel{
		ID:         value.ID,
		AccountID:  value.AccountID,
		IP:         value.IP,
		UserAgent:  value.UserAgent,
		BindToIP:   value.BindToIP,
		ExpiresAt:  value.ExpiresAt,
		RevokedAt:  value.RevokedAt,
		LastUsedAt: value.LastUsedAt,
		CreatedAt:  value.CreatedAt,
	}
}

func mapWorkspace(value repository.Workspace) WorkspaceModel {
	return WorkspaceModel{
		ID:        value.ID,
		Slug:      value.Slug,
		Title:     value.Title,
		Status:    value.Status,
		CreatedBy: value.CreatedBy,
		CreatedAt: value.CreatedAt,
		UpdatedAt: value.UpdatedAt,
	}
}

func mapRole(value repository.Role) RoleModel {
	return RoleModel{
		ID:          value.ID,
		WorkspaceID: value.WorkspaceID,
		Code:        value.Code,
		Title:       value.Title,
		Description: value.Description,
		Position:    value.Position,
		IsOwner:     value.IsOwner,
		MemberCount: value.MemberCount,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}
}

func mapInvite(value repository.Invite) InviteModel {
	return InviteModel{
		ID:          value.ID,
		WorkspaceID: value.WorkspaceID,
		CreatedBy:   value.CreatedBy,
		MaxUses:     value.MaxUses,
		UsedCount:   value.UsedCount,
		ExpiresAt:   value.ExpiresAt,
		RevokedAt:   value.RevokedAt,
		CreatedAt:   value.CreatedAt,
		RoleIDs:     append([]string(nil), value.RoleIDs...),
	}
}

func mapMethod(value repository.Method) MethodModel {
	return MethodModel{
		Key:       value.Key,
		Service:   value.Service,
		GroupKey:  value.GroupKey,
		CreatedAt: value.CreatedAt,
		UpdatedAt: value.UpdatedAt,
	}
}

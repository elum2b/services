package admin

import (
	"context"
	"strings"

	services "github.com/elum2b/services"
	controlmodel "github.com/elum2b/services/control/model"
	"github.com/elum2b/services/control/repository"
	"github.com/google/uuid"
)

func (a *Admin) CreateWorkspace(
	ctx context.Context,
	params CreateWorkspaceParams,
) (WorkspaceModel, error) {

	if strings.TrimSpace(params.ID) == "" {
		params.ID = uuid.NewString()
	}
	if err := services.ValidateWorkspaceID(params.ID); err != nil {
		return WorkspaceModel{}, err
	}

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    strings.TrimSpace(params.ActorID),
		MethodKey:  "control.global.workspace.create",
		TargetType: "workspace",
		TargetID:   params.ID,
	})
	defer cancel()

	workspace, err := a.repository.CreateWorkspace(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		params.ID,
		strings.ToLower(strings.TrimSpace(params.Slug)),
		strings.TrimSpace(params.Title),
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

func (a *Admin) ListWorkspaces(
	ctx context.Context,
	accountID string,
	page Page,
) ([]WorkspaceModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	items, err := a.repository.ListWorkspaces(
		mergedCtx,
		strings.TrimSpace(accountID),
		mapCursor(page),
		page.Limit,
	)
	if err != nil {
		return nil, err
	}

	result := make([]WorkspaceModel, 0, len(items))
	for _, item := range items {
		result = append(result, mapWorkspace(item))
	}

	return result, nil

}

func (a *Admin) UpdateWorkspace(
	ctx context.Context,
	params UpdateWorkspaceParams,
) (int64, error) {

	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return 0, err
	}

	mergedCtx, cancel := a.withMutation(ctx, workspaceAudit(
		params.ActorID,
		params.WorkspaceID,
		"control.workspace.update",
		"workspace",
		params.WorkspaceID,
	))
	defer cancel()

	return a.repository.UpdateWorkspace(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		repository.Workspace{
			ID:    params.WorkspaceID,
			Slug:  strings.ToLower(strings.TrimSpace(params.Slug)),
			Title: strings.TrimSpace(params.Title),
		},
	)

}

func (a *Admin) ArchiveWorkspace(
	ctx context.Context,
	actorID string,
	workspaceID string,
) (int64, error) {

	mergedCtx, cancel := a.withMutation(ctx, workspaceAudit(
		actorID,
		workspaceID,
		"control.workspace.archive",
		"workspace",
		workspaceID,
	))
	defer cancel()

	return a.repository.ArchiveWorkspace(
		mergedCtx,
		strings.TrimSpace(actorID),
		workspaceID,
	)

}

func (a *Admin) TransferWorkspaceOwnership(
	ctx context.Context,
	actorID string,
	workspaceID string,
	targetAccountID string,
) error {

	mergedCtx, cancel := a.withMutation(ctx, workspaceAudit(
		actorID,
		workspaceID,
		"control.workspace.owner.transfer",
		"account",
		targetAccountID,
	))
	defer cancel()

	return a.repository.TransferWorkspaceOwnership(
		mergedCtx,
		strings.TrimSpace(actorID),
		workspaceID,
		strings.TrimSpace(targetAccountID),
	)

}

func (a *Admin) ListPlatformMembers(
	ctx context.Context,
	page Page,
) ([]PlatformMemberModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	items, err := a.repository.ListPlatformMembers(mergedCtx, mapCursor(page), page.Limit)
	if err != nil {
		return nil, err
	}

	result := make([]PlatformMemberModel, 0, len(items))
	for _, item := range items {
		result = append(result, PlatformMemberModel{
			AccountID:           item.AccountID,
			DisplayName:         item.DisplayName,
			Status:              item.Status,
			WorkspaceLimit:      item.WorkspaceLimit,
			OwnedWorkspaceCount: item.OwnedWorkspaceCount,
			InvitedBy:           item.InvitedBy,
			JoinedAt:            item.JoinedAt,
			UpdatedAt:           item.UpdatedAt,
		})
	}

	return result, nil

}

func (a *Admin) RemovePlatformMember(
	ctx context.Context,
	actorID string,
	accountID string,
) (int64, error) {

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    strings.TrimSpace(actorID),
		MethodKey:  "control.global.member.remove",
		TargetType: "account",
		TargetID:   strings.TrimSpace(accountID),
	})
	defer cancel()

	return a.repository.RemovePlatformMember(
		mergedCtx,
		strings.TrimSpace(actorID),
		strings.TrimSpace(accountID),
	)

}

func (a *Admin) TransferGlobalOwnership(
	ctx context.Context,
	actorID string,
	targetAccountID string,
) error {

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    strings.TrimSpace(actorID),
		MethodKey:  "control.global.owner.transfer",
		TargetType: "account",
		TargetID:   strings.TrimSpace(targetAccountID),
	})
	defer cancel()

	return a.repository.TransferGlobalOwnership(
		mergedCtx,
		strings.TrimSpace(actorID),
		strings.TrimSpace(targetAccountID),
	)

}

func (a *Admin) CreateGlobalRole(
	ctx context.Context,
	params CreateRoleParams,
) (RoleModel, error) {

	return a.createRole(ctx, params, true)

}

func (a *Admin) CreateWorkspaceRole(
	ctx context.Context,
	params CreateRoleParams,
) (RoleModel, error) {

	return a.createRole(ctx, params, false)

}

func (a *Admin) createRole(
	ctx context.Context,
	params CreateRoleParams,
	global bool,
) (RoleModel, error) {

	if params.ID == "" {
		params.ID = uuid.NewString()
	}
	role := repository.Role{
		ID:          strings.TrimSpace(params.ID),
		WorkspaceID: strings.TrimSpace(params.WorkspaceID),
		Code:        strings.ToLower(strings.TrimSpace(params.Code)),
		Title:       strings.TrimSpace(params.Title),
		Description: strings.TrimSpace(params.Description),
		Position:    params.Position,
	}
	if global {
		mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
			Scope:      repository.ScopeGlobal,
			ActorID:    strings.TrimSpace(params.ActorID),
			MethodKey:  "control.global.role.create",
			TargetType: "role",
			TargetID:   role.ID,
		})
		defer cancel()

		value, err := a.repository.CreateGlobalRole(
			mergedCtx,
			strings.TrimSpace(params.ActorID),
			role,
		)

		return mapRole(value), err
	}

	mergedCtx, cancel := a.withMutation(ctx, workspaceAudit(
		params.ActorID,
		params.WorkspaceID,
		"control.workspace.role.create",
		"role",
		role.ID,
	))
	defer cancel()

	value, err := a.repository.CreateWorkspaceRole(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		role,
	)

	return mapRole(value), err

}

func (a *Admin) ListGlobalRoles(ctx context.Context) ([]RoleModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	items, err := a.repository.ListGlobalRoles(mergedCtx)
	if err != nil {
		return nil, err
	}

	return mapRoles(items), nil

}

func (a *Admin) GetGlobalRole(ctx context.Context, roleID string) (RoleModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	value, err := a.repository.GetGlobalRole(mergedCtx, strings.TrimSpace(roleID))

	return mapRole(value), err

}

func (a *Admin) ListWorkspaceRoles(
	ctx context.Context,
	workspaceID string,
) ([]RoleModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	items, err := a.repository.ListWorkspaceRoles(mergedCtx, workspaceID)
	if err != nil {
		return nil, err
	}

	return mapRoles(items), nil

}

func (a *Admin) GetWorkspaceRole(
	ctx context.Context,
	workspaceID string,
	roleID string,
) (RoleModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	value, err := a.repository.GetWorkspaceRole(
		mergedCtx,
		workspaceID,
		strings.TrimSpace(roleID),
	)

	return mapRole(value), err

}

func (a *Admin) UpdateGlobalRole(
	ctx context.Context,
	params UpdateRoleParams,
) (int64, error) {

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    strings.TrimSpace(params.ActorID),
		MethodKey:  "control.global.role.update",
		TargetType: "role",
		TargetID:   strings.TrimSpace(params.ID),
	})
	defer cancel()

	return a.repository.UpdateGlobalRole(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		mapRoleParams(params),
	)

}

func (a *Admin) UpdateWorkspaceRole(
	ctx context.Context,
	params UpdateRoleParams,
) (int64, error) {

	mergedCtx, cancel := a.withMutation(ctx, workspaceAudit(
		params.ActorID,
		params.WorkspaceID,
		"control.workspace.role.update",
		"role",
		params.ID,
	))
	defer cancel()

	return a.repository.UpdateWorkspaceRole(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		mapRoleParams(params),
	)

}

func (a *Admin) DeleteGlobalRole(
	ctx context.Context,
	actorID string,
	roleID string,
) (int64, error) {

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    strings.TrimSpace(actorID),
		MethodKey:  "control.global.role.delete",
		TargetType: "role",
		TargetID:   strings.TrimSpace(roleID),
	})
	defer cancel()

	return a.repository.DeleteGlobalRole(
		mergedCtx,
		strings.TrimSpace(actorID),
		strings.TrimSpace(roleID),
	)

}

func (a *Admin) DeleteWorkspaceRole(
	ctx context.Context,
	actorID string,
	workspaceID string,
	roleID string,
) (int64, error) {

	mergedCtx, cancel := a.withMutation(ctx, workspaceAudit(
		actorID,
		workspaceID,
		"control.workspace.role.delete",
		"role",
		roleID,
	))
	defer cancel()

	return a.repository.DeleteWorkspaceRole(
		mergedCtx,
		strings.TrimSpace(actorID),
		workspaceID,
		strings.TrimSpace(roleID),
	)

}

func (a *Admin) AssignGlobalRole(ctx context.Context, params SetRoleMemberParams) error {

	return a.changeRoleMember(ctx, params, true, true)

}

func (a *Admin) RemoveGlobalRole(ctx context.Context, params SetRoleMemberParams) error {

	return a.changeRoleMember(ctx, params, true, false)

}

func (a *Admin) AssignWorkspaceRole(ctx context.Context, params SetRoleMemberParams) error {

	return a.changeRoleMember(ctx, params, false, true)

}

func (a *Admin) RemoveWorkspaceRole(ctx context.Context, params SetRoleMemberParams) error {

	return a.changeRoleMember(ctx, params, false, false)

}

func (a *Admin) changeRoleMember(
	ctx context.Context,
	params SetRoleMemberParams,
	global bool,
	assign bool,
) error {

	if global {
		methodKey := "control.global.role.member.remove"
		if assign {
			methodKey = "control.global.role.member.assign"
		}
		mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
			Scope:      repository.ScopeGlobal,
			ActorID:    strings.TrimSpace(params.ActorID),
			MethodKey:  methodKey,
			TargetType: "account",
			TargetID:   strings.TrimSpace(params.AccountID),
		})
		defer cancel()

		if assign {
			return a.repository.AssignGlobalRole(
				mergedCtx,
				strings.TrimSpace(params.ActorID),
				strings.TrimSpace(params.AccountID),
				strings.TrimSpace(params.RoleID),
			)
		}

		return a.repository.RemoveGlobalRole(
			mergedCtx,
			strings.TrimSpace(params.ActorID),
			strings.TrimSpace(params.AccountID),
			strings.TrimSpace(params.RoleID),
		)
	}

	methodKey := "control.workspace.role.member.remove"
	if assign {
		methodKey = "control.workspace.role.member.assign"
	}
	mergedCtx, cancel := a.withMutation(ctx, workspaceAudit(
		params.ActorID,
		params.WorkspaceID,
		methodKey,
		"account",
		params.AccountID,
	))
	defer cancel()

	if assign {
		return a.repository.AssignWorkspaceRole(
			mergedCtx,
			strings.TrimSpace(params.ActorID),
			params.WorkspaceID,
			strings.TrimSpace(params.AccountID),
			strings.TrimSpace(params.RoleID),
		)
	}

	return a.repository.RemoveWorkspaceRole(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		params.WorkspaceID,
		strings.TrimSpace(params.AccountID),
		strings.TrimSpace(params.RoleID),
	)

}

func (a *Admin) ReplaceGlobalRolePermissions(
	ctx context.Context,
	params ReplaceRolePermissionsParams,
) error {

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    strings.TrimSpace(params.ActorID),
		MethodKey:  "control.global.role.permission.replace",
		TargetType: "role",
		TargetID:   strings.TrimSpace(params.RoleID),
	})
	defer cancel()

	return a.repository.ReplaceGlobalRolePermissions(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		strings.TrimSpace(params.RoleID),
		params.MethodKeys,
	)

}

func (a *Admin) ListGlobalRolePermissions(
	ctx context.Context,
	roleID string,
) ([]string, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.ListGlobalRolePermissions(mergedCtx, strings.TrimSpace(roleID))

}

func (a *Admin) ReplaceWorkspaceRolePermissions(
	ctx context.Context,
	params ReplaceRolePermissionsParams,
) error {

	mergedCtx, cancel := a.withMutation(ctx, workspaceAudit(
		params.ActorID,
		params.WorkspaceID,
		"control.workspace.role.permission.replace",
		"role",
		params.RoleID,
	))
	defer cancel()

	return a.repository.ReplaceWorkspaceRolePermissions(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		params.WorkspaceID,
		strings.TrimSpace(params.RoleID),
		params.MethodKeys,
	)

}

func (a *Admin) ListWorkspaceRolePermissions(
	ctx context.Context,
	workspaceID string,
	roleID string,
) ([]string, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.ListWorkspaceRolePermissions(
		mergedCtx,
		workspaceID,
		strings.TrimSpace(roleID),
	)

}

func (a *Admin) ListMembers(
	ctx context.Context,
	workspaceID string,
	page Page,
) ([]MemberModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	items, err := a.repository.ListMembers(
		mergedCtx,
		workspaceID,
		mapCursor(page),
		page.Limit,
	)
	if err != nil {
		return nil, err
	}

	result := make([]MemberModel, 0, len(items))
	for _, item := range items {
		result = append(result, MemberModel{
			WorkspaceID: item.WorkspaceID,
			AccountID:   item.AccountID,
			DisplayName: item.DisplayName,
			IsOwner:     item.IsOwner,
			RoleIDs:     append([]string(nil), item.RoleIDs...),
			JoinedAt:    item.JoinedAt,
			UpdatedAt:   item.UpdatedAt,
		})
	}

	return result, nil

}

func (a *Admin) RemoveMember(
	ctx context.Context,
	actorID string,
	workspaceID string,
	accountID string,
) (int64, error) {

	mergedCtx, cancel := a.withMutation(ctx, workspaceAudit(
		actorID,
		workspaceID,
		"control.workspace.member.remove",
		"account",
		accountID,
	))
	defer cancel()

	return a.repository.RemoveMember(
		mergedCtx,
		strings.TrimSpace(actorID),
		workspaceID,
		strings.TrimSpace(accountID),
	)

}

func (a *Admin) CreateGlobalInvite(
	ctx context.Context,
	params CreateInviteParams,
) (InviteModel, string, error) {

	return a.createInvite(ctx, params, true)

}

func (a *Admin) CreateWorkspaceInvite(
	ctx context.Context,
	params CreateInviteParams,
) (InviteModel, string, error) {

	return a.createInvite(ctx, params, false)

}

func (a *Admin) createInvite(
	ctx context.Context,
	params CreateInviteParams,
	global bool,
) (InviteModel, string, error) {

	event := workspaceAudit(
		params.ActorID,
		params.WorkspaceID,
		"control.workspace.invite.create",
		"invite",
		"",
	)
	if global {
		event = repository.AuditEvent{
			Scope:      repository.ScopeGlobal,
			ActorID:    strings.TrimSpace(params.ActorID),
			MethodKey:  "control.global.invite.create",
			TargetType: "invite",
		}
	}
	mergedCtx, cancel := a.withMutation(ctx, event)
	defer cancel()

	input := repository.CreateInviteInput{
		ActorID:     strings.TrimSpace(params.ActorID),
		WorkspaceID: strings.TrimSpace(params.WorkspaceID),
		RoleIDs:     append([]string(nil), params.RoleIDs...),
		ExpiresAt:   params.ExpiresAt,
	}
	var (
		value repository.Invite
		token string
		err   error
	)
	if global {
		value, token, err = a.repository.CreateGlobalInvite(mergedCtx, input)
	} else {
		value, token, err = a.repository.CreateWorkspaceInvite(mergedCtx, input)
	}

	return mapInvite(value), token, err

}

func (a *Admin) ListGlobalInvites(
	ctx context.Context,
	page Page,
) ([]InviteModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	items, err := a.repository.ListGlobalInvites(mergedCtx, mapCursor(page), page.Limit)
	if err != nil {
		return nil, err
	}

	return mapInvites(items), nil

}

func (a *Admin) ListWorkspaceInvites(
	ctx context.Context,
	workspaceID string,
	page Page,
) ([]InviteModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	items, err := a.repository.ListWorkspaceInvites(
		mergedCtx,
		workspaceID,
		mapCursor(page),
		page.Limit,
	)
	if err != nil {
		return nil, err
	}

	return mapInvites(items), nil

}

func (a *Admin) RevokeInvite(
	ctx context.Context,
	actorID string,
	inviteID string,
) (int64, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.RevokeInvite(
		mergedCtx,
		strings.TrimSpace(actorID),
		strings.TrimSpace(inviteID),
	)

}

func (a *Admin) RequestWorkspaceLimit(
	ctx context.Context,
	accountID string,
	requestedLimit int32,
	reason string,
) (LimitRequestModel, error) {

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    strings.TrimSpace(accountID),
		MethodKey:  "control.global.workspace_limit.request",
		TargetType: "account",
		TargetID:   strings.TrimSpace(accountID),
	})
	defer cancel()

	value, err := a.repository.RequestWorkspaceLimit(
		mergedCtx,
		strings.TrimSpace(accountID),
		requestedLimit,
		strings.TrimSpace(reason),
	)

	return mapLimitRequest(value), err

}

func (a *Admin) RequestEmployeeLimit(
	ctx context.Context,
	actorID string,
	workspaceID string,
	requestedLimit int32,
	reason string,
) (LimitRequestModel, error) {

	mergedCtx, cancel := a.withMutation(ctx, workspaceAudit(
		actorID,
		workspaceID,
		"control.workspace.employee_limit.request",
		"workspace",
		workspaceID,
	))
	defer cancel()

	value, err := a.repository.RequestEmployeeLimit(
		mergedCtx,
		strings.TrimSpace(actorID),
		workspaceID,
		requestedLimit,
		strings.TrimSpace(reason),
	)

	return mapLimitRequest(value), err

}

func (a *Admin) ListLimitRequests(
	ctx context.Context,
	actorID string,
	status controlmodel.LimitRequestStatus,
	page Page,
) ([]LimitRequestModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	items, err := a.repository.ListLimitRequests(
		mergedCtx,
		strings.TrimSpace(actorID),
		controlmodel.LimitRequestStatus(strings.TrimSpace(string(status))),
		mapCursor(page),
		page.Limit,
	)
	if err != nil {
		return nil, err
	}

	result := make([]LimitRequestModel, 0, len(items))
	for _, item := range items {
		result = append(result, mapLimitRequest(item))
	}

	return result, nil

}

func (a *Admin) ResolveLimitRequest(
	ctx context.Context,
	params ResolveLimitRequestParams,
) (LimitRequestModel, error) {

	mergedCtx, cancel := a.withMutation(ctx, repository.AuditEvent{
		Scope:      repository.ScopeGlobal,
		ActorID:    strings.TrimSpace(params.ActorID),
		MethodKey:  limitResolutionMethod(params.Approved),
		TargetType: "limit_request",
		TargetID:   strings.TrimSpace(params.RequestID),
	})
	defer cancel()

	value, err := a.repository.ResolveLimitRequest(
		mergedCtx,
		strings.TrimSpace(params.ActorID),
		strings.TrimSpace(params.RequestID),
		params.Approved,
		params.ApprovedLimit,
		strings.TrimSpace(params.Comment),
	)

	return mapLimitRequest(value), err

}

func (a *Admin) CancelLimitRequest(
	ctx context.Context,
	accountID string,
	requestID string,
) (int64, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.CancelLimitRequest(
		mergedCtx,
		strings.TrimSpace(accountID),
		strings.TrimSpace(requestID),
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

func (a *Admin) ListAccess(
	ctx context.Context,
	locale string,
	scope AccessScope,
) ([]AccessGroupModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	rows, err := a.repository.ListAccessCatalog(
		mergedCtx,
		strings.TrimSpace(locale),
		repository.AccessScope(scope),
	)
	if err != nil {
		return nil, err
	}

	services := make([]AccessGroupModel, 0)
	for _, row := range rows {
		if len(services) == 0 || services[len(services)-1].Service != row.Service {
			services = append(services, AccessGroupModel{
				Service:     row.Service,
				Title:       row.ServiceTitle,
				Description: row.ServiceDescription,
			})
		}
		serviceIndex := len(services) - 1
		groups := services[serviceIndex].Groups
		if len(groups) == 0 || groups[len(groups)-1].Key != row.GroupKey {
			services[serviceIndex].Groups = append(groups, AccessGroups{
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
				Scope: AccessScope(row.Scope),
				Title: row.Title,
				Desc:  row.Desc,
			},
		)
	}

	return services, nil

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
		ID:             value.ID,
		Slug:           value.Slug,
		Title:          value.Title,
		Status:         value.Status,
		CreatedBy:      value.CreatedBy,
		OwnerAccountID: value.OwnerAccountID,
		EmployeeLimit:  value.EmployeeLimit,
		CreatedAt:      value.CreatedAt,
		UpdatedAt:      value.UpdatedAt,
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
		MemberCount: value.MemberCount,
		CreatedAt:   value.CreatedAt,
		UpdatedAt:   value.UpdatedAt,
	}

}

func mapRoles(values []repository.Role) []RoleModel {

	result := make([]RoleModel, 0, len(values))
	for _, value := range values {
		result = append(result, mapRole(value))
	}

	return result

}

func mapInvite(value repository.Invite) InviteModel {

	return InviteModel{
		ID:          value.ID,
		Kind:        InviteKind(value.Kind),
		WorkspaceID: value.WorkspaceID,
		CreatedBy:   value.CreatedBy,
		ExpiresAt:   value.ExpiresAt,
		AcceptedBy:  value.AcceptedBy,
		AcceptedAt:  value.AcceptedAt,
		RevokedAt:   value.RevokedAt,
		CreatedAt:   value.CreatedAt,
		RoleIDs:     append([]string(nil), value.RoleIDs...),
	}

}

func mapInvites(values []repository.Invite) []InviteModel {

	result := make([]InviteModel, 0, len(values))
	for _, value := range values {
		result = append(result, mapInvite(value))
	}

	return result

}

func mapLimitRequest(value repository.LimitRequest) LimitRequestModel {

	return LimitRequestModel{
		ID:             value.ID,
		Kind:           LimitKind(value.Kind),
		AccountID:      value.AccountID,
		WorkspaceID:    value.WorkspaceID,
		CurrentLimit:   value.CurrentLimit,
		RequestedLimit: value.RequestedLimit,
		ApprovedLimit:  value.ApprovedLimit,
		Reason:         value.Reason,
		Status:         value.Status,
		RequestedBy:    value.RequestedBy,
		ReviewedBy:     value.ReviewedBy,
		ReviewComment:  value.ReviewComment,
		CreatedAt:      value.CreatedAt,
		ReviewedAt:     value.ReviewedAt,
	}

}

func mapMethod(value repository.Method) MethodModel {

	return MethodModel{
		Key:       value.Key,
		Service:   value.Service,
		GroupKey:  value.GroupKey,
		Scope:     AccessScope(value.Scope),
		Position:  value.Position,
		CreatedAt: value.CreatedAt,
		UpdatedAt: value.UpdatedAt,
	}

}

func mapRoleParams(params UpdateRoleParams) repository.Role {

	return repository.Role{
		ID:          strings.TrimSpace(params.ID),
		WorkspaceID: strings.TrimSpace(params.WorkspaceID),
		Title:       strings.TrimSpace(params.Title),
		Description: strings.TrimSpace(params.Description),
		Position:    params.Position,
	}

}

func mapCursor(page Page) repository.Cursor {

	return repository.Cursor{
		Time: page.CursorAt,
		ID:   strings.TrimSpace(page.CursorID),
	}

}

func workspaceAudit(
	actorID string,
	workspaceID string,
	methodKey string,
	targetType string,
	targetID string,
) repository.AuditEvent {

	return repository.AuditEvent{
		Scope:       repository.ScopeWorkspace,
		WorkspaceID: workspaceID,
		ActorID:     strings.TrimSpace(actorID),
		MethodKey:   methodKey,
		TargetType:  targetType,
		TargetID:    strings.TrimSpace(targetID),
	}

}

func limitResolutionMethod(approved bool) string {

	if approved {
		return "control.global.limit.approve"
	}

	return "control.global.limit.reject"

}

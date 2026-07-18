package repository

import (
	"context"

	controlmodel "github.com/elum2b/services/control/model"
	controlsqlc "github.com/elum2b/services/control/sqlc"
	"github.com/google/uuid"
)

func (r *Repository) CreateWorkspace(
	ctx context.Context,
	actorID string,
	workspaceID string,
	slug string,
	title string,
) (Workspace, error) {

	if err := requireWorkspaceID(workspaceID); err != nil {
		return Workspace{}, err
	}
	if err := required(actorID, slug, title); err != nil {
		return Workspace{}, err
	}

	var result Workspace

	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := q.GetWorkspaceCreationBundleForUpdate(ctx, actorID)
		if err != nil {
			return noRows(err, ErrForbidden)
		}
		if !allowed(bundle.Allowed) {
			return ErrForbidden
		}
		if bundle.OwnedWorkspaceCount >= int64(bundle.WorkspaceLimit) {
			return ErrWorkspaceLimit
		}
		created, err := q.CreateWorkspace(ctx, controlsqlc.CreateWorkspaceParams{
			ID:        workspaceID,
			Slug:      slug,
			Title:     title,
			CreatedBy: actorID,
		})
		if err != nil {
			return writeConflict(err)
		}
		result = mapWorkspace(created)

		return q.AddWorkspaceMember(ctx, controlsqlc.AddWorkspaceMemberParams{
			WorkspaceID: workspaceID,
			AccountID:   actorID,
		})
	})
	if err != nil {
		return Workspace{}, err
	}

	return result, nil

}

func (r *Repository) GetWorkspace(ctx context.Context, workspaceID string) (Workspace, error) {

	row, err := r.q.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return Workspace{}, noRows(err, ErrWorkspaceNotFound)
	}

	return mapWorkspace(row), nil

}

func (r *Repository) ListWorkspaces(
	ctx context.Context,
	accountID string,
	cursor Cursor,
	limit int32,
) ([]Workspace, error) {

	rows, err := r.q.ListWorkspacesForAccount(
		ctx,
		controlsqlc.ListWorkspacesForAccountParams{
			AccountID: accountID,
			CursorAt:  nullableCursorTime(cursor),
			CursorID:  cursor.ID,
			PageLimit: pageLimit(limit),
		},
	)
	if err != nil {
		return nil, err
	}

	result := make([]Workspace, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapWorkspace(row))
	}

	return result, nil

}

func (r *Repository) UpdateWorkspace(
	ctx context.Context,
	actorID string,
	value Workspace,
) (int64, error) {

	if err := required(actorID, value.ID, value.Slug, value.Title); err != nil {
		return 0, err
	}

	var affected int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := requireWorkspaceAuthorization(
			ctx,
			q,
			actorID,
			value.ID,
			actorID,
			"control.workspace.update",
		); err != nil {
			return err
		}

		rows, err := q.UpdateWorkspace(ctx, controlsqlc.UpdateWorkspaceParams{
			Slug:  value.Slug,
			Title: value.Title,
			ID:    value.ID,
		})
		affected = rows

		return writeConflict(err)
	})

	return affected, err

}

func (r *Repository) ArchiveWorkspace(
	ctx context.Context,
	actorID string,
	workspaceID string,
) (int64, error) {

	var affected int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := q.GetWorkspaceAuthorizationForUpdate(
			ctx,
			controlsqlc.GetWorkspaceAuthorizationForUpdateParams{
				ActorID:         actorID,
				MethodKey:       "control.workspace.archive",
				TargetAccountID: actorID,
				WorkspaceID:     workspaceID,
			},
		)
		if err != nil {
			return noRows(err, ErrWorkspaceNotFound)
		}
		if !bundle.ActorIsOwner {
			return ErrForbidden
		}

		affected, err = q.ArchiveWorkspace(ctx, workspaceID)
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrWorkspaceNotFound
		}

		_, err = q.CancelPendingWorkspaceLimitRequests(
			ctx,
			nullableString(workspaceID),
		)

		return err
	})

	return affected, err

}

func (r *Repository) TransferWorkspaceOwnership(
	ctx context.Context,
	actorID string,
	workspaceID string,
	targetAccountID string,
) error {

	return r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := q.GetWorkspaceAuthorizationForUpdate(
			ctx,
			controlsqlc.GetWorkspaceAuthorizationForUpdateParams{
				ActorID:         actorID,
				MethodKey:       "control.workspace.owner.transfer",
				TargetAccountID: targetAccountID,
				WorkspaceID:     workspaceID,
			},
		)
		if err != nil {
			return noRows(err, ErrWorkspaceNotFound)
		}
		if !bundle.ActorIsOwner || !bundle.TargetIsActive || actorID == targetAccountID {
			return ErrOwnershipTransfer
		}

		capacity, err := q.GetWorkspaceOwnershipCapacityForUpdate(ctx, targetAccountID)
		if err != nil {
			return noRows(err, ErrOwnershipTransfer)
		}
		if capacity.OwnedWorkspaceCount >= int64(capacity.WorkspaceLimit) {
			return ErrWorkspaceLimit
		}

		rows, err := q.TransferWorkspaceOwnership(
			ctx,
			controlsqlc.TransferWorkspaceOwnershipParams{
				NewOwnerAccountID:     targetAccountID,
				WorkspaceID:           workspaceID,
				CurrentOwnerAccountID: actorID,
			},
		)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrOwnershipTransfer
		}

		return nil
	})

}

func (r *Repository) ListMembers(
	ctx context.Context,
	workspaceID string,
	cursor Cursor,
	limit int32,
) ([]Member, error) {

	rows, err := r.q.ListWorkspaceMembers(
		ctx,
		controlsqlc.ListWorkspaceMembersParams{
			WorkspaceID: workspaceID,
			CursorAt:    nullableCursorTime(cursor),
			CursorID:    cursor.ID,
			PageLimit:   pageLimit(limit),
		},
	)
	if err != nil {
		return nil, err
	}

	accountIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		accountIDs = append(accountIDs, row.AccountID)
	}
	roleRows, err := r.q.ListWorkspaceMemberRoles(
		ctx,
		controlsqlc.ListWorkspaceMemberRolesParams{
			WorkspaceID: workspaceID,
			AccountIds:  accountIDs,
		},
	)
	if err != nil {
		return nil, err
	}

	roles := make(map[string][]string, len(rows))
	for _, row := range roleRows {
		roles[row.AccountID] = append(roles[row.AccountID], row.RoleID)
	}

	result := make([]Member, 0, len(rows))
	for _, row := range rows {
		result = append(result, Member{
			WorkspaceID: row.WorkspaceID,
			AccountID:   row.AccountID,
			DisplayName: row.DisplayName,
			IsOwner:     row.IsOwner,
			RoleIDs:     roles[row.AccountID],
			JoinedAt:    row.JoinedAt,
			UpdatedAt:   row.UpdatedAt,
		})
	}

	return result, nil

}

func (r *Repository) RemoveMember(
	ctx context.Context,
	actorID string,
	workspaceID string,
	accountID string,
) (int64, error) {

	var affected int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := q.GetWorkspaceAuthorizationForUpdate(
			ctx,
			controlsqlc.GetWorkspaceAuthorizationForUpdateParams{
				ActorID:         actorID,
				MethodKey:       "control.workspace.member.remove",
				TargetAccountID: accountID,
				WorkspaceID:     workspaceID,
			},
		)
		if err != nil {
			return noRows(err, ErrWorkspaceNotFound)
		}
		if !allowed(bundle.Allowed) || !bundle.TargetIsActive || bundle.TargetPosition == 0 {
			return ErrForbidden
		}
		actorPosition, err := position(bundle.ActorPosition)
		if err != nil {
			return err
		}
		targetPosition, err := position(bundle.TargetPosition)
		if err != nil {
			return err
		}
		if err := requireHigher(actorPosition, targetPosition); err != nil {
			return err
		}
		if _, err := q.RemoveWorkspaceMemberRoles(
			ctx,
			controlsqlc.RemoveWorkspaceMemberRolesParams{
				WorkspaceID: workspaceID,
				AccountID:   accountID,
			},
		); err != nil {
			return err
		}
		if _, err := q.RevokePendingWorkspaceInvitesByCreator(
			ctx,
			controlsqlc.RevokePendingWorkspaceInvitesByCreatorParams{
				WorkspaceID: nullableString(workspaceID),
				CreatedBy:   accountID,
			},
		); err != nil {
			return err
		}

		affected, err = q.RemoveWorkspaceMember(
			ctx,
			controlsqlc.RemoveWorkspaceMemberParams{
				WorkspaceID: workspaceID,
				AccountID:   accountID,
			},
		)

		return err
	})

	return affected, err

}

func (r *Repository) CreateWorkspaceRole(
	ctx context.Context,
	actorID string,
	role Role,
) (Role, error) {

	if role.ID == "" {
		role.ID = uuid.NewString()
	}
	if err := required(actorID, role.ID, role.WorkspaceID, role.Code, role.Title); err != nil {
		return Role{}, err
	}

	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := workspaceAuthorization(
			ctx,
			q,
			actorID,
			role.WorkspaceID,
			actorID,
			"control.workspace.role.create",
		)
		if err != nil {
			return err
		}
		actorPosition, err := position(bundle.ActorPosition)
		if err != nil {
			return err
		}
		if err := requireHigher(actorPosition, role.Position); err != nil {
			return err
		}

		created, err := q.CreateWorkspaceRole(ctx, controlsqlc.CreateWorkspaceRoleParams{
			ID:          role.ID,
			WorkspaceID: role.WorkspaceID,
			Code:        role.Code,
			Title:       role.Title,
			Description: role.Description,
			Position:    role.Position,
		})
		if err != nil {
			return writeConflict(err)
		}

		role = Role{
			ID:          created.ID,
			WorkspaceID: created.WorkspaceID,
			Code:        created.Code,
			Title:       created.Title,
			Description: created.Description,
			Position:    created.Position,
			CreatedAt:   created.CreatedAt,
			UpdatedAt:   created.UpdatedAt,
		}

		return nil
	})

	return role, err

}

func (r *Repository) ListWorkspaceRoles(ctx context.Context, workspaceID string) ([]Role, error) {

	rows, err := r.q.ListWorkspaceRoles(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	result := make([]Role, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapWorkspaceRole(row))
	}

	return result, nil

}

func (r *Repository) GetWorkspaceRole(
	ctx context.Context,
	workspaceID string,
	roleID string,
) (Role, error) {

	row, err := r.q.GetWorkspaceRole(ctx, controlsqlc.GetWorkspaceRoleParams{
		ID:          roleID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return Role{}, noRows(err, ErrRoleNotFound)
	}

	return Role{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Code:        row.Code,
		Title:       row.Title,
		Description: row.Description,
		Position:    row.Position,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil

}

func (r *Repository) UpdateWorkspaceRole(
	ctx context.Context,
	actorID string,
	role Role,
) (int64, error) {

	if err := required(actorID, role.ID, role.WorkspaceID, role.Title); err != nil {
		return 0, err
	}

	var affected int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := workspaceAuthorization(
			ctx,
			q,
			actorID,
			role.WorkspaceID,
			actorID,
			"control.workspace.role.update",
		)
		if err != nil {
			return err
		}
		current, err := q.GetWorkspaceRole(ctx, controlsqlc.GetWorkspaceRoleParams{
			ID:          role.ID,
			WorkspaceID: role.WorkspaceID,
		})
		if err != nil {
			return noRows(err, ErrRoleNotFound)
		}
		actorPosition, err := position(bundle.ActorPosition)
		if err != nil {
			return err
		}
		if err := requireHigher(actorPosition, current.Position); err != nil {
			return err
		}
		if err := requireHigher(actorPosition, role.Position); err != nil {
			return err
		}

		affected, err = q.UpdateWorkspaceRole(ctx, controlsqlc.UpdateWorkspaceRoleParams{
			Title:       role.Title,
			Description: role.Description,
			Position:    role.Position,
			ID:          role.ID,
			WorkspaceID: role.WorkspaceID,
		})

		return err
	})

	return affected, err

}

func (r *Repository) DeleteWorkspaceRole(
	ctx context.Context,
	actorID string,
	workspaceID string,
	roleID string,
) (int64, error) {

	var affected int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := workspaceAuthorization(
			ctx,
			q,
			actorID,
			workspaceID,
			actorID,
			"control.workspace.role.delete",
		)
		if err != nil {
			return err
		}
		role, err := q.GetWorkspaceRole(ctx, controlsqlc.GetWorkspaceRoleParams{
			ID:          roleID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return noRows(err, ErrRoleNotFound)
		}
		actorPosition, err := position(bundle.ActorPosition)
		if err != nil {
			return err
		}
		if err := requireHigher(actorPosition, role.Position); err != nil {
			return err
		}
		if _, err := q.DeleteWorkspaceInviteRoleReferences(
			ctx,
			controlsqlc.DeleteWorkspaceInviteRoleReferencesParams{
				RoleID:      roleID,
				WorkspaceID: workspaceID,
			},
		); err != nil {
			return err
		}

		affected, err = q.DeleteWorkspaceRole(
			ctx,
			controlsqlc.DeleteWorkspaceRoleParams{
				ID:          roleID,
				WorkspaceID: workspaceID,
			},
		)

		return err
	})

	return affected, err

}

func (r *Repository) AssignWorkspaceRole(
	ctx context.Context,
	actorID string,
	workspaceID string,
	accountID string,
	roleID string,
) error {

	return r.changeWorkspaceRoleMember(
		ctx,
		actorID,
		workspaceID,
		accountID,
		roleID,
		true,
	)

}

func (r *Repository) RemoveWorkspaceRole(
	ctx context.Context,
	actorID string,
	workspaceID string,
	accountID string,
	roleID string,
) error {

	return r.changeWorkspaceRoleMember(
		ctx,
		actorID,
		workspaceID,
		accountID,
		roleID,
		false,
	)

}

func (r *Repository) changeWorkspaceRoleMember(
	ctx context.Context,
	actorID string,
	workspaceID string,
	accountID string,
	roleID string,
	assign bool,
) error {

	methodKey := "control.workspace.role.member.remove"
	if assign {
		methodKey = "control.workspace.role.member.assign"
	}

	return r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := workspaceAuthorization(
			ctx,
			q,
			actorID,
			workspaceID,
			accountID,
			methodKey,
		)
		if err != nil {
			return err
		}
		if !bundle.TargetIsActive || bundle.TargetPosition == 0 {
			return ErrForbidden
		}
		role, err := q.GetWorkspaceRole(ctx, controlsqlc.GetWorkspaceRoleParams{
			ID:          roleID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return noRows(err, ErrRoleNotFound)
		}
		actorPosition, err := position(bundle.ActorPosition)
		if err != nil {
			return err
		}
		targetPosition, err := position(bundle.TargetPosition)
		if err != nil {
			return err
		}
		if err := requireHigher(actorPosition, targetPosition); err != nil {
			return err
		}
		if err := requireHigher(actorPosition, role.Position); err != nil {
			return err
		}

		if assign {
			_, err = q.AddWorkspaceRoleMember(
				ctx,
				controlsqlc.AddWorkspaceRoleMemberParams{
					RoleID:      roleID,
					WorkspaceID: workspaceID,
					AccountID:   accountID,
				},
			)

			return err
		}

		_, err = q.RemoveWorkspaceRoleMember(
			ctx,
			controlsqlc.RemoveWorkspaceRoleMemberParams{
				RoleID:      roleID,
				WorkspaceID: workspaceID,
				AccountID:   accountID,
			},
		)

		return err
	})

}

func (r *Repository) ReplaceWorkspaceRolePermissions(
	ctx context.Context,
	actorID string,
	workspaceID string,
	roleID string,
	methodKeys []string,
) error {

	methodKeys = uniqueStrings(methodKeys)

	return r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := workspaceAuthorization(
			ctx,
			q,
			actorID,
			workspaceID,
			actorID,
			"control.workspace.role.permission.replace",
		)
		if err != nil {
			return err
		}
		role, err := q.GetWorkspaceRole(ctx, controlsqlc.GetWorkspaceRoleParams{
			ID:          roleID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return noRows(err, ErrRoleNotFound)
		}
		actorPosition, err := position(bundle.ActorPosition)
		if err != nil {
			return err
		}
		if err := requireHigher(actorPosition, role.Position); err != nil {
			return err
		}
		count, err := q.CountMethodsByScope(ctx, controlsqlc.CountMethodsByScopeParams{
			Scope:      string(ScopeWorkspace),
			MethodKeys: methodKeys,
		})
		if err != nil {
			return err
		}
		if count != int64(len(methodKeys)) {
			return ErrMethodNotFound
		}
		if !bundle.ActorIsOwner {
			methods, err := q.ListAuthorizedWorkspaceMethods(
				ctx,
				controlsqlc.ListAuthorizedWorkspaceMethodsParams{
					WorkspaceID: workspaceID,
					AccountID:   actorID,
				},
			)
			if err != nil {
				return err
			}
			allowedMethods := make(map[string]struct{}, len(methods))
			for _, method := range methods {
				allowedMethods[method.MethodKey] = struct{}{}
			}
			if !methodSubset(methodKeys, allowedMethods) {
				return ErrForbidden
			}
		}

		return q.ReplaceWorkspaceRolePermissions(
			ctx,
			controlsqlc.ReplaceWorkspaceRolePermissionsParams{
				WorkspaceID: workspaceID,
				RoleID:      roleID,
				MethodKeys:  methodKeys,
			},
		)
	})

}

func (r *Repository) ListWorkspaceRolePermissions(
	ctx context.Context,
	workspaceID string,
	roleID string,
) ([]string, error) {

	return r.q.ListWorkspaceRolePermissions(
		ctx,
		controlsqlc.ListWorkspaceRolePermissionsParams{
			WorkspaceID: workspaceID,
			RoleID:      roleID,
		},
	)

}

func workspaceAuthorization(
	ctx context.Context,
	q *controlsqlc.Queries,
	actorID string,
	workspaceID string,
	targetAccountID string,
	methodKey string,
) (controlsqlc.GetWorkspaceAuthorizationForUpdateRow, error) {

	bundle, err := q.GetWorkspaceAuthorizationForUpdate(
		ctx,
		controlsqlc.GetWorkspaceAuthorizationForUpdateParams{
			ActorID:         actorID,
			MethodKey:       methodKey,
			TargetAccountID: targetAccountID,
			WorkspaceID:     workspaceID,
		},
	)
	if err != nil {
		return controlsqlc.GetWorkspaceAuthorizationForUpdateRow{}, noRows(
			err,
			ErrWorkspaceNotFound,
		)
	}
	if !bundle.ActorIsActive || !allowed(bundle.Allowed) {
		return controlsqlc.GetWorkspaceAuthorizationForUpdateRow{}, ErrForbidden
	}

	return bundle, nil

}

func requireWorkspaceAuthorization(
	ctx context.Context,
	q *controlsqlc.Queries,
	actorID string,
	workspaceID string,
	targetAccountID string,
	methodKey string,
) error {

	_, err := workspaceAuthorization(
		ctx,
		q,
		actorID,
		workspaceID,
		targetAccountID,
		methodKey,
	)

	return err

}

func mapWorkspace(value controlsqlc.ControlWorkspace) Workspace {

	return Workspace{
		ID:             value.ID,
		Slug:           value.Slug,
		Title:          value.Title,
		Status:         controlmodel.WorkspaceStatus(value.Status),
		CreatedBy:      value.CreatedBy,
		OwnerAccountID: value.OwnerAccountID,
		EmployeeLimit:  value.EmployeeLimit,
		CreatedAt:      value.CreatedAt,
		UpdatedAt:      value.UpdatedAt,
	}

}

func mapWorkspaceRole(value controlsqlc.ListWorkspaceRolesRow) Role {

	return Role{
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

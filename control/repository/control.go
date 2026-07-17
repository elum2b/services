package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	controlsqlc "github.com/elum2b/services/control/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/google/uuid"
)

const (
	accessWorkspaceUpdate     = "control.workspace.update"
	accessMemberRemove        = "control.member.remove"
	accessInviteCreate        = "control.invite.create"
	accessInviteRevoke        = "control.invite.revoke"
	accessRoleCreate          = "control.role.create"
	accessRoleUpdate          = "control.role.update"
	accessRoleDelete          = "control.role.delete"
	accessRoleMemberSet       = "control.role_member.set"
	accessRoleMemberRemove    = "control.role_member.remove"
	accessRolePermissionSet   = "control.role_permission.set"
	accessRolePermissionClear = "control.role_permission.clear"
)

func (r *Repository) CreateAccount(ctx context.Context, id, displayName string) (Account, error) {
	if id = normalizeID(id); id == "" {
		id = uuid.NewString()
	}
	if err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		return q.CreateAccount(ctx, controlsqlc.CreateAccountParams{
			ID:          id,
			DisplayName: displayName,
		})
	}); err != nil {
		return Account{}, err
	}
	now := time.Now()
	return Account{ID: id, DisplayName: displayName, Status: "active", CreatedAt: now, UpdatedAt: now}, nil
}

func (r *Repository) GetAccount(ctx context.Context, id string) (Account, error) {
	row, err := r.q.GetAccount(ctx, normalizeID(id))
	if err != nil {
		return Account{}, noRows(err, ErrAccountNotFound)
	}
	return Account{
		ID:          row.ID,
		DisplayName: row.DisplayName,
		Status:      row.Status,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func (r *Repository) CreateWorkspace(ctx context.Context, workspaceID, slug, title, actorID string) (Workspace, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Workspace{}, err
	}
	if err := required(workspaceID, slug, title, actorID); err != nil {
		return Workspace{}, err
	}
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := q.CreateWorkspace(ctx, controlsqlc.CreateWorkspaceParams{ID: workspaceID, Slug: slug, Title: title, CreatedBy: actorID}); err != nil {
			return err
		}
		if err := q.AddWorkspaceMember(ctx, controlsqlc.AddWorkspaceMemberParams{WorkspaceID: workspaceID, AccountID: actorID}); err != nil {
			return err
		}
		ownerID := uuid.NewString()
		if err := q.CreateRole(ctx, controlsqlc.CreateRoleParams{ID: ownerID, WorkspaceID: workspaceID, Code: "owner", Title: "Owner", Position: 1, IsOwner: true}); err != nil {
			return err
		}
		if err := q.AddRoleMember(ctx, controlsqlc.AddRoleMemberParams{RoleID: ownerID, AccountID: actorID}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Workspace{}, err
	}
	return r.GetWorkspace(ctx, workspaceID)
}

func (r *Repository) GetWorkspace(ctx context.Context, workspaceID string) (Workspace, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Workspace{}, err
	}

	row, err := r.q.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return Workspace{}, noRows(err, ErrWorkspaceNotFound)
	}
	return Workspace{
		ID:        row.ID,
		Slug:      row.Slug,
		Title:     row.Title,
		Status:    row.Status,
		CreatedBy: row.CreatedBy,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

func (r *Repository) ListWorkspaces(ctx context.Context, accountID string, limit, offset int32) ([]Workspace, error) {
	if err := required(accountID); err != nil {
		return nil, err
	}
	rows, err := r.q.ListWorkspacesForAccount(
		ctx,
		controlsqlc.ListWorkspacesForAccountParams{AccountID: accountID, Limit: limit, Offset: offset},
	)
	if err != nil {
		return nil, err
	}
	result := make([]Workspace, 0, len(rows))
	for _, row := range rows {
		result = append(
			result,
			Workspace{
				ID:        row.ID,
				Slug:      row.Slug,
				Title:     row.Title,
				Status:    row.Status,
				CreatedBy: row.CreatedBy,
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
		)
	}
	return result, nil
}

func (r *Repository) CreateRole(ctx context.Context, actorID string, role Role) (Role, error) {
	if err := required(actorID, role.ID, role.WorkspaceID, role.Code, role.Title); err != nil {
		return Role{}, err
	}
	if err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := authorizeWorkspaceMutation(
			ctx,
			q,
			actorID,
			role.WorkspaceID,
			accessRoleCreate,
		); err != nil {
			return err
		}
		if err := requireHigherThanPositionWithQueries(
			ctx,
			q,
			actorID,
			role.WorkspaceID,
			role.Position,
		); err != nil {
			return err
		}

		return q.CreateRole(ctx, controlsqlc.CreateRoleParams{
			ID:          role.ID,
			WorkspaceID: role.WorkspaceID,
			Code:        role.Code,
			Title:       role.Title,
			Description: role.Description,
			Position:    role.Position,
			IsOwner:     false,
		})
	}); err != nil {
		return Role{}, err
	}
	_ = r.touchAuthVersion(ctx, role.WorkspaceID)
	return r.GetRole(ctx, role.WorkspaceID, role.ID)
}

func (r *Repository) GetRole(ctx context.Context, workspaceID, roleID string) (Role, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Role{}, err
	}

	row, err := r.q.GetRole(ctx, controlsqlc.GetRoleParams{WorkspaceID: workspaceID, ID: roleID})
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
		IsOwner:     row.IsOwner,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

func (r *Repository) ListRoles(ctx context.Context, workspaceID string) ([]Role, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	rows, err := r.q.ListRoles(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make([]Role, 0, len(rows))
	for _, row := range rows {
		result = append(
			result,
			Role{
				ID:          row.ID,
				WorkspaceID: row.WorkspaceID,
				Code:        row.Code,
				Title:       row.Title,
				Description: row.Description,
				Position:    row.Position,
				IsOwner:     row.IsOwner,
				MemberCount: row.MemberCount,
				CreatedAt:   row.CreatedAt,
				UpdatedAt:   row.UpdatedAt,
			},
		)
	}
	return result, nil
}

func (r *Repository) AssignRole(ctx context.Context, actorID, workspaceID, accountID, roleID string) error {
	if err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := authorizeWorkspaceMutation(
			ctx,
			q,
			actorID,
			workspaceID,
			accessRoleMemberSet,
		); err != nil {
			return err
		}

		role, err := q.GetRole(ctx, controlsqlc.GetRoleParams{
			WorkspaceID: workspaceID,
			ID:          roleID,
		})
		if err != nil {
			return noRows(err, ErrRoleNotFound)
		}
		if role.IsOwner {
			return ErrRoleHierarchy
		}
		if err := requireActorHigherWithQueries(
			ctx,
			q,
			actorID,
			workspaceID,
			accountID,
			role.Position,
		); err != nil {
			return err
		}

		active, err := q.IsActiveWorkspaceMember(
			ctx,
			controlsqlc.IsActiveWorkspaceMemberParams{
				WorkspaceID: workspaceID,
				AccountID:   accountID,
			},
		)
		if err != nil {
			return err
		}
		if !active {
			return ErrForbidden
		}

		return q.AddRoleMember(ctx, controlsqlc.AddRoleMemberParams{
			RoleID:    roleID,
			AccountID: accountID,
		})
	}); err != nil {
		return err
	}
	return r.touchAuthVersion(ctx, workspaceID)
}

func (r *Repository) RemoveRole(ctx context.Context, actorID, workspaceID, accountID, roleID string) (int64, error) {
	var rows int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := authorizeWorkspaceMutation(
			ctx,
			q,
			actorID,
			workspaceID,
			accessRoleMemberRemove,
		); err != nil {
			return err
		}

		role, err := q.GetRole(ctx, controlsqlc.GetRoleParams{
			WorkspaceID: workspaceID,
			ID:          roleID,
		})
		if err != nil {
			return noRows(err, ErrRoleNotFound)
		}
		if role.IsOwner {
			return ErrRoleHierarchy
		}
		if err := requireActorHigherWithQueries(
			ctx,
			q,
			actorID,
			workspaceID,
			accountID,
			role.Position,
		); err != nil {
			return err
		}

		var writeErr error
		rows, writeErr = q.RemoveRoleMember(ctx, controlsqlc.RemoveRoleMemberParams{
			RoleID:    roleID,
			AccountID: accountID,
		})
		if writeErr != nil || rows == 0 {
			return writeErr
		}
		return nil
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	return rows, r.touchAuthVersion(ctx, workspaceID)
}

func (r *Repository) UpdateRole(ctx context.Context, actorID string, role Role) (int64, error) {
	var rows int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := authorizeWorkspaceMutation(ctx, q, actorID, role.WorkspaceID, accessRoleUpdate); err != nil {
			return err
		}

		current, err := q.GetRole(ctx, controlsqlc.GetRoleParams{
			WorkspaceID: role.WorkspaceID,
			ID:          role.ID,
		})
		if err != nil {
			return noRows(err, ErrRoleNotFound)
		}
		if current.IsOwner {
			return ErrRoleHierarchy
		}
		if err := requireHigherThanPositionWithQueries(
			ctx,
			q,
			actorID,
			role.WorkspaceID,
			current.Position,
		); err != nil {
			return err
		}
		if err := requireHigherThanPositionWithQueries(
			ctx,
			q,
			actorID,
			role.WorkspaceID,
			role.Position,
		); err != nil {
			return err
		}

		var writeErr error
		rows, writeErr = q.UpdateRole(ctx, controlsqlc.UpdateRoleParams{
			Title:       role.Title,
			Description: role.Description,
			Position:    role.Position,
			ID:          role.ID,
			WorkspaceID: role.WorkspaceID,
		})
		if writeErr != nil || rows == 0 {
			return writeErr
		}
		return nil
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	return rows, r.touchAuthVersion(ctx, role.WorkspaceID)
}

func (r *Repository) DeleteRole(ctx context.Context, actorID, workspaceID, roleID string) (int64, error) {
	var rows int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := authorizeWorkspaceMutation(ctx, q, actorID, workspaceID, accessRoleDelete); err != nil {
			return err
		}

		role, err := q.GetRole(ctx, controlsqlc.GetRoleParams{
			WorkspaceID: workspaceID,
			ID:          roleID,
		})
		if err != nil {
			return noRows(err, ErrRoleNotFound)
		}
		if role.IsOwner {
			return ErrRoleHierarchy
		}
		if err := requireHigherThanPositionWithQueries(
			ctx,
			q,
			actorID,
			workspaceID,
			role.Position,
		); err != nil {
			return err
		}

		var writeErr error
		rows, writeErr = q.DeleteRole(ctx, controlsqlc.DeleteRoleParams{
			ID:          roleID,
			WorkspaceID: workspaceID,
		})
		if writeErr != nil || rows == 0 {
			return writeErr
		}
		return nil
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	return rows, r.touchAuthVersion(ctx, workspaceID)
}

func (r *Repository) SetPermission(
	ctx context.Context,
	actorID, workspaceID, roleID, methodKey string,
	enabled bool,
) error {
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := authorizeWorkspaceMutation(
			ctx,
			q,
			actorID,
			workspaceID,
			accessRolePermissionSet,
		); err != nil {
			return err
		}
		if enabled {
			if err := requireMethodAccessWithQueries(ctx, q, actorID, workspaceID, methodKey); err != nil {
				return err
			}
		}

		role, err := q.GetRole(ctx, controlsqlc.GetRoleParams{
			WorkspaceID: workspaceID,
			ID:          roleID,
		})
		if err != nil {
			return noRows(err, ErrRoleNotFound)
		}
		if role.IsOwner {
			return ErrRoleHierarchy
		}
		if err := requireHigherThanPositionWithQueries(
			ctx,
			q,
			actorID,
			workspaceID,
			role.Position,
		); err != nil {
			return err
		}
		if _, err := q.GetMethod(ctx, methodKey); err != nil {
			return noRows(err, ErrMethodNotFound)
		}

		if enabled {
			return q.SetRolePermission(ctx, controlsqlc.SetRolePermissionParams{
				RoleID:    roleID,
				MethodKey: methodKey,
			})
		}
		_, deleteErr := q.DeleteRolePermission(ctx, controlsqlc.DeleteRolePermissionParams{
			RoleID:    roleID,
			MethodKey: methodKey,
		})
		return deleteErr
	})
	if err != nil {
		return err
	}
	return r.touchAuthVersion(ctx, workspaceID)
}

func (r *Repository) ListPermissions(ctx context.Context, workspaceID, roleID string) ([]string, error) {
	rows, err := r.q.ListRolePermissions(
		ctx,
		controlsqlc.ListRolePermissionsParams{WorkspaceID: workspaceID, RoleID: roleID},
	)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.MethodKey)
	}
	return result, nil
}

func (r *Repository) ClearPermissions(ctx context.Context, actorID, workspaceID, roleID string) (int64, error) {
	var rows int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := authorizeWorkspaceMutation(
			ctx,
			q,
			actorID,
			workspaceID,
			accessRolePermissionClear,
		); err != nil {
			return err
		}

		role, err := q.GetRole(ctx, controlsqlc.GetRoleParams{
			WorkspaceID: workspaceID,
			ID:          roleID,
		})
		if err != nil {
			return noRows(err, ErrRoleNotFound)
		}
		if role.IsOwner {
			return ErrRoleHierarchy
		}
		if err := requireHigherThanPositionWithQueries(
			ctx,
			q,
			actorID,
			workspaceID,
			role.Position,
		); err != nil {
			return err
		}

		var writeErr error
		rows, writeErr = q.ClearRolePermissions(ctx, roleID)
		if writeErr != nil || rows == 0 {
			return writeErr
		}
		return nil
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	return rows, r.touchAuthVersion(ctx, workspaceID)
}

func (r *Repository) RegisterMethod(ctx context.Context, method Method) error {
	return r.RegisterMethods(ctx, []Method{method})
}

func (r *Repository) RegisterMethods(ctx context.Context, methods []Method) error {
	for _, method := range methods {
		if err := required(method.Key, method.Service, method.GroupKey); err != nil {
			return err
		}
	}
	if len(methods) == 0 {
		return nil
	}

	err := sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(_ *sql.Tx, q *controlsqlc.Queries) error {
			if err := q.LockMethodRegistry(ctx); err != nil {
				return err
			}

			for _, method := range methods {
				existing, err := q.GetMethod(ctx, method.Key)
				if err == nil && existing.Service != method.Service {
					return ErrMethodOwner
				}
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}

				if err := q.UpsertMethodGroup(ctx, controlsqlc.UpsertMethodGroupParams{
					Service:  method.Service,
					GroupKey: method.GroupKey,
					Position: method.Position,
				}); err != nil {
					return err
				}

				if err := q.UpsertMethod(ctx, controlsqlc.UpsertMethodParams{
					MethodKey: method.Key,
					Service:   method.Service,
					GroupKey:  method.GroupKey,
					Position:  method.Position,
				}); err != nil {
					return err
				}
			}

			return nil
		},
	)
	if err != nil {
		return err
	}

	r.bumpCacheVersion("control", "method-registry")
	return nil
}

func (r *Repository) ListMethodGroups(ctx context.Context) ([]MethodGroup, error) {
	rows, err := r.q.ListMethodGroups(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]MethodGroup, 0, len(rows))
	for _, row := range rows {
		result = append(
			result,
			MethodGroup{
				Service:   row.Service,
				Key:       row.GroupKey,
				Position:  row.Position,
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
		)
	}
	return result, nil
}

func (r *Repository) ListAccessCatalog(ctx context.Context, locale string) ([]AccessCatalogRow, error) {
	locale = strings.TrimSpace(locale)
	version := r.db.CacheVersion("control", "access-catalog")
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		KeyParts:          []any{"control", "access-catalog", version, locale},
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: []any{"control", "access-catalog"},
	}, func(ctx context.Context) ([]AccessCatalogRow, error) {
		rows, err := r.q.ListAccessCatalog(
			ctx,
			controlsqlc.ListAccessCatalogParams{
				Locale:   locale,
				Locale_2: locale,
				Locale_3: locale,
				Locale_4: locale,
				Locale_5: locale,
				Locale_6: locale,
			},
		)
		if err != nil {
			return nil, err
		}
		result := make([]AccessCatalogRow, 0, len(rows))
		for _, row := range rows {
			result = append(
				result,
				AccessCatalogRow{
					Service:            row.Service,
					ServicePosition:    row.ServicePosition,
					ServiceTitle:       row.ServiceTitle,
					ServiceDescription: row.ServiceDescription,
					GroupKey:           row.GroupKey,
					GroupPosition:      row.GroupPosition,
					GroupTitle:         row.GroupTitle,
					GroupDescription:   row.GroupDescription,
					Key:                row.MethodKey,
					Position:           row.Position,
					Title:              row.AccessTitle,
					Desc:               row.AccessDescription,
				},
			)
		}
		return result, nil
	})
}

func (r *Repository) ListMethods(ctx context.Context) ([]Method, error) {
	rows, err := r.q.ListMethods(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Method, 0, len(rows))
	for _, row := range rows {
		result = append(
			result,
			Method{
				Key:       row.MethodKey,
				Service:   row.Service,
				GroupKey:  row.GroupKey,
				Position:  row.Position,
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
		)
	}
	return result, nil
}

func (r *Repository) GetMethod(ctx context.Context, methodKey string) (Method, error) {
	row, err := r.q.GetMethod(ctx, methodKey)
	if err != nil {
		return Method{}, noRows(err, ErrMethodNotFound)
	}
	return Method{
		Key:       row.MethodKey,
		Service:   row.Service,
		GroupKey:  row.GroupKey,
		Position:  row.Position,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

func (r *Repository) CheckAccess(ctx context.Context, accountID, workspaceID, methodKey string) (bool, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return false, err
	}

	if err := required(accountID, workspaceID, methodKey); err != nil {
		return false, err
	}
	methodVersion := r.db.CacheVersion("control", "method-registry")
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		KeyParts: []any{"control", "access", methodVersion, workspaceID, accountID, methodKey}, Timeout: r.timeout,
		CacheL1Delay: r.cacheL1, CacheL2Delay: r.cacheL2,
		CacheVersionScope: []any{"control", "access", workspaceID},
	}, func(ctx context.Context) (bool, error) {
		return r.q.CheckAccess(
			ctx,
			controlsqlc.CheckAccessParams{AccountID: accountID, WorkspaceID: workspaceID, MethodKey: methodKey},
		)
	})
}

func (r *Repository) ListAuthorizedMethods(ctx context.Context, accountID, workspaceID string) ([]Method, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	if err := required(accountID, workspaceID); err != nil {
		return nil, err
	}
	methodVersion := r.db.CacheVersion("control", "method-registry")
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		KeyParts:          []any{"control", "authorized-methods", methodVersion, workspaceID, accountID},
		Timeout:           r.timeout,
		CacheL1Delay:      r.cacheL1,
		CacheL2Delay:      r.cacheL2,
		CacheVersionScope: []any{"control", "access", workspaceID},
	}, func(ctx context.Context) ([]Method, error) {
		rows, err := r.q.ListAuthorizedMethods(
			ctx,
			controlsqlc.ListAuthorizedMethodsParams{WorkspaceID: workspaceID, AccountID: accountID},
		)
		if err != nil {
			return nil, err
		}
		result := make([]Method, 0, len(rows))
		for _, row := range rows {
			result = append(
				result,
				Method{Key: row.MethodKey, Service: row.Service, GroupKey: row.GroupKey, Position: row.Position},
			)
		}
		return result, nil
	})
}

func (r *Repository) touchAuthVersion(_ context.Context, workspaceID string) error {
	r.bumpCacheVersion("control", "access", workspaceID)
	return nil
}

func (r *Repository) requireMethodAccess(ctx context.Context, accountID, workspaceID, methodKey string) error {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}

	if err := required(accountID, workspaceID, methodKey); err != nil {
		return err
	}
	return requireMethodAccessWithQueries(ctx, r.q, accountID, workspaceID, methodKey)
}

func requireMethodAccessWithQueries(
	ctx context.Context,
	q *controlsqlc.Queries,
	accountID string,
	workspaceID string,
	methodKey string,
) error {
	allowed, err := q.CheckAccess(ctx, controlsqlc.CheckAccessParams{
		AccountID:   accountID,
		WorkspaceID: workspaceID,
		MethodKey:   methodKey,
	})
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (r *Repository) requireHigherThanPosition(
	ctx context.Context,
	actorID, workspaceID string,
	targetPosition int32,
) error {
	return requireHigherThanPositionWithQueries(ctx, r.q, actorID, workspaceID, targetPosition)
}

func requireHigherThanPositionWithQueries(
	ctx context.Context,
	q *controlsqlc.Queries,
	actorID string,
	workspaceID string,
	targetPosition int32,
) error {
	position, err := accountPositionWithQueries(ctx, q, actorID, workspaceID)
	if err != nil || position >= targetPosition {
		if err != nil {
			return err
		}
		return ErrRoleHierarchy
	}
	return nil
}

func (r *Repository) requireActorHigher(
	ctx context.Context,
	actorID, workspaceID, targetAccountID string,
	changedRolePosition int32,
) error {
	return requireActorHigherWithQueries(
		ctx,
		r.q,
		actorID,
		workspaceID,
		targetAccountID,
		changedRolePosition,
	)
}

func requireActorHigherWithQueries(
	ctx context.Context,
	q *controlsqlc.Queries,
	actorID string,
	workspaceID string,
	targetAccountID string,
	changedRolePosition int32,
) error {
	actorPosition, err := accountPositionWithQueries(ctx, q, actorID, workspaceID)
	if err != nil {
		return err
	}
	targetPosition, err := accountPositionWithQueries(ctx, q, targetAccountID, workspaceID)
	if err != nil || actorPosition >= targetPosition || actorPosition >= changedRolePosition {
		if err != nil {
			return err
		}
		return ErrRoleHierarchy
	}
	return nil
}

func (r *Repository) accountPosition(ctx context.Context, accountID, workspaceID string) (int32, error) {
	return accountPositionWithQueries(ctx, r.q, accountID, workspaceID)
}

func accountPositionWithQueries(
	ctx context.Context,
	q *controlsqlc.Queries,
	accountID string,
	workspaceID string,
) (int32, error) {
	value, err := q.GetAccountPosition(
		ctx,
		controlsqlc.GetAccountPositionParams{WorkspaceID: workspaceID, AccountID: accountID},
	)
	if err != nil {
		return 0, err
	}
	switch value := value.(type) {
	case int64:
		return int32(value), nil
	case uint64:
		return int32(value), nil
	case int32:
		return value, nil
	case int:
		return int32(value), nil
	case []byte:
		var position int32
		if _, err := fmt.Sscan(string(value), &position); err != nil {
			return 0, err
		}
		return position, nil
	default:
		return 0, fmt.Errorf("unexpected account position type %T", value)
	}
}

func authorizeWorkspaceMutation(
	ctx context.Context,
	q *controlsqlc.Queries,
	actorID string,
	workspaceID string,
	methodKey string,
) error {
	if err := q.LockWorkspaceAuthorization(ctx, workspaceID); err != nil {
		return err
	}

	return requireMethodAccessWithQueries(ctx, q, actorID, workspaceID, methodKey)
}

func (r *Repository) requireActiveMember(ctx context.Context, accountID, workspaceID string) error {
	active, err := r.q.IsActiveWorkspaceMember(
		ctx,
		controlsqlc.IsActiveWorkspaceMemberParams{WorkspaceID: workspaceID, AccountID: accountID},
	)
	if err != nil {
		return err
	}
	if !active {
		return ErrForbidden
	}
	return nil
}

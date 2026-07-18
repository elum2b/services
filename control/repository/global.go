package repository

import (
	"context"
	"database/sql"

	controlmodel "github.com/elum2b/services/control/model"
	controlsqlc "github.com/elum2b/services/control/sqlc"
	"github.com/google/uuid"
)

func (r *Repository) ListPlatformMembers(
	ctx context.Context,
	cursor Cursor,
	limit int32,
) ([]PlatformMember, error) {

	rows, err := r.q.ListPlatformMembers(ctx, controlsqlc.ListPlatformMembersParams{
		CursorAt:  nullableCursorTime(cursor),
		CursorID:  cursor.ID,
		PageLimit: pageLimit(limit),
	})
	if err != nil {
		return nil, err
	}

	result := make([]PlatformMember, 0, len(rows))
	for _, row := range rows {
		result = append(result, PlatformMember{
			AccountID:           row.AccountID,
			DisplayName:         row.DisplayName,
			Status:              controlmodel.MembershipStatus(row.Status),
			WorkspaceLimit:      row.WorkspaceLimit,
			OwnedWorkspaceCount: row.OwnedWorkspaceCount,
			InvitedBy:           valueString(row.InvitedBy),
			JoinedAt:            row.JoinedAt,
			UpdatedAt:           row.UpdatedAt,
		})
	}

	return result, nil

}

func (r *Repository) RemovePlatformMember(
	ctx context.Context,
	actorID string,
	accountID string,
) (int64, error) {

	var affected int64
	err := r.withAuditDBTx(ctx, func(tx *sql.Tx, q *controlsqlc.Queries) error {
		if err := lockAccountAuthentication(ctx, tx, accountID); err != nil {
			return err
		}

		bundle, err := globalAuthorization(
			ctx,
			q,
			actorID,
			accountID,
			"control.global.member.remove",
		)
		if err != nil {
			return err
		}
		if !bundle.TargetIsActive || bundle.TargetPosition == 0 {
			return ErrForbidden
		}
		capacity, err := q.GetWorkspaceOwnershipCapacityForUpdate(ctx, accountID)
		if err != nil {
			return noRows(err, ErrForbidden)
		}
		if capacity.OwnedWorkspaceCount > 0 {
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
		if _, err := q.RemoveAllGlobalRoleMemberships(ctx, accountID); err != nil {
			return err
		}
		if _, err := q.RemoveAllWorkspaceRoleMemberships(ctx, accountID); err != nil {
			return err
		}
		if _, err := q.RemoveAllWorkspaceMemberships(ctx, accountID); err != nil {
			return err
		}
		if _, err := q.RevokePendingInvitesByCreator(ctx, accountID); err != nil {
			return err
		}
		if _, err := q.RevokeAllSessions(ctx, controlsqlc.RevokeAllSessionsParams{
			AccountID: accountID,
			Column2:   "",
		}); err != nil {
			return err
		}
		if _, err := q.DeleteTwoFactorChallengesForAccount(ctx, accountID); err != nil {
			return err
		}
		if _, err := q.CancelPendingAccountLimitRequests(
			ctx,
			nullableString(accountID),
		); err != nil {
			return err
		}

		affected, err = q.RemovePlatformMember(ctx, accountID)

		return err
	})

	return affected, err

}

func (r *Repository) TransferGlobalOwnership(
	ctx context.Context,
	actorID string,
	targetAccountID string,
) error {

	return r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := globalAuthorization(
			ctx,
			q,
			actorID,
			targetAccountID,
			"control.global.owner.transfer",
		)
		if err != nil {
			return err
		}
		if !bundle.ActorIsOwner || !bundle.TargetIsActive || actorID == targetAccountID {
			return ErrOwnershipTransfer
		}

		rows, err := q.TransferGlobalOwnership(
			ctx,
			controlsqlc.TransferGlobalOwnershipParams{
				NewOwnerAccountID:     targetAccountID,
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

func (r *Repository) CreateGlobalRole(
	ctx context.Context,
	actorID string,
	role Role,
) (Role, error) {

	if role.ID == "" {
		role.ID = uuid.NewString()
	}
	if err := required(actorID, role.ID, role.Code, role.Title); err != nil {
		return Role{}, err
	}

	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := globalAuthorization(
			ctx,
			q,
			actorID,
			actorID,
			"control.global.role.create",
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

		created, err := q.CreateGlobalRole(ctx, controlsqlc.CreateGlobalRoleParams{
			ID:          role.ID,
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

func (r *Repository) ListGlobalRoles(ctx context.Context) ([]Role, error) {

	rows, err := r.q.ListGlobalRoles(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]Role, 0, len(rows))
	for _, row := range rows {
		result = append(result, Role{
			ID:          row.ID,
			Code:        row.Code,
			Title:       row.Title,
			Description: row.Description,
			Position:    row.Position,
			MemberCount: row.MemberCount,
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
		})
	}

	return result, nil

}

func (r *Repository) GetGlobalRole(ctx context.Context, roleID string) (Role, error) {

	row, err := r.q.GetGlobalRole(ctx, roleID)
	if err != nil {
		return Role{}, noRows(err, ErrRoleNotFound)
	}

	return Role{
		ID:          row.ID,
		Code:        row.Code,
		Title:       row.Title,
		Description: row.Description,
		Position:    row.Position,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil

}

func (r *Repository) UpdateGlobalRole(
	ctx context.Context,
	actorID string,
	role Role,
) (int64, error) {

	if err := required(actorID, role.ID, role.Title); err != nil {
		return 0, err
	}

	var affected int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := globalAuthorization(
			ctx,
			q,
			actorID,
			actorID,
			"control.global.role.update",
		)
		if err != nil {
			return err
		}
		current, err := q.GetGlobalRole(ctx, role.ID)
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

		affected, err = q.UpdateGlobalRole(ctx, controlsqlc.UpdateGlobalRoleParams{
			Title:       role.Title,
			Description: role.Description,
			Position:    role.Position,
			ID:          role.ID,
		})

		return err
	})

	return affected, err

}

func (r *Repository) DeleteGlobalRole(
	ctx context.Context,
	actorID string,
	roleID string,
) (int64, error) {

	var affected int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := globalAuthorization(
			ctx,
			q,
			actorID,
			actorID,
			"control.global.role.delete",
		)
		if err != nil {
			return err
		}
		role, err := q.GetGlobalRole(ctx, roleID)
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
		if _, err := q.DeleteGlobalInviteRoleReferences(ctx, roleID); err != nil {
			return err
		}

		affected, err = q.DeleteGlobalRole(ctx, roleID)

		return err
	})

	return affected, err

}

func (r *Repository) AssignGlobalRole(
	ctx context.Context,
	actorID string,
	accountID string,
	roleID string,
) error {

	return r.changeGlobalRoleMember(ctx, actorID, accountID, roleID, true)

}

func (r *Repository) RemoveGlobalRole(
	ctx context.Context,
	actorID string,
	accountID string,
	roleID string,
) error {

	return r.changeGlobalRoleMember(ctx, actorID, accountID, roleID, false)

}

func (r *Repository) changeGlobalRoleMember(
	ctx context.Context,
	actorID string,
	accountID string,
	roleID string,
	assign bool,
) error {

	methodKey := "control.global.role.member.remove"
	if assign {
		methodKey = "control.global.role.member.assign"
	}

	return r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := globalAuthorization(ctx, q, actorID, accountID, methodKey)
		if err != nil {
			return err
		}
		if !bundle.TargetIsActive || bundle.TargetPosition == 0 {
			return ErrForbidden
		}
		role, err := q.GetGlobalRole(ctx, roleID)
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
			_, err = q.AddGlobalRoleMember(ctx, controlsqlc.AddGlobalRoleMemberParams{
				RoleID:    roleID,
				AccountID: accountID,
			})

			return err
		}

		_, err = q.RemoveGlobalRoleMember(
			ctx,
			controlsqlc.RemoveGlobalRoleMemberParams{
				RoleID:    roleID,
				AccountID: accountID,
			},
		)

		return err
	})

}

func (r *Repository) ReplaceGlobalRolePermissions(
	ctx context.Context,
	actorID string,
	roleID string,
	methodKeys []string,
) error {

	methodKeys = uniqueStrings(methodKeys)

	return r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := globalAuthorization(
			ctx,
			q,
			actorID,
			actorID,
			"control.global.role.permission.replace",
		)
		if err != nil {
			return err
		}
		role, err := q.GetGlobalRole(ctx, roleID)
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
			Scope:      string(ScopeGlobal),
			MethodKeys: methodKeys,
		})
		if err != nil {
			return err
		}
		if count != int64(len(methodKeys)) {
			return ErrMethodNotFound
		}
		if !bundle.ActorIsOwner {
			methods, err := q.ListAuthorizedGlobalMethods(ctx, actorID)
			if err != nil {
				return err
			}
			if !methodSubset(methodKeys, authorizedGlobalKeys(methods)) {
				return ErrForbidden
			}
		}

		return q.ReplaceGlobalRolePermissions(
			ctx,
			controlsqlc.ReplaceGlobalRolePermissionsParams{
				RoleID:     roleID,
				MethodKeys: methodKeys,
			},
		)
	})

}

func (r *Repository) ListGlobalRolePermissions(
	ctx context.Context,
	roleID string,
) ([]string, error) {

	return r.q.ListGlobalRolePermissions(ctx, roleID)

}

func globalAuthorization(
	ctx context.Context,
	q *controlsqlc.Queries,
	actorID string,
	targetAccountID string,
	methodKey string,
) (controlsqlc.GetGlobalAuthorizationForUpdateRow, error) {

	bundle, err := q.GetGlobalAuthorizationForUpdate(
		ctx,
		controlsqlc.GetGlobalAuthorizationForUpdateParams{
			ActorID:         actorID,
			MethodKey:       methodKey,
			TargetAccountID: targetAccountID,
		},
	)
	if err != nil {
		return controlsqlc.GetGlobalAuthorizationForUpdateRow{}, noRows(err, ErrForbidden)
	}
	if !bundle.ActorIsActive || !allowed(bundle.Allowed) {
		return controlsqlc.GetGlobalAuthorizationForUpdateRow{}, ErrForbidden
	}

	return bundle, nil

}

func authorizedGlobalKeys(rows []controlsqlc.ListAuthorizedGlobalMethodsRow) map[string]struct{} {

	result := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		result[row.MethodKey] = struct{}{}
	}

	return result

}

func methodSubset(values []string, allowedValues map[string]struct{}) bool {

	for _, value := range values {
		if _, ok := allowedValues[value]; !ok {
			return false
		}
	}

	return true

}

func uniqueStrings(values []string) []string {

	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result

}

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	controlsqlc "github.com/elum2b/services/control/sqlc"
	"github.com/google/uuid"
)

func (r *Repository) UpdateWorkspace(
	ctx context.Context,
	actorID, workspaceID, slug, title, status string,
) (int64, error) {
	if err := required(actorID, workspaceID, slug, title, status); err != nil {
		return 0, err
	}
	var rows int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := authorizeWorkspaceMutation(
			ctx,
			q,
			actorID,
			workspaceID,
			accessWorkspaceUpdate,
		); err != nil {
			return err
		}

		var writeErr error
		rows, writeErr = q.UpdateWorkspaceAsActiveMember(ctx, controlsqlc.UpdateWorkspaceAsActiveMemberParams{
			Slug:      slug,
			Title:     title,
			Status:    status,
			ID:        workspaceID,
			AccountID: actorID,
		})
		return writeErr
	})
	if err != nil || rows > 0 {
		return rows, err
	}
	if _, err := r.GetWorkspace(ctx, workspaceID); err != nil {
		return 0, err
	}
	if err := r.requireActiveMember(ctx, actorID, workspaceID); err != nil {
		return 0, err
	}
	return rows, nil
}

func (r *Repository) ListMembers(ctx context.Context, workspaceID string, limit, offset int32) ([]Member, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	if err := required(workspaceID); err != nil {
		return nil, err
	}
	rows, err := r.q.ListWorkspaceMembers(
		ctx,
		controlsqlc.ListWorkspaceMembersParams{WorkspaceID: workspaceID, Limit: limit, Offset: offset},
	)
	if err != nil {
		return nil, err
	}
	result := make([]Member, 0, len(rows))
	for _, row := range rows {
		result = append(result, Member{
			WorkspaceID: row.WorkspaceID,
			AccountID:   row.AccountID,
			DisplayName: row.DisplayName,
			Position:    coercePosition(row.Position),
			JoinedAt:    row.JoinedAt,
			UpdatedAt:   row.UpdatedAt,
		})
	}
	return result, nil
}

func (r *Repository) RemoveMember(ctx context.Context, actorID, workspaceID, accountID string) (int64, error) {
	var rows int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := authorizeWorkspaceMutation(ctx, q, actorID, workspaceID, accessMemberRemove); err != nil {
			return err
		}
		if err := requireActorHigherWithQueries(
			ctx,
			q,
			actorID,
			workspaceID,
			accountID,
			2147483647,
		); err != nil {
			return err
		}

		var err error
		rows, err = q.RemoveWorkspaceMember(
			ctx,
			controlsqlc.RemoveWorkspaceMemberParams{WorkspaceID: workspaceID, AccountID: accountID},
		)
		if err != nil || rows == 0 {
			return err
		}
		_, err = q.RemoveWorkspaceMemberRoles(
			ctx,
			controlsqlc.RemoveWorkspaceMemberRolesParams{WorkspaceID: workspaceID, AccountID: accountID},
		)
		return err
	})
	if err != nil || rows == 0 {
		return rows, err
	}
	return rows, r.touchAuthVersion(ctx, workspaceID)
}

func (r *Repository) CreateInvite(
	ctx context.Context,
	actorID string,
	workspaceID string,
	roleIDs []string,
	expiresAt *time.Time,
	maxUses *uint32,
) (Invite, string, error) {
	if err := required(actorID, workspaceID); err != nil {
		return Invite{}, "", err
	}
	if maxUses != nil && (*maxUses == 0 || *maxUses > math.MaxInt32) {
		return Invite{}, "", ErrInviteMaxUses
	}

	rawToken, err := randomToken()
	if err != nil {
		return Invite{}, "", err
	}
	invite := Invite{
		ID:          uuid.NewString(),
		WorkspaceID: workspaceID,
		CreatedBy:   actorID,
		ExpiresAt:   expiresAt,
		MaxUses:     maxUses,
		RoleIDs:     append([]string(nil), roleIDs...),
	}
	err = r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := authorizeWorkspaceMutation(ctx, q, actorID, workspaceID, accessInviteCreate); err != nil {
			return err
		}
		for _, roleID := range roleIDs {
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
		}

		var sqlMaxUses sql.NullInt32
		if maxUses != nil {
			sqlMaxUses = sql.NullInt32{Int32: int32(*maxUses), Valid: true}
		}
		var sqlExpiresAt sql.NullTime
		if expiresAt != nil {
			sqlExpiresAt = sql.NullTime{Time: *expiresAt, Valid: true}
		}
		if err := q.CreateInvite(ctx, controlsqlc.CreateInviteParams{
			ID:          invite.ID,
			WorkspaceID: workspaceID,
			CreatedBy:   actorID,
			TokenHash:   tokenHash(rawToken),
			MaxUses:     sqlMaxUses,
			ExpiresAt:   sqlExpiresAt,
		}); err != nil {
			return err
		}
		for _, roleID := range roleIDs {
			if err := q.AddInviteRole(ctx, controlsqlc.AddInviteRoleParams{InviteID: invite.ID, RoleID: roleID}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Invite{}, "", err
	}
	return invite, rawToken, nil
}

func (r *Repository) AcceptInvite(ctx context.Context, accountID, rawToken string) (Invite, error) {
	if err := required(accountID, rawToken); err != nil {
		return Invite{}, err
	}

	var invite Invite
	var changed bool
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		row, err := q.GetInviteByHashForUpdate(ctx, tokenHash(rawToken))
		if err != nil {
			return noRows(err, ErrNotFound)
		}
		roleIDs, err := q.ListInviteRoles(ctx, row.ID)
		if err != nil {
			return err
		}
		invite = mapInvite(row, roleIDs)

		accepted, err := q.CreateInviteAcceptance(ctx, controlsqlc.CreateInviteAcceptanceParams{
			InviteID:  row.ID,
			AccountID: accountID,
		})
		if err != nil {
			return err
		}
		if accepted == 0 {
			return nil
		}

		if err := q.AddWorkspaceMember(ctx, controlsqlc.AddWorkspaceMemberParams{WorkspaceID: row.WorkspaceID, AccountID: accountID}); err != nil {
			return err
		}
		for _, roleID := range roleIDs {
			if err := q.AddRoleMember(ctx, controlsqlc.AddRoleMemberParams{RoleID: roleID, AccountID: accountID}); err != nil {
				return err
			}
		}
		updated, err := q.IncrementInviteUse(ctx, row.ID)
		if err != nil || updated != 1 {
			if err != nil {
				return err
			}
			return ErrNotFound
		}
		changed = true

		return nil
	})
	if err != nil {
		return Invite{}, err
	}
	if changed {
		if err := r.touchAuthVersion(ctx, invite.WorkspaceID); err != nil {
			return Invite{}, err
		}
	}

	return invite, nil

}

func (r *Repository) ListInvites(ctx context.Context, workspaceID string, limit, offset int32) ([]Invite, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	rows, err := r.q.ListInvites(
		ctx,
		controlsqlc.ListInvitesParams{WorkspaceID: workspaceID, Limit: limit, Offset: offset},
	)
	if err != nil {
		return nil, err
	}
	result := make([]Invite, 0, len(rows))
	for _, row := range rows {
		roleIDs, err := r.q.ListInviteRoles(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, mapInvite(row, roleIDs))
	}
	return result, nil
}

func (r *Repository) RevokeInvite(ctx context.Context, actorID, workspaceID, inviteID string) (int64, error) {
	if err := required(actorID, workspaceID, inviteID); err != nil {
		return 0, err
	}
	var rows int64
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if err := authorizeWorkspaceMutation(ctx, q, actorID, workspaceID, accessInviteRevoke); err != nil {
			return err
		}

		var writeErr error
		rows, writeErr = q.RevokeInviteAsActiveMember(ctx, controlsqlc.RevokeInviteAsActiveMemberParams{
			ID:          inviteID,
			WorkspaceID: workspaceID,
			AccountID:   actorID,
		})
		return writeErr
	})
	if err != nil || rows > 0 {
		return rows, err
	}
	if err := r.requireActiveMember(ctx, actorID, workspaceID); err != nil {
		return 0, err
	}
	return rows, nil
}

func mapInvite(row controlsqlc.ControlWorkspaceInvite, roleIDs []string) Invite {
	result := Invite{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		CreatedBy:   row.CreatedBy,
		UsedCount:   uint32Pointer(row.UsedCount),
		CreatedAt:   row.CreatedAt,
		RoleIDs:     append([]string(nil), roleIDs...),
	}
	if row.MaxUses.Valid {
		result.MaxUses = uint32Pointer(row.MaxUses.Int32)
	}
	if row.ExpiresAt.Valid {
		result.ExpiresAt = &row.ExpiresAt.Time
	}
	if row.RevokedAt.Valid {
		result.RevokedAt = &row.RevokedAt.Time
	}
	return result
}

func uint32Pointer(value int32) *uint32 {
	result := uint32(value)
	return &result
}

func coercePosition(value any) int32 {
	switch value := value.(type) {
	case int64:
		return int32(value)
	case uint64:
		return int32(value)
	case int32:
		return value
	case int:
		return int32(value)
	case []byte:
		var result int32
		_, _ = fmt.Sscan(string(value), &result)
		return result
	default:
		return 2147483647
	}
}

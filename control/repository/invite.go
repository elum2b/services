package repository

import (
	"context"
	"database/sql"
	"strings"
	"time"

	controlmodel "github.com/elum2b/services/control/model"
	controlsqlc "github.com/elum2b/services/control/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/google/uuid"
)

type CreateInviteInput struct {
	ActorID     string
	WorkspaceID string
	RoleIDs     []string
	ExpiresAt   *time.Time
}

func (r *Repository) CreateGlobalInvite(
	ctx context.Context,
	input CreateInviteInput,
) (Invite, string, error) {

	return r.createInvite(ctx, InviteKindGlobal, input)

}

func (r *Repository) CreateWorkspaceInvite(
	ctx context.Context,
	input CreateInviteInput,
) (Invite, string, error) {

	if err := requireWorkspaceID(input.WorkspaceID); err != nil {
		return Invite{}, "", err
	}

	return r.createInvite(ctx, InviteKindWorkspace, input)

}

func (r *Repository) createInvite(
	ctx context.Context,
	kind InviteKind,
	input CreateInviteInput,
) (Invite, string, error) {

	if err := required(input.ActorID); err != nil {
		return Invite{}, "", err
	}

	roleIDs := make([]string, 0, len(input.RoleIDs))
	for _, roleID := range input.RoleIDs {
		roleIDs = append(roleIDs, strings.TrimSpace(roleID))
	}
	roleIDs = uniqueStrings(roleIDs)

	rawToken, err := randomToken()
	if err != nil {
		return Invite{}, "", err
	}

	invite := Invite{
		ID:          uuid.NewString(),
		Kind:        kind,
		WorkspaceID: input.WorkspaceID,
		CreatedBy:   input.ActorID,
		ExpiresAt:   input.ExpiresAt,
		CreatedAt:   time.Now(),
		RoleIDs:     append([]string(nil), roleIDs...),
	}
	if event, ok := auditFromContext(ctx); ok &&
		event.TargetType == "invite" &&
		event.TargetID == "" {
		event.TargetID = invite.ID
		ctx = WithAudit(ctx, event)
	}

	err = r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		if kind == InviteKindGlobal {
			bundle, err := q.GetGlobalAuthorizationForUpdate(
				ctx,
				controlsqlc.GetGlobalAuthorizationForUpdateParams{
					ActorID:         input.ActorID,
					MethodKey:       "control.global.invite.create",
					TargetAccountID: input.ActorID,
				},
			)
			if err != nil {
				return err
			}
			if !bundle.ActorIsActive || !allowed(bundle.Allowed) {
				return ErrForbidden
			}

			actorPosition, err := position(bundle.ActorPosition)
			if err != nil {
				return err
			}
			for _, roleID := range roleIDs {
				role, err := q.GetGlobalRole(ctx, roleID)
				if err != nil {
					return noRows(err, ErrRoleNotFound)
				}
				if err := requireHigher(actorPosition, role.Position); err != nil {
					return err
				}
			}
		} else {
			bundle, err := q.GetWorkspaceAuthorizationForUpdate(
				ctx,
				controlsqlc.GetWorkspaceAuthorizationForUpdateParams{
					ActorID:         input.ActorID,
					MethodKey:       "control.workspace.invite.create",
					TargetAccountID: input.ActorID,
					WorkspaceID:     input.WorkspaceID,
				},
			)
			if err != nil {
				return noRows(err, ErrWorkspaceNotFound)
			}
			if !bundle.ActorIsActive || !allowed(bundle.Allowed) {
				return ErrForbidden
			}
			if bundle.EmployeeCount+bundle.PendingInviteCount >= int64(bundle.EmployeeLimit) {
				return ErrEmployeeLimit
			}

			actorPosition, err := position(bundle.ActorPosition)
			if err != nil {
				return err
			}
			for _, roleID := range roleIDs {
				role, err := q.GetWorkspaceRole(
					ctx,
					controlsqlc.GetWorkspaceRoleParams{
						ID:          roleID,
						WorkspaceID: input.WorkspaceID,
					},
				)
				if err != nil {
					return noRows(err, ErrRoleNotFound)
				}
				if err := requireHigher(actorPosition, role.Position); err != nil {
					return err
				}
			}
		}

		if err := q.CreateInvite(ctx, controlsqlc.CreateInviteParams{
			ID:          invite.ID,
			Kind:        string(kind),
			WorkspaceID: nullableString(input.WorkspaceID),
			CreatedBy:   input.ActorID,
			TokenHash:   tokenHash(rawToken),
			ExpiresAt:   nullableTime(input.ExpiresAt),
		}); err != nil {
			return err
		}

		if len(roleIDs) == 0 {
			return nil
		}
		if kind == InviteKindGlobal {
			return q.AddInviteGlobalRoles(ctx, controlsqlc.AddInviteGlobalRolesParams{
				InviteID: invite.ID,
				RoleIds:  roleIDs,
			})
		}

		return q.AddInviteWorkspaceRoles(ctx, controlsqlc.AddInviteWorkspaceRolesParams{
			InviteID:    invite.ID,
			WorkspaceID: input.WorkspaceID,
			RoleIds:     roleIDs,
		})
	})
	if err != nil {
		return Invite{}, "", err
	}

	return invite, rawToken, nil

}

func (r *Repository) AcceptInvite(
	ctx context.Context,
	accountID string,
	rawToken string,
) (Invite, error) {

	if err := required(accountID, rawToken); err != nil {
		return Invite{}, err
	}

	var result Invite
	err := sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(_ *sql.Tx, q *controlsqlc.Queries) error {
			row, err := getInviteByHashForAcceptance(ctx, q, tokenHash(rawToken))
			if err != nil {
				return noRows(err, ErrInviteUnavailable)
			}

			result = mapInvite(row)
			result.RoleIDs, err = listInviteRoles(ctx, q, row)
			if err != nil {
				return err
			}
			if row.AcceptedBy.Valid {
				if row.AcceptedBy.String != accountID {
					return ErrInviteUnavailable
				}
				return nil
			}

			return r.acceptInviteRowWithQueries(ctx, q, row, accountID)
		},
	)
	if err != nil {
		return Invite{}, err
	}

	return result, nil

}

func (r *Repository) ListGlobalInvites(
	ctx context.Context,
	cursor Cursor,
	limit int32,
) ([]Invite, error) {

	rows, err := r.q.ListGlobalInvites(ctx, controlsqlc.ListGlobalInvitesParams{
		CursorAt:  nullableCursorTime(cursor),
		CursorID:  cursor.ID,
		PageLimit: pageLimit(limit),
	})
	if err != nil {
		return nil, err
	}

	result := make([]Invite, 0, len(rows))
	for _, row := range rows {
		invite := mapInviteFields(
			row.ID,
			row.Kind,
			row.WorkspaceID,
			row.CreatedBy,
			row.ExpiresAt,
			row.AcceptedBy,
			row.AcceptedAt,
			row.RevokedAt,
			row.CreatedAt,
		)
		invite.RoleIDs, err = r.q.ListInviteGlobalRoles(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, invite)
	}

	return result, nil

}

func (r *Repository) ListWorkspaceInvites(
	ctx context.Context,
	workspaceID string,
	cursor Cursor,
	limit int32,
) ([]Invite, error) {

	rows, err := r.q.ListWorkspaceInvites(
		ctx,
		controlsqlc.ListWorkspaceInvitesParams{
			WorkspaceID: workspaceID,
			CursorAt:    nullableCursorTime(cursor),
			CursorID:    cursor.ID,
			PageLimit:   pageLimit(limit),
		},
	)
	if err != nil {
		return nil, err
	}

	result := make([]Invite, 0, len(rows))
	for _, row := range rows {
		invite := mapInviteFields(
			row.ID,
			row.Kind,
			row.WorkspaceID,
			row.CreatedBy,
			row.ExpiresAt,
			row.AcceptedBy,
			row.AcceptedAt,
			row.RevokedAt,
			row.CreatedAt,
		)
		invite.RoleIDs, err = r.q.ListInviteWorkspaceRoles(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, invite)
	}

	return result, nil

}

func (r *Repository) acceptInviteRowWithQueries(
	ctx context.Context,
	q *controlsqlc.Queries,
	invite controlsqlc.ControlInvite,
	accountID string,
) error {

	if err := validateInvite(invite, time.Now()); err != nil {
		return err
	}

	switch InviteKind(invite.Kind) {
	case InviteKindGlobal:
		if err := q.AddGlobalRolesFromInvite(ctx, controlsqlc.AddGlobalRolesFromInviteParams{
			AccountID: accountID,
			InviteID:  invite.ID,
		}); err != nil {
			return err
		}
	case InviteKindWorkspace:
		if !invite.WorkspaceID.Valid {
			return ErrInviteUnavailable
		}

		active, err := q.IsActiveWorkspaceMember(
			ctx,
			controlsqlc.IsActiveWorkspaceMemberParams{
				WorkspaceID: invite.WorkspaceID.String,
				AccountID:   accountID,
			},
		)
		if err != nil {
			return err
		}
		if !active {
			capacity, err := q.GetWorkspaceCapacityForUpdate(ctx, invite.WorkspaceID.String)
			if err != nil {
				return noRows(err, ErrWorkspaceNotFound)
			}
			if capacity.EmployeeCount >= int64(capacity.EmployeeLimit) {
				return ErrEmployeeLimit
			}
			if err := q.AddWorkspaceMember(ctx, controlsqlc.AddWorkspaceMemberParams{
				WorkspaceID: invite.WorkspaceID.String,
				AccountID:   accountID,
			}); err != nil {
				return err
			}
		}

		if err := q.AddWorkspaceRolesFromInvite(
			ctx,
			controlsqlc.AddWorkspaceRolesFromInviteParams{
				AccountID: accountID,
				InviteID:  invite.ID,
			},
		); err != nil {
			return err
		}
	default:
		return ErrInviteUnavailable
	}

	rows, err := q.AcceptInvite(ctx, controlsqlc.AcceptInviteParams{
		AcceptedBy: sql.NullString{
			String: accountID,
			Valid:  true,
		},
		ID: invite.ID,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrInviteUnavailable
	}

	return nil

}

func (r *Repository) RevokeInvite(
	ctx context.Context,
	actorID string,
	inviteID string,
) (int64, error) {

	var rows int64
	err := sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(_ *sql.Tx, q *controlsqlc.Queries) error {
			snapshot, err := q.GetInvite(ctx, inviteID)
			if err != nil {
				return noRows(err, ErrNotFound)
			}

			event := AuditEvent{
				ActorID:    actorID,
				TargetType: "invite",
				TargetID:   inviteID,
				Result:     controlmodel.AuditResultSucceeded,
			}
			if InviteKind(snapshot.Kind) == InviteKindGlobal {
				if _, err := globalAuthorization(
					ctx,
					q,
					actorID,
					actorID,
					"control.global.invite.revoke",
				); err != nil {
					return err
				}
				event.Scope = ScopeGlobal
				event.MethodKey = "control.global.invite.revoke"
			} else {
				workspaceID := valueString(snapshot.WorkspaceID)
				if _, err := workspaceAuthorization(
					ctx,
					q,
					actorID,
					workspaceID,
					actorID,
					"control.workspace.invite.revoke",
				); err != nil {
					return err
				}
				event.Scope = ScopeWorkspace
				event.WorkspaceID = workspaceID
				event.MethodKey = "control.workspace.invite.revoke"
			}

			invite, err := q.GetInviteForUpdate(ctx, inviteID)
			if err != nil {
				return noRows(err, ErrNotFound)
			}
			if !sameInviteScope(snapshot, invite) {
				return ErrInviteUnavailable
			}

			rows, err = q.RevokeInvite(ctx, inviteID)
			if err != nil {
				return err
			}
			if rows != 1 {
				return ErrInviteUnavailable
			}

			return appendAudit(ctx, q, event)
		},
	)

	return rows, err

}

func getInviteByHashForAcceptance(
	ctx context.Context,
	q *controlsqlc.Queries,
	tokenHash string,
) (controlsqlc.ControlInvite, error) {

	snapshot, err := q.GetInviteByHash(ctx, tokenHash)
	if err != nil {
		return controlsqlc.ControlInvite{}, err
	}
	if err := lockInviteAcceptanceScope(ctx, q, snapshot); err != nil {
		return controlsqlc.ControlInvite{}, err
	}

	invite, err := q.GetInviteByHashForUpdate(ctx, tokenHash)
	if err != nil {
		return controlsqlc.ControlInvite{}, err
	}
	if !sameInviteScope(snapshot, invite) {
		return controlsqlc.ControlInvite{}, ErrInviteUnavailable
	}

	return invite, nil

}

func getInviteByIDForAcceptance(
	ctx context.Context,
	q *controlsqlc.Queries,
	inviteID string,
) (controlsqlc.ControlInvite, error) {

	snapshot, err := q.GetInvite(ctx, inviteID)
	if err != nil {
		return controlsqlc.ControlInvite{}, err
	}
	if err := lockInviteAcceptanceScope(ctx, q, snapshot); err != nil {
		return controlsqlc.ControlInvite{}, err
	}

	invite, err := q.GetInviteForUpdate(ctx, inviteID)
	if err != nil {
		return controlsqlc.ControlInvite{}, err
	}
	if !sameInviteScope(snapshot, invite) {
		return controlsqlc.ControlInvite{}, ErrInviteUnavailable
	}

	return invite, nil

}

func lockInviteAcceptanceScope(
	ctx context.Context,
	q *controlsqlc.Queries,
	invite controlsqlc.ControlInvite,
) error {

	switch InviteKind(invite.Kind) {
	case InviteKindGlobal:
		return nil
	case InviteKindWorkspace:
		if !invite.WorkspaceID.Valid {
			return ErrInviteUnavailable
		}

		_, err := q.GetWorkspaceCapacityForUpdate(ctx, invite.WorkspaceID.String)

		return noRows(err, ErrWorkspaceNotFound)
	default:
		return ErrInviteUnavailable
	}

}

func sameInviteScope(left, right controlsqlc.ControlInvite) bool {

	return left.ID == right.ID &&
		left.Kind == right.Kind &&
		valueString(left.WorkspaceID) == valueString(right.WorkspaceID)

}

func validateInvite(value controlsqlc.ControlInvite, now time.Time) error {

	if value.AcceptedAt.Valid || value.RevokedAt.Valid {
		return ErrInviteUnavailable
	}
	if value.ExpiresAt.Valid && !value.ExpiresAt.Time.After(now) {
		return ErrInviteUnavailable
	}

	return nil

}

func mapInvite(value controlsqlc.ControlInvite) Invite {

	return mapInviteFields(
		value.ID,
		value.Kind,
		value.WorkspaceID,
		value.CreatedBy,
		value.ExpiresAt,
		value.AcceptedBy,
		value.AcceptedAt,
		value.RevokedAt,
		value.CreatedAt,
	)

}

func mapInviteFields(
	id string,
	kind string,
	workspaceID sql.NullString,
	createdBy string,
	expiresAt sql.NullTime,
	acceptedBy sql.NullString,
	acceptedAt sql.NullTime,
	revokedAt sql.NullTime,
	createdAt time.Time,
) Invite {

	result := Invite{
		ID:          id,
		Kind:        InviteKind(kind),
		WorkspaceID: valueString(workspaceID),
		CreatedBy:   createdBy,
		AcceptedBy:  valueString(acceptedBy),
		CreatedAt:   createdAt,
	}
	if expiresAt.Valid {
		result.ExpiresAt = &expiresAt.Time
	}
	if acceptedAt.Valid {
		result.AcceptedAt = &acceptedAt.Time
	}
	if revokedAt.Valid {
		result.RevokedAt = &revokedAt.Time
	}

	return result

}

func nullableTime(value *time.Time) sql.NullTime {

	if value == nil {
		return sql.NullTime{}
	}

	return sql.NullTime{
		Time:  *value,
		Valid: true,
	}

}

func listInviteRoles(
	ctx context.Context,
	q *controlsqlc.Queries,
	invite controlsqlc.ControlInvite,
) ([]string, error) {

	if InviteKind(invite.Kind) == InviteKindGlobal {
		return q.ListInviteGlobalRoles(ctx, invite.ID)
	}

	return q.ListInviteWorkspaceRoles(ctx, invite.ID)

}

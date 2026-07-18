package repository

import (
	"context"
	"database/sql"
	"time"

	controlmodel "github.com/elum2b/services/control/model"
	controlsqlc "github.com/elum2b/services/control/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/google/uuid"
)

func (r *Repository) RequestWorkspaceLimit(
	ctx context.Context,
	accountID string,
	requestedLimit int32,
	reason string,
) (LimitRequest, error) {

	var request LimitRequest
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		member, err := q.GetPlatformMemberForUpdate(ctx, accountID)
		if err != nil {
			return noRows(err, ErrForbidden)
		}
		if member.Status != string(controlmodel.MembershipStatusActive) ||
			requestedLimit <= member.WorkspaceLimit {
			return ErrInvalidArgument
		}

		request = LimitRequest{
			ID:             uuid.NewString(),
			Kind:           LimitKindAccountWorkspace,
			AccountID:      accountID,
			CurrentLimit:   member.WorkspaceLimit,
			RequestedLimit: requestedLimit,
			Reason:         reason,
			Status:         controlmodel.LimitRequestStatusPending,
			RequestedBy:    accountID,
			CreatedAt:      time.Now(),
		}

		rows, err := q.CreateLimitRequest(ctx, controlsqlc.CreateLimitRequestParams{
			ID:             request.ID,
			Kind:           string(request.Kind),
			AccountID:      nullableString(accountID),
			WorkspaceID:    sql.NullString{},
			CurrentLimit:   request.CurrentLimit,
			RequestedLimit: request.RequestedLimit,
			Reason:         reason,
			RequestedBy:    accountID,
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrLimitRequest
		}

		return nil
	})
	if err != nil {
		return LimitRequest{}, err
	}

	return request, nil

}

func (r *Repository) RequestEmployeeLimit(
	ctx context.Context,
	actorID string,
	workspaceID string,
	requestedLimit int32,
	reason string,
) (LimitRequest, error) {

	var request LimitRequest
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		bundle, err := workspaceAuthorization(
			ctx,
			q,
			actorID,
			workspaceID,
			actorID,
			"control.workspace.employee_limit.request",
		)
		if err != nil {
			return err
		}
		if !bundle.ActorIsOwner || requestedLimit <= bundle.EmployeeLimit {
			return ErrInvalidArgument
		}

		request = LimitRequest{
			ID:             uuid.NewString(),
			Kind:           LimitKindWorkspaceEmployee,
			WorkspaceID:    workspaceID,
			CurrentLimit:   bundle.EmployeeLimit,
			RequestedLimit: requestedLimit,
			Reason:         reason,
			Status:         controlmodel.LimitRequestStatusPending,
			RequestedBy:    actorID,
			CreatedAt:      time.Now(),
		}

		rows, err := q.CreateLimitRequest(ctx, controlsqlc.CreateLimitRequestParams{
			ID:             request.ID,
			Kind:           string(request.Kind),
			AccountID:      sql.NullString{},
			WorkspaceID:    nullableString(workspaceID),
			CurrentLimit:   request.CurrentLimit,
			RequestedLimit: request.RequestedLimit,
			Reason:         reason,
			RequestedBy:    actorID,
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrLimitRequest
		}

		return nil
	})
	if err != nil {
		return LimitRequest{}, err
	}

	return request, nil

}

func (r *Repository) ListLimitRequests(
	ctx context.Context,
	actorID string,
	status controlmodel.LimitRequestStatus,
	cursor Cursor,
	limit int32,
) ([]LimitRequest, error) {

	switch status {
	case "",
		controlmodel.LimitRequestStatusPending,
		controlmodel.LimitRequestStatusApproved,
		controlmodel.LimitRequestStatusRejected,
		controlmodel.LimitRequestStatusCancelled:
	default:
		return nil, ErrInvalidArgument
	}

	allowed, err := r.CheckGlobalAccess(ctx, actorID, "control.global.limit.list")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrForbidden
	}

	rows, err := r.q.ListLimitRequests(ctx, controlsqlc.ListLimitRequestsParams{
		StatusFilter: string(status),
		CursorAt:     nullableCursorTime(cursor),
		CursorID:     cursor.ID,
		PageLimit:    pageLimit(limit),
	})
	if err != nil {
		return nil, err
	}

	result := make([]LimitRequest, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapLimitRequest(row))
	}

	return result, nil

}

func (r *Repository) ResolveLimitRequest(
	ctx context.Context,
	actorID string,
	requestID string,
	approved bool,
	approvedLimit int32,
	comment string,
) (LimitRequest, error) {

	var result LimitRequest
	err := r.withAuditTx(ctx, func(q *controlsqlc.Queries) error {
		methodKey := "control.global.limit.reject"
		if approved {
			methodKey = "control.global.limit.approve"
		}
		if _, err := globalAuthorization(ctx, q, actorID, actorID, methodKey); err != nil {
			return err
		}

		snapshot, err := q.GetLimitRequest(ctx, requestID)
		if err != nil {
			return noRows(err, ErrLimitRequest)
		}
		switch LimitKind(snapshot.Kind) {
		case LimitKindAccountWorkspace:
			if !snapshot.AccountID.Valid {
				return ErrLimitRequest
			}
			if _, err := q.GetPlatformMemberForUpdate(ctx, snapshot.AccountID.String); err != nil {
				return noRows(err, ErrLimitRequest)
			}
		case LimitKindWorkspaceEmployee:
			if !snapshot.WorkspaceID.Valid {
				return ErrLimitRequest
			}
			if _, err := q.GetWorkspaceCapacityForUpdate(ctx, snapshot.WorkspaceID.String); err != nil {
				return noRows(err, ErrLimitRequest)
			}
		default:
			return ErrLimitRequest
		}

		row, err := q.GetLimitRequestForUpdate(ctx, requestID)
		if err != nil {
			return noRows(err, ErrLimitRequest)
		}
		if row.Status != string(controlmodel.LimitRequestStatusPending) ||
			row.Kind != snapshot.Kind ||
			row.AccountID != snapshot.AccountID ||
			row.WorkspaceID != snapshot.WorkspaceID {
			return ErrLimitRequest
		}

		status := controlmodel.LimitRequestStatusRejected
		approvedValue := sql.NullInt32{}
		if approved {
			if approvedLimit < row.RequestedLimit || approvedLimit <= row.CurrentLimit {
				return ErrInvalidArgument
			}
			status = controlmodel.LimitRequestStatusApproved
			approvedValue = sql.NullInt32{
				Int32: approvedLimit,
				Valid: true,
			}

			switch LimitKind(row.Kind) {
			case LimitKindAccountWorkspace:
				updated, err := q.UpdateAccountWorkspaceLimit(
					ctx,
					controlsqlc.UpdateAccountWorkspaceLimitParams{
						WorkspaceLimit: approvedLimit,
						AccountID:      valueString(row.AccountID),
					},
				)
				if err != nil {
					return err
				}
				if updated != 1 {
					return ErrLimitRequest
				}
			case LimitKindWorkspaceEmployee:
				updated, err := q.UpdateWorkspaceEmployeeLimit(
					ctx,
					controlsqlc.UpdateWorkspaceEmployeeLimitParams{
						EmployeeLimit: approvedLimit,
						ID:            valueString(row.WorkspaceID),
					},
				)
				if err != nil {
					return err
				}
				if updated != 1 {
					return ErrLimitRequest
				}
			default:
				return ErrLimitRequest
			}
		}

		rows, err := q.ResolveLimitRequest(ctx, controlsqlc.ResolveLimitRequestParams{
			Status:        string(status),
			ApprovedLimit: approvedValue,
			ReviewedBy:    nullableString(actorID),
			ReviewComment: comment,
			ID:            requestID,
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrLimitRequest
		}

		result = mapLimitRequest(row)
		result.Status = status
		result.ReviewedBy = actorID
		result.ReviewComment = comment
		reviewedAt := time.Now()
		result.ReviewedAt = &reviewedAt
		result.ApprovedLimit = nil
		if approvedValue.Valid {
			value := approvedValue.Int32
			result.ApprovedLimit = &value
		}

		return nil
	})
	if err != nil {
		return LimitRequest{}, err
	}

	return result, nil

}

func (r *Repository) CancelLimitRequest(
	ctx context.Context,
	accountID string,
	requestID string,
) (int64, error) {

	var affected int64
	err := sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(_ *sql.Tx, q *controlsqlc.Queries) error {
			request, err := q.GetLimitRequestForUpdate(ctx, requestID)
			if err != nil {
				return noRows(err, ErrLimitRequest)
			}
			if request.Status != string(controlmodel.LimitRequestStatusPending) ||
				request.RequestedBy != accountID {
				return ErrLimitRequest
			}

			affected, err = q.CancelLimitRequest(ctx, controlsqlc.CancelLimitRequestParams{
				ID:          requestID,
				RequestedBy: accountID,
			})
			if err != nil {
				return err
			}
			if affected != 1 {
				return ErrLimitRequest
			}

			var event AuditEvent
			switch LimitKind(request.Kind) {
			case LimitKindAccountWorkspace:
				event = AuditEvent{
					Scope:      ScopeGlobal,
					ActorID:    accountID,
					MethodKey:  "control.global.limit.cancel",
					TargetType: "limit_request",
					TargetID:   requestID,
				}
			case LimitKindWorkspaceEmployee:
				event = AuditEvent{
					Scope:       ScopeWorkspace,
					WorkspaceID: valueString(request.WorkspaceID),
					ActorID:     accountID,
					MethodKey:   "control.workspace.employee_limit.cancel",
					TargetType:  "limit_request",
					TargetID:    requestID,
				}
			default:
				return ErrLimitRequest
			}

			return appendAudit(ctx, q, event)
		},
	)

	return affected, err

}

func mapLimitRequest(value controlsqlc.ControlLimitRequest) LimitRequest {

	result := LimitRequest{
		ID:             value.ID,
		Kind:           LimitKind(value.Kind),
		AccountID:      valueString(value.AccountID),
		WorkspaceID:    valueString(value.WorkspaceID),
		CurrentLimit:   value.CurrentLimit,
		RequestedLimit: value.RequestedLimit,
		Reason:         value.Reason,
		Status:         controlmodel.LimitRequestStatus(value.Status),
		RequestedBy:    value.RequestedBy,
		ReviewedBy:     valueString(value.ReviewedBy),
		ReviewComment:  value.ReviewComment,
		CreatedAt:      value.CreatedAt,
	}
	if value.ApprovedLimit.Valid {
		approved := value.ApprovedLimit.Int32
		result.ApprovedLimit = &approved
	}
	if value.ReviewedAt.Valid {
		result.ReviewedAt = &value.ReviewedAt.Time
	}

	return result

}

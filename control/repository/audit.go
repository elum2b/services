package repository

import (
	"context"
	"database/sql"

	controlmodel "github.com/elum2b/services/control/model"
	controlsqlc "github.com/elum2b/services/control/sqlc"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/google/uuid"
)

type auditContextKey struct{}

func WithAudit(ctx context.Context, event AuditEvent) context.Context {

	return context.WithValue(ctx, auditContextKey{}, event)

}

func auditFromContext(ctx context.Context) (AuditEvent, bool) {

	event, ok := ctx.Value(auditContextKey{}).(AuditEvent)

	return event, ok

}

func (r *Repository) withAuditTx(
	ctx context.Context,
	write func(*controlsqlc.Queries) error,
) error {

	return r.withAuditDBTx(ctx, func(_ *sql.Tx, q *controlsqlc.Queries) error {
		return write(q)
	})

}

func (r *Repository) withAuditDBTx(
	ctx context.Context,
	write func(*sql.Tx, *controlsqlc.Queries) error,
) error {

	event, hasAudit := auditFromContext(ctx)

	return sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(tx *sql.Tx, q *controlsqlc.Queries) error {
			if err := write(tx, q); err != nil {
				return err
			}
			if !hasAudit {
				return nil
			}

			return appendAudit(ctx, q, event)
		},
	)

}

func (r *Repository) AppendAudit(ctx context.Context, event AuditEvent) error {

	if err := validateAuditScope(event); err != nil {
		return err
	}
	if err := required(event.MethodKey); err != nil {
		return err
	}

	return appendAudit(ctx, r.q, event)

}

func appendAudit(
	ctx context.Context,
	q *controlsqlc.Queries,
	event AuditEvent,
) error {

	if event.Scope == "" {
		if event.WorkspaceID == "" {
			event.Scope = ScopeGlobal
		} else {
			event.Scope = ScopeWorkspace
		}
	}
	if event.Result == "" {
		event.Result = controlmodel.AuditResultSucceeded
	}
	if event.Result != controlmodel.AuditResultSucceeded &&
		event.Result != controlmodel.AuditResultFailed {
		return ErrInvalidArgument
	}
	if event.ID == "" {
		event.ID = uuid.NewString()
	}

	return q.CreateAuditEvent(ctx, controlsqlc.CreateAuditEventParams{
		ID:          event.ID,
		Scope:       string(event.Scope),
		WorkspaceID: nullableString(event.WorkspaceID),
		ActorID:     nullableString(event.ActorID),
		MethodKey:   event.MethodKey,
		TargetType:  event.TargetType,
		TargetID:    event.TargetID,
		BeforeData:  rawMessageParam(event.BeforeData),
		AfterData:   rawMessageParam(event.AfterData),
		Result:      string(event.Result),
		RequestID:   event.RequestID,
	})

}

func (r *Repository) ListAudit(
	ctx context.Context,
	scope AccessScope,
	workspaceID string,
	cursor Cursor,
	limit int32,
) ([]AuditEvent, error) {

	if err := validateAuditScope(AuditEvent{
		Scope:       scope,
		WorkspaceID: workspaceID,
	}); err != nil {
		return nil, err
	}

	rows, err := r.q.ListAuditEvents(ctx, controlsqlc.ListAuditEventsParams{
		Scope:       string(scope),
		WorkspaceID: workspaceID,
		CursorAt:    nullableCursorTime(cursor),
		CursorID:    cursor.ID,
		PageLimit:   pageLimit(limit),
	})
	if err != nil {
		return nil, err
	}

	result := make([]AuditEvent, 0, len(rows))
	for _, row := range rows {
		result = append(result, AuditEvent{
			ID:          row.ID,
			Scope:       AccessScope(row.Scope),
			WorkspaceID: valueString(row.WorkspaceID),
			ActorID:     valueString(row.ActorID),
			MethodKey:   row.MethodKey,
			TargetType:  row.TargetType,
			TargetID:    row.TargetID,
			BeforeData:  nullRawMessage(row.BeforeData),
			AfterData:   nullRawMessage(row.AfterData),
			Result:      controlmodel.AuditResult(row.Result),
			RequestID:   row.RequestID,
			OccurredAt:  row.OccurredAt,
		})
	}

	return result, nil

}

func validateAuditScope(event AuditEvent) error {

	switch event.Scope {
	case ScopeGlobal:
		if event.WorkspaceID != "" {
			return ErrInvalidArgument
		}
	case ScopeWorkspace:
		if err := requireWorkspaceID(event.WorkspaceID); err != nil {
			return err
		}
	default:
		return ErrInvalidArgument
	}

	return nil

}

func nullableString(value string) sql.NullString {

	return sql.NullString{
		String: value,
		Valid:  value != "",
	}

}

func valueString(value sql.NullString) string {

	if value.Valid {
		return value.String
	}

	return ""

}

func nullableCursorTime(cursor Cursor) sql.NullTime {

	return sql.NullTime{
		Time:  cursor.Time,
		Valid: !cursor.Time.IsZero(),
	}

}

func pageLimit(limit int32) int32 {

	if limit <= 0 || limit > 100 {
		return 100
	}

	return limit

}

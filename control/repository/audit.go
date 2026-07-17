package repository

import (
	"context"
	"database/sql"

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

func (r *Repository) withAuditTx(ctx context.Context, write func(*controlsqlc.Queries) error) error {
	event, hasAudit := auditFromContext(ctx)

	return sqlwrap.WithTx(
		ctx,
		r.db.DB(),
		func(tx *sql.Tx) *controlsqlc.Queries {
			return controlsqlc.New(tx)
		},
		func(_ *sql.Tx, q *controlsqlc.Queries) error {
			if err := write(q); err != nil {
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
	if event.WorkspaceID != "" {
		if err := requireWorkspaceID(event.WorkspaceID); err != nil {
			return err
		}
	}

	if err := required(event.MethodKey, event.Result); err != nil {
		return err
	}
	return appendAudit(ctx, r.q, event)
}

func appendAudit(ctx context.Context, q *controlsqlc.Queries, event AuditEvent) error {
	if event.Result == "" {
		event.Result = "succeeded"
	}
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	return q.CreateAuditEvent(ctx, controlsqlc.CreateAuditEventParams{
		ID:          event.ID,
		WorkspaceID: nullableString(event.WorkspaceID),
		ActorID:     nullableString(event.ActorID),
		MethodKey:   event.MethodKey,
		TargetType:  event.TargetType,
		TargetID:    event.TargetID,
		BeforeData:  rawMessageParam(event.BeforeData),
		AfterData:   rawMessageParam(event.AfterData),
		Result:      event.Result,
		RequestID:   event.RequestID,
	})
}

func (r *Repository) ListAudit(ctx context.Context, workspaceID string, limit, offset int32) ([]AuditEvent, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	rows, err := r.q.ListAuditEvents(
		ctx,
		controlsqlc.ListAuditEventsParams{WorkspaceID: nullableString(workspaceID), Limit: limit, Offset: offset},
	)
	if err != nil {
		return nil, err
	}
	result := make([]AuditEvent, 0, len(rows))
	for _, row := range rows {
		result = append(
			result,
			AuditEvent{
				ID:          row.ID,
				WorkspaceID: valueString(row.WorkspaceID),
				ActorID:     valueString(row.ActorID),
				MethodKey:   row.MethodKey,
				TargetType:  row.TargetType,
				TargetID:    row.TargetID,
				BeforeData:  nullRawMessage(row.BeforeData),
				AfterData:   nullRawMessage(row.AfterData),
				Result:      row.Result,
				RequestID:   row.RequestID,
				OccurredAt:  row.OccurredAt,
			},
		)
	}
	return result, nil
}

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}
func valueString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

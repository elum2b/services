package repository

import (
	"context"
	"time"

	calendarsqlc "github.com/elum2b/services/calendar/sqlc"
)

func (r *Repository) GetProgress(ctx context.Context, identity Identity, calendarID string) (*Progress, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}

	row, err := r.q.GetProgress(ctx, calendarsqlc.GetProgressParams{
		WorkspaceID: identity.WorkspaceID, CalendarID: calendarID,
		AppID: identity.AppID, PlatformID: identity.PlatformID,
		PlatformUserID: identity.PlatformUserID,
	})
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &Progress{
		CurrentPosition: uint32(row.CurrentPosition), ClaimCount: uint64(row.ClaimCount),
		LastClaimPosition: sqlNullUint32Ptr(row.LastClaimPosition),
		LastClaimAt:       sqlNullTimePtr(row.LastClaimAt), NextClaimAt: sqlNullTimePtr(row.NextClaimAt),
		IsCompleted: row.IsCompleted, ResetCount: uint64(row.ResetCount),
		LastWasReset: row.LastWasReset,
	}, nil
}

func (r *Repository) Next(
	ctx context.Context,
	identity Identity,
	ref, locale string,
	now time.Time,
) (RecordResult, error) {
	if err := identity.Validate(); err != nil {
		return RecordResult{}, err
	}

	calendar, err := r.GetCalendar(ctx, identity.WorkspaceID, ref, locale)
	if err != nil || calendar.ID == "" {
		return RecordResult{Status: StatusNotFound, Calendar: calendar}, err
	}
	progress, err := r.GetProgress(ctx, identity, calendar.ID)
	if err != nil {
		return RecordResult{}, err
	}
	value := Progress{}
	if progress != nil {
		value = *progress
	}
	result, err := calculateRecord(calendar, value, "", now)
	result.OperationID = ""
	return result, err
}

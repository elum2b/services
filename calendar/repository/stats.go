package repository

import (
	"context"
	"fmt"
	"time"

	calendarsqlc "github.com/elum2b/services/calendar/sqlc"
)

func (r *Repository) ListOperations(
	ctx context.Context,
	workspaceID, calendarID string,
	limit, offset int32,
) ([]Operation, error) {
	limit, offset = normalizePage(limit, offset)
	rows, err := r.q.AdminListOperations(ctx, calendarsqlc.AdminListOperationsParams{
		WorkspaceID: workspaceID, CalendarID: calendarID, Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, err
	}
	result := make([]Operation, 0, len(rows))
	for _, row := range rows {
		result = append(result, Operation{
			ID: uint64(row.ID), Identity: Identity{
				WorkspaceID: row.WorkspaceID, AppID: row.AppID,
				PlatformID: row.PlatformID, PlatformUserID: row.PlatformUserID,
			},
			CalendarID: row.CalendarID, OperationID: row.OperationID,
			Granted: row.Granted, Status: row.Status, Position: sqlNullUint32Ptr(row.Position),
			Rewards: row.RewardsSnapshot, CurrentPosition: uint32(row.CurrentPosition),
			ClaimCount: uint64(row.ClaimCount), OccurredAt: row.OccurredAt,
		})
	}
	return result, nil
}

func (r *Repository) GetStats(ctx context.Context, workspaceID, calendarID string) (Stats, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Stats{}, err
	}

	row, err := r.q.AdminGetStats(ctx, calendarsqlc.AdminGetStatsParams{
		WorkspaceID: workspaceID, CalendarID: calendarID,
	})
	if err != nil {
		return Stats{}, err
	}
	return Stats{
		OperationCount: uint64(row.OperationCount), GrantCount: uint64(row.GrantCount),
		UniqueUsers: uint64(row.UniqueUsers),
	}, nil
}

func (r *Repository) ListDailyStats(
	ctx context.Context,
	workspaceID, calendarID string,
	from, until time.Time,
) ([]DailyStats, error) {
	rows, err := r.q.AdminListDailyStats(ctx, calendarsqlc.AdminListDailyStatsParams{
		WorkspaceID: workspaceID, CalendarID: calendarID, StatsDate: from, StatsDate_2: until,
	})
	if err != nil {
		return nil, err
	}
	result := make([]DailyStats, 0, len(rows))
	for _, row := range rows {
		result = append(result, DailyStats{
			Date: row.StatsDate, OperationCount: uint64(row.OperationCount),
			GrantCount: uint64(row.GrantCount), UniqueUsers: uint64(row.UniqueUsers),
		})
	}
	return result, nil
}

func (r *Repository) RefreshDailyStats(ctx context.Context, workspaceID string, from, until time.Time) error {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}
	if from.IsZero() || until.IsZero() || from.After(until) {
		return fmt.Errorf("calendar stats workspace or date range is invalid")
	}

	return r.q.RefreshDailyStats(ctx, calendarsqlc.RefreshDailyStatsParams{
		RefreshWorkspaceID: workspaceID,
		OccurredAt:         from,
		OccurredAt_2:       until,
	})
}

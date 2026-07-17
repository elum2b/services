package repository

import (
	"context"
	"time"

	cpasqlc "github.com/elum2b/services/cpa/sqlc"
)

func (r *Repository) GetStats(ctx context.Context, workspaceID, cpaID string) (Stats, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return Stats{}, err
	}

	assignmentStats, err := r.q.AdminGetOfferStats(ctx, cpasqlc.AdminGetOfferStatsParams{
		WorkspaceID: workspaceID,
		CpaID:       cpaID,
	})
	if err != nil {
		return Stats{}, err
	}
	codeStats, err := r.q.AdminGetCodeStats(ctx, cpasqlc.AdminGetCodeStatsParams{
		WorkspaceID: workspaceID,
		CpaID:       cpaID,
	})
	if err != nil {
		return Stats{}, err
	}
	return Stats{
		AssignmentsTotal: uint64(assignmentStats.AssignmentsTotal),
		IssuedTotal:      uint64(assignmentStats.IssuedTotal),
		CompletedTotal:   uint64(assignmentStats.CompletedTotal),
		DeletedTotal:     uint64(assignmentStats.DeletedTotal),
		CodesTotal:       uint64(codeStats.CodesTotal),
		AvailableCodes:   uint64(codeStats.AvailableTotal),
		IssuedCodes:      uint64(codeStats.IssuedTotal),
		CompletedCodes:   uint64(codeStats.CompletedTotal),
		DeletedCodes:     uint64(codeStats.DeletedTotal),
	}, nil
}

func (r *Repository) ListDailyStats(ctx context.Context, workspaceID, cpaID string, from, until time.Time) ([]DailyStats, error) {
	if err := requireScope(workspaceID, cpaID); err != nil {
		return nil, err
	}
	if from.IsZero() || until.IsZero() || from.After(until) {
		return nil, ErrInvalidDateRange
	}

	rows, err := r.q.AdminListDailyStats(ctx, cpasqlc.AdminListDailyStatsParams{
		WorkspaceID: workspaceID,
		CpaID:       cpaID,
		StatsDate:   from,
		StatsDate_2: until,
	})
	if err != nil {
		return nil, err
	}
	result := make([]DailyStats, 0, len(rows))
	for _, row := range rows {
		result = append(result, DailyStats{
			Date:           row.StatsDate,
			IssuedCount:    uint64(row.IssuedCount),
			CompletedCount: uint64(row.CompletedCount),
			UniqueUsers:    uint64(row.UniqueUsers),
		})
	}
	return result, nil
}

func (r *Repository) RefreshDailyStats(ctx context.Context, workspaceID string, from, until time.Time) error {
	if err := requireWorkspace(workspaceID); err != nil {
		return err
	}
	if from.IsZero() || until.IsZero() || from.After(until) {
		return ErrInvalidDateRange
	}

	return r.q.RefreshDailyStats(ctx, cpasqlc.RefreshDailyStatsParams{
		RefreshWorkspaceID: workspaceID,
		OccurredAt:         from,
		OccurredAt_2:       until,
	})
}

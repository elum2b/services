package admin

import (
	"context"
	"time"
)

func (a *Admin) GetStats(ctx context.Context, workspaceID, cpaID string) (StatsModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	value, err := a.repository.GetStats(mergedCtx, workspaceID, cpaID)
	if err != nil {
		return StatsModel{}, err
	}

	return StatsModel(value), nil

}

func (a *Admin) ListDailyStats(ctx context.Context, workspaceID, cpaID string, from, until time.Time) ([]DailyStatsModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	values, err := a.repository.ListDailyStats(mergedCtx, workspaceID, cpaID, from, until)
	if err != nil {
		return nil, err
	}

	result := make([]DailyStatsModel, 0, len(values))
	for _, value := range values {
		result = append(result, DailyStatsModel(value))
	}

	return result, nil

}

func (a *Admin) RefreshDailyStats(ctx context.Context, workspaceID string, from, until time.Time) error {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.RefreshDailyStats(mergedCtx, workspaceID, from, until)

}

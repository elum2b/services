package admin

import (
	"context"
	"time"
)

func (a *Admin) GetStats(ctx context.Context, workspaceID string) (StatsModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetStats(mergedCtx, workspaceID)
	if err != nil {
		return StatsModel{}, err
	}
	return StatsModel{
		TasksTotal: value.TasksTotal, ActiveTasks: value.ActiveTasks, VisibleTasks: value.VisibleTasks,
		ProgressTotal: value.ProgressTotal, OpenProgress: value.OpenProgress,
		ReadyProgress: value.ReadyProgress, ClaimedProgress: value.ClaimedProgress,
		ProgressCreated: value.ProgressCreated, ProgressAmount: value.ProgressAmount,
		ReadyCount: value.ReadyCount, ClaimedCount: value.ClaimedCount,
		ManualClaimedCount: value.ManualClaimedCount, AutoClaimedCount: value.AutoClaimedCount,
		UniqueParticipants: value.UniqueParticipants, UniqueClaimers: value.UniqueClaimers,
	}, nil
}

func (a *Admin) GetTaskStats(ctx context.Context, workspaceID string, taskID uint64) (TaskStatsModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetSingleTaskStats(mergedCtx, workspaceID, taskID)
	if err != nil {
		return TaskStatsModel{}, err
	}
	return TaskStatsModel{
		TaskID: value.TaskID, ProgressTotal: value.ProgressTotal,
		OpenProgress: value.OpenProgress, ReadyProgress: value.ReadyProgress,
		ClaimedProgress: value.ClaimedProgress, ProgressCreated: value.ProgressCreated,
		ProgressAmount: value.ProgressAmount, ReadyCount: value.ReadyCount,
		ClaimedCount: value.ClaimedCount, ManualClaimedCount: value.ManualClaimedCount,
		AutoClaimedCount: value.AutoClaimedCount, UniqueParticipants: value.UniqueParticipants,
		UniqueClaimers: value.UniqueClaimers,
	}, nil
}

func (a *Admin) ListDailyStats(
	ctx context.Context,
	workspaceID string,
	taskID uint64,
	from, until time.Time,
) ([]DailyStatsModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	values, err := a.repository.ListDailyStats(mergedCtx, workspaceID, taskID, from, until)
	if err != nil {
		return nil, err
	}
	result := make([]DailyStatsModel, 0, len(values))
	for _, value := range values {
		result = append(result, DailyStatsModel{
			Date: value.Date, TaskID: value.TaskID,
			ProgressCreated: value.ProgressCreated, ProgressAmount: value.ProgressAmount,
			ReadyCount: value.ReadyCount, ClaimedCount: value.ClaimedCount,
			ManualClaimedCount: value.ManualClaimedCount, AutoClaimedCount: value.AutoClaimedCount,
			UniqueParticipants: value.UniqueParticipants, UniqueClaimers: value.UniqueClaimers,
		})
	}
	return result, nil
}

func (a *Admin) ListDailyOverview(
	ctx context.Context,
	workspaceID string,
	from, until time.Time,
) ([]DailyOverviewModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	values, err := a.repository.ListDailyOverview(mergedCtx, workspaceID, from, until)
	if err != nil {
		return nil, err
	}
	result := make([]DailyOverviewModel, 0, len(values))
	for _, value := range values {
		result = append(result, DailyOverviewModel{
			Date: value.Date, TasksTotal: value.TasksTotal,
			ActiveTasks: value.ActiveTasks, VisibleTasks: value.VisibleTasks,
			ProgressCreated: value.ProgressCreated, ProgressAmount: value.ProgressAmount,
			ReadyCount: value.ReadyCount, ClaimedCount: value.ClaimedCount,
			ManualClaimedCount: value.ManualClaimedCount, AutoClaimedCount: value.AutoClaimedCount,
			UniqueParticipants: value.UniqueParticipants, UniqueClaimers: value.UniqueClaimers,
		})
	}
	return result, nil
}

func (a *Admin) RefreshDailyStats(ctx context.Context, workspaceID string, from, until time.Time) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.RefreshDailyStats(mergedCtx, workspaceID, from, until)
}

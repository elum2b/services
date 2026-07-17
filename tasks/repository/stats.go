package repository

import (
	"context"
	"fmt"
	"time"

	tasksqlc "github.com/elum2b/services/tasks/sqlc"
)

type Stats struct {
	TasksTotal         uint64
	ActiveTasks        uint64
	VisibleTasks       uint64
	ProgressTotal      uint64
	OpenProgress       uint64
	ReadyProgress      uint64
	ClaimedProgress    uint64
	ProgressCreated    uint64
	ProgressAmount     uint64
	ReadyCount         uint64
	ClaimedCount       uint64
	ManualClaimedCount uint64
	AutoClaimedCount   uint64
	UniqueParticipants uint64
	UniqueClaimers     uint64
}

type SingleTaskStats struct {
	TaskID             uint64
	ProgressTotal      uint64
	OpenProgress       uint64
	ReadyProgress      uint64
	ClaimedProgress    uint64
	ProgressCreated    uint64
	ProgressAmount     uint64
	ReadyCount         uint64
	ClaimedCount       uint64
	ManualClaimedCount uint64
	AutoClaimedCount   uint64
	UniqueParticipants uint64
	UniqueClaimers     uint64
}

type DailyStats struct {
	Date               time.Time
	TaskID             uint64
	ProgressCreated    uint64
	ProgressAmount     uint64
	ReadyCount         uint64
	ClaimedCount       uint64
	ManualClaimedCount uint64
	AutoClaimedCount   uint64
	UniqueParticipants uint64
	UniqueClaimers     uint64
}

type DailyOverview struct {
	Date               time.Time
	TasksTotal         uint64
	ActiveTasks        uint64
	VisibleTasks       uint64
	ProgressCreated    uint64
	ProgressAmount     uint64
	ReadyCount         uint64
	ClaimedCount       uint64
	ManualClaimedCount uint64
	AutoClaimedCount   uint64
	UniqueParticipants uint64
	UniqueClaimers     uint64
}

func (r *Repository) GetStats(ctx context.Context, workspaceID string) (Stats, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Stats{}, err
	}

	row, err := r.q.AdminGetTaskStats(ctx, tasksqlc.AdminGetTaskStatsParams{
		WorkspaceID: workspaceID, WorkspaceID_2: workspaceID, WorkspaceID_3: workspaceID,
	})
	if err != nil {
		return Stats{}, err
	}
	return Stats{
		TasksTotal: uint64(row.TasksTotal), ActiveTasks: uint64(row.ActiveTasks),
		VisibleTasks: uint64(row.VisibleTasks), ProgressTotal: uint64(row.ProgressTotal),
		OpenProgress: uint64(row.OpenProgress), ReadyProgress: uint64(row.ReadyProgress),
		ClaimedProgress: uint64(row.ClaimedProgress), ProgressCreated: uint64(row.ProgressCreated),
		ProgressAmount: uint64(row.ProgressAmount), ReadyCount: uint64(row.ReadyCount),
		ClaimedCount: uint64(row.ClaimedCount), ManualClaimedCount: uint64(row.ManualClaimedCount),
		AutoClaimedCount: uint64(row.AutoClaimedCount), UniqueParticipants: uint64(row.UniqueParticipants),
		UniqueClaimers: uint64(row.UniqueClaimers),
	}, nil
}

func (r *Repository) GetSingleTaskStats(
	ctx context.Context,
	workspaceID string,
	taskID uint64,
) (SingleTaskStats, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return SingleTaskStats{}, err
	}

	row, err := r.q.AdminGetSingleTaskStats(ctx, tasksqlc.AdminGetSingleTaskStatsParams{
		WorkspaceID: workspaceID, TaskID: int64(taskID),
		WorkspaceID_2: workspaceID, TaskID_2: int64(taskID),
		WorkspaceID_3: workspaceID, ID: int64(taskID),
	})
	if err != nil {
		return SingleTaskStats{}, err
	}
	return SingleTaskStats{
		TaskID: uint64(row.TaskID), ProgressTotal: uint64(row.ProgressTotal),
		OpenProgress: uint64(row.OpenProgress), ReadyProgress: uint64(row.ReadyProgress),
		ClaimedProgress: uint64(row.ClaimedProgress), ProgressCreated: uint64(row.ProgressCreated),
		ProgressAmount: uint64(row.ProgressAmount), ReadyCount: uint64(row.ReadyCount),
		ClaimedCount: uint64(row.ClaimedCount), ManualClaimedCount: uint64(row.ManualClaimedCount),
		AutoClaimedCount: uint64(row.AutoClaimedCount), UniqueParticipants: uint64(row.UniqueParticipants),
		UniqueClaimers: uint64(row.UniqueClaimers),
	}, nil
}

func (r *Repository) ListDailyStats(
	ctx context.Context,
	workspaceID string,
	taskID uint64,
	from, until time.Time,
) ([]DailyStats, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	rows, err := r.q.AdminListTaskDailyStats(ctx, tasksqlc.AdminListTaskDailyStatsParams{
		WorkspaceID: workspaceID, TaskID: int64(taskID),
		StatsDate: from, StatsDate_2: until,
		WorkspaceID_2: workspaceID, TaskID_2: int64(taskID),
		WorkspaceID_3: workspaceID, TaskID_3: int64(taskID),
		Column9: from, Column10: until,
	})
	if err != nil {
		return nil, err
	}
	result := make([]DailyStats, 0, len(rows))
	for _, row := range rows {
		result = append(result, DailyStats{
			Date: row.StatsDate, TaskID: uint64(row.TaskID),
			ProgressCreated: uint64(row.ProgressCreated), ProgressAmount: uint64(row.ProgressAmount),
			ReadyCount: uint64(row.ReadyCount), ClaimedCount: uint64(row.ClaimedCount),
			ManualClaimedCount: uint64(row.ManualClaimedCount), AutoClaimedCount: uint64(row.AutoClaimedCount),
			UniqueParticipants: uint64(row.UniqueParticipants), UniqueClaimers: uint64(row.UniqueClaimers),
		})
	}
	return result, nil
}

func (r *Repository) ListDailyOverview(
	ctx context.Context,
	workspaceID string,
	from, until time.Time,
) ([]DailyOverview, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return nil, err
	}

	rows, err := r.q.AdminListTaskDailyOverview(ctx, tasksqlc.AdminListTaskDailyOverviewParams{
		WorkspaceID: workspaceID, StatsDate: from, StatsDate_2: until,
		WorkspaceID_2: workspaceID, WorkspaceID_3: workspaceID, WorkspaceID_4: workspaceID,
		Column7: from, Column8: until,
	})
	if err != nil {
		return nil, err
	}
	result := make([]DailyOverview, 0, len(rows))
	for _, row := range rows {
		result = append(result, DailyOverview{
			Date: row.StatsDate, TasksTotal: uint64(row.TasksTotal),
			ActiveTasks: uint64(row.ActiveTasks), VisibleTasks: uint64(row.VisibleTasks),
			ProgressCreated: uint64(row.ProgressCreated), ProgressAmount: uint64(row.ProgressAmount),
			ReadyCount: uint64(row.ReadyCount), ClaimedCount: uint64(row.ClaimedCount),
			ManualClaimedCount: uint64(row.ManualClaimedCount), AutoClaimedCount: uint64(row.AutoClaimedCount),
			UniqueParticipants: uint64(row.UniqueParticipants), UniqueClaimers: uint64(row.UniqueClaimers),
		})
	}
	return result, nil
}

func (r *Repository) RefreshDailyStats(ctx context.Context, workspaceID string, from, until time.Time) error {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}
	if from.IsZero() || until.IsZero() || from.After(until) {
		return fmt.Errorf("tasks stats workspace or date range is invalid")
	}

	if err := r.q.RefreshTaskDailyStats(ctx, tasksqlc.RefreshTaskDailyStatsParams{
		RefreshWorkspaceID: workspaceID,
		OccurredAt:         from,
		OccurredAt_2:       until,
	}); err != nil {
		return err
	}
	return r.q.RefreshTaskDailyOverview(ctx, tasksqlc.RefreshTaskDailyOverviewParams{
		RefreshWorkspaceID: workspaceID,
		OccurredAt:         from,
		OccurredAt_2:       until,
	})
}

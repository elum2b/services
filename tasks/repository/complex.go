package repository

import (
	"context"
	"time"

	tasksqlc "github.com/elum2b/services/tasks/sqlc"
)

type complexRefreshResult struct {
	changed  bool
	progress *Progress
}

func (r *Repository) refreshComplexParentsForChangedTasks(
	ctx context.Context,
	identity Identity,
	changedTaskIDs []uint64,
	now time.Time,
) error {
	if len(changedTaskIDs) == 0 {
		return nil
	}
	queue := uniqueUint64(changedTaskIDs)
	for len(queue) > 0 {
		parentIDs, err := repositoryValue(ctx, r, func(ctx context.Context) ([]uint64, error) {
			ids, err := r.q.ListComplexParentIDsForConditionTasks(
				ctx,
				tasksqlc.ListComplexParentIDsForConditionTasksParams{
					WorkspaceID: identity.WorkspaceID,
					Column2:     int64sFromUint64s(queue),
				},
			)
			if err != nil {
				return nil, err
			}
			return uint64sFromInt64s(ids), nil
		})
		if err != nil {
			return err
		}
		next := make([]uint64, 0, len(parentIDs))
		for _, parentID := range parentIDs {
			refreshed, err := r.refreshComplexParent(ctx, identity, parentID, now)
			if err != nil {
				return err
			}
			if refreshed.changed {
				next = append(next, parentID)
			}
		}
		queue = next
	}
	return nil
}

func (r *Repository) refreshComplexParent(
	ctx context.Context,
	identity Identity,
	parentTaskID uint64,
	now time.Time,
) (complexRefreshResult, error) {
	conditions, err := repositoryValue(
		ctx,
		r,
		func(ctx context.Context) ([]tasksqlc.ListComplexConditionProgressForParentRow, error) {
			return r.q.ListComplexConditionProgressForParent(ctx, tasksqlc.ListComplexConditionProgressForParentParams{
				WorkspaceID:    identity.WorkspaceID,
				ParentTaskID:   int64(parentTaskID),
				AppID:          identity.AppID,
				PlatformID:     identity.PlatformID,
				PlatformUserID: identity.PlatformUserID,
				PeriodStartAt:  now,
				PeriodEndAt:    now,
			})
		},
	)
	if err != nil || len(conditions) == 0 {
		return complexRefreshResult{}, err
	}
	parent := complexParentFromConditionRow(identity.WorkspaceID, conditions[0])
	if parent.TaskKind != TaskKindComplex || !taskVisibleAt(parent, now) {
		return complexRefreshResult{}, nil
	}
	var completed uint64
	for _, condition := range conditions {
		if complexConditionCompleted(condition) {
			completed++
		}
	}
	periodStart, periodEnd := periodFor(parent, now)
	progress, err := r.currentProgressForUpdate(ctx, identity, parent.ID, now)
	if err != nil {
		if !isNoRows(err) {
			return complexRefreshResult{}, err
		}
		if completed == 0 {
			return complexRefreshResult{}, nil
		}
		progress = Progress{
			PeriodStartAt: periodStart,
			PeriodEndAt:   periodEnd,
			Status:        StatusOpen,
		}
	}
	if progress.Status == StatusClaimed {
		return complexRefreshResult{progress: &progress}, nil
	}
	existingProgress := progress.ID != 0
	beforeProgress := progress.Progress
	beforeStatus := progress.Status
	progress.Progress = completed
	progress.PeriodStartAt = periodStart
	progress.PeriodEndAt = periodEnd
	if completed >= uint64(len(conditions)) {
		progress.Status = StatusReady
		if progress.ReadyAt == nil {
			progress.ReadyAt = &now
		}
	} else {
		progress.Status = StatusOpen
		progress.ReadyAt = nil
	}
	changed := beforeProgress != progress.Progress || beforeStatus != progress.Status
	if existingProgress && !changed {
		return complexRefreshResult{progress: &progress}, nil
	}
	if !existingProgress {
		progress.Rewards, err = r.rewards(ctx, parent.WorkspaceID, parent.ID)
		if err != nil {
			return complexRefreshResult{}, err
		}
	}

	if err := r.saveOrCreateComplexProgress(ctx, identity, parent, progress); err != nil {
		return complexRefreshResult{}, err
	}
	if progress.ID == 0 {
		return complexRefreshResult{changed: changed || !existingProgress}, nil
	}
	return complexRefreshResult{changed: changed || !existingProgress, progress: &progress}, nil
}

func complexParentFromConditionRow(workspaceID string, row tasksqlc.ListComplexConditionProgressForParentRow) Task {
	return Task{
		ID:          uint64(row.ParentID),
		WorkspaceID: workspaceID,
		TaskKind:    row.ParentTaskKind,
		TargetCount: uint64(row.ParentTargetCount),
		ResetUnit:   row.ParentResetUnit,
		ResetEvery:  uint32(row.ParentResetEvery),
		StartAt:     ptrTime(row.ParentStartAt),
		EndAt:       ptrTime(row.ParentEndAt),
	}
}

func (r *Repository) saveOrCreateComplexProgress(
	ctx context.Context,
	identity Identity,
	parent Task,
	progress Progress,
) error {
	if progress.ID != 0 {
		return r.saveProgress(ctx, progress)
	}
	readyAt := progress.ReadyAt
	_, err := r.q.UpsertProgress(ctx, tasksqlc.UpsertProgressParams{
		WorkspaceID:    identity.WorkspaceID,
		TaskID:         int64(parent.ID),
		AppID:          identity.AppID,
		PlatformID:     identity.PlatformID,
		PlatformUserID: identity.PlatformUserID,
		PeriodStartAt:  progress.PeriodStartAt,
		PeriodEndAt:    progress.PeriodEndAt,
		Progress:       int64(progress.Progress),
		Status:         progress.Status,
		ReadyAt:        nullTime(readyAt),
		RewardsSnapshot: rawMessageParam(
			rewardsSnapshot(progress.Rewards),
		),
	})
	return err
}

func complexConditionCompleted(condition tasksqlc.ListComplexConditionProgressForParentRow) bool {
	if !condition.ProgressID.Valid || !condition.Status.Valid {
		return false
	}
	status := condition.Status.String
	switch condition.RequiredStatus {
	case ComplexRequiredStatusClaimed:
		return status == StatusClaimed
	default:
		return status == StatusReady || status == StatusClaimed
	}
}

func uniqueUint64(values []uint64) []uint64 {
	if len(values) < 2 {
		return values
	}
	seen := make(map[uint64]struct{}, len(values))
	out := make([]uint64, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (r *Repository) refreshComplexTaskBeforeClaim(
	ctx context.Context,
	identity Identity,
	task Task,
	now time.Time,
) (*Progress, error) {
	if task.TaskKind != TaskKindComplex {
		return nil, nil
	}
	refreshed, err := r.refreshComplexParent(ctx, identity, task.ID, now)
	if err != nil {
		return nil, err
	}
	return refreshed.progress, nil
}
